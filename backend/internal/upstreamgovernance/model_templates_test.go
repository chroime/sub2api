package upstreamgovernance

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func templateFixture() ModelTemplates {
	return ModelTemplates{Templates: []ModelTemplate{{ID: "openai-default", Name: "OpenAI", Platform: "openai", Models: []string{"gpt-5.6", "gpt-6"}, IsDefault: true}}}
}

func TestModelTemplatesValidationAndIndependentDefaults(t *testing.T) {
	input := templateFixture()
	input.Templates[0].Name = " OpenAI 默认 "
	input.Templates[0].Models = []string{"gpt-6", " gpt-6 ", "gpt-5.6"}
	input.Templates = append(input.Templates, ModelTemplate{ID: "claude", Name: "Claude", Platform: "anthropic", Models: []string{"claude-sonnet"}, IsDefault: true})
	got, err := normalizeModelTemplates(input)
	require.NoError(t, err)
	require.Equal(t, "OpenAI 默认", got.Templates[0].Name)
	require.Equal(t, []string{"gpt-6", "gpt-5.6"}, got.Templates[0].Models)
	require.Len(t, input.Templates[0].Models, 3, "normalization must not mutate caller data")
	for name, change := range map[string]func(*ModelTemplates){
		"negative version":        func(v *ModelTemplates) { v.Version = -1 },
		"unknown platform":        func(v *ModelTemplates) { v.Templates[0].Platform = "newapi" },
		"empty whitelist":         func(v *ModelTemplates) { v.Templates[0].Models = nil },
		"blank model":             func(v *ModelTemplates) { v.Templates[0].Models = []string{" "} },
		"wildcard is not a model": func(v *ModelTemplates) { v.Templates[0].Models = []string{"gpt-*"} },
		"control model":           func(v *ModelTemplates) { v.Templates[0].Models = []string{"gpt\n6"} },
		"long model":              func(v *ModelTemplates) { v.Templates[0].Models = []string{strings.Repeat("x", 201)} },
		"too many models":         func(v *ModelTemplates) { v.Templates[0].Models = make([]string, 501) },
		"duplicate id":            func(v *ModelTemplates) { v.Templates = append(v.Templates, v.Templates[0]) },
		"duplicate default": func(v *ModelTemplates) {
			other := v.Templates[0]
			other.ID = "second"
			v.Templates = append(v.Templates, other)
		},
		"blank name": func(v *ModelTemplates) { v.Templates[0].Name = " " },
		"invalid id": func(v *ModelTemplates) { v.Templates[0].ID = "../template" },
	} {
		t.Run(name, func(t *testing.T) {
			value := templateFixture()
			change(&value)
			_, err := normalizeModelTemplates(value)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	clear, err := normalizeModelTemplates(ModelTemplates{})
	require.NoError(t, err)
	require.NotNil(t, clear.Templates)
	require.Empty(t, clear.Templates)
}

// Uses an isolated schema, never the live application settings or credentials.
func TestModelTemplatesPostgresConcurrentPersistence(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	if u.Hostname() != "127.0.0.1" || u.User == nil || u.User.Username() != "governance_fixture" {
		t.Fatal("requires isolated loopback governance_fixture database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	schema := fmt.Sprintf("governance_templates_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer fixture.Close()
	_, err = fixture.Exec(`CREATE TABLE settings (id BIGSERIAL PRIMARY KEY, key VARCHAR(100) NOT NULL UNIQUE, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	ctx := context.Background()
	svc := NewService(NewSQLStore(fixture), nil, nil, nil, false)
	empty, err := svc.ModelTemplates(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, empty.Version)
	require.Empty(t, empty.Templates)

	// Two first writers with version zero must not silently overwrite each other.
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.SaveModelTemplates(ctx, templateFixture())
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	successes, conflicts := 0, 0
	for err := range errors {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	// A second service represents another worker or a server restart.
	restarted := NewService(NewSQLStore(fixture), nil, nil, nil, false)
	saved, err := restarted.ModelTemplates(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Version)
	require.Equal(t, templateFixture().Templates, saved.Templates)
	stale := *saved
	saved.Templates[0].Name = "Updated"
	updated, err := restarted.SaveModelTemplates(ctx, *saved)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Version)
	_, err = svc.SaveModelTemplates(ctx, stale)
	require.ErrorIs(t, err, ErrConflict)
	cleared, err := svc.SaveModelTemplates(ctx, ModelTemplates{Version: 2})
	require.NoError(t, err)
	require.EqualValues(t, 3, cleared.Version)
	require.Empty(t, cleared.Templates)
}
