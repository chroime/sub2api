package upstreamgovernance

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Opt-in isolated role and random schema only. Never read application settings
// or credentials to discover a database connection.
func TestImportTemplatesPostgresConcurrentPersistence(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback governance_fixture database")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.User == nil || u.User.Username() != "governance_fixture" {
		t.Fatal("requires isolated loopback governance_fixture database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	schema := fmt.Sprintf("governance_import_templates_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer fixture.Close()
	_, err = fixture.Exec(`CREATE TABLE settings(id BIGSERIAL PRIMARY KEY,key VARCHAR(100) NOT NULL UNIQUE,value TEXT NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); INSERT INTO settings(key,value) VALUES('upstream_governance_model_templates','{"version":7,"templates":[]}'),('fixture_other_setting','untouched')`)
	require.NoError(t, err)
	ctx := t.Context()
	svc := NewService(NewSQLStore(fixture), nil, nil, nil, false)
	empty, err := svc.ImportTemplates(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), empty.Version)
	require.NotNil(t, empty.Templates)
	require.Empty(t, empty.Templates)
	var count int
	require.NoError(t, fixture.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&count))
	require.Equal(t, 2, count, "GET must not persist a default row")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := svc.SaveImportTemplates(ctx, importTemplateFixture())
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else {
			require.ErrorIs(t, e, ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	restarted := NewService(NewSQLStore(fixture), nil, nil, nil, false)
	saved, err := restarted.ImportTemplates(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	require.Equal(t, importTemplateFixture().Templates, saved.Templates)
	// Two edits made from the same collection revision cannot silently overwrite.
	stale := *saved
	stale.Templates = append([]ImportTemplate{}, saved.Templates...)
	saved.Templates[0].Name = " Updated "
	saved.Templates[0].Settings = ImportTemplateSettings{Concurrency: 1}
	updated, err := restarted.SaveImportTemplates(ctx, *saved)
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Version)
	require.Equal(t, "Updated", updated.Templates[0].Name)
	require.Equal(t, ImportTemplateSettings{Concurrency: 1}, updated.Templates[0].Settings)
	_, err = svc.SaveImportTemplates(ctx, stale)
	require.ErrorIs(t, err, ErrConflict)
	cleared, err := svc.SaveImportTemplates(ctx, ImportTemplates{Version: 2, Templates: []ImportTemplate{}})
	require.NoError(t, err)
	require.Equal(t, int64(3), cleared.Version)
	require.Empty(t, cleared.Templates)
	loaded, err := restarted.ImportTemplates(ctx)
	require.NoError(t, err)
	require.Equal(t, cleared, loaded)
	raw, err := json.Marshal(loaded)
	require.NoError(t, err)
	require.JSONEq(t, `{"version":3,"templates":[]}`, string(raw))
	var other, model string
	require.NoError(t, fixture.QueryRow(`SELECT value FROM settings WHERE key='fixture_other_setting'`).Scan(&other))
	require.Equal(t, "untouched", other)
	require.NoError(t, fixture.QueryRow(`SELECT value FROM settings WHERE key='upstream_governance_model_templates'`).Scan(&model))
	require.JSONEq(t, `{"version":7,"templates":[]}`, model)
	require.NoError(t, fixture.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&count))
	require.Equal(t, 3, count, "only the dedicated import-template key may be created")
}
