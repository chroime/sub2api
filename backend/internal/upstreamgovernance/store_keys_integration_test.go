package upstreamgovernance_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type keyFixtureDoer func(*http.Request) (*http.Response, error)

func (f keyFixtureDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestSQLManagedKeysPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer base.Close()
	schema := fmt.Sprintf("governance_keys_fixture_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ); INSERT INTO groups(id) VALUES(1)`)
	require.NoError(t, err)
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql"} {
		raw, err := os.ReadFile("../../migrations/" + migration)
		require.NoError(t, err)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	encryptor, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	sessionJSON, err := json.Marshal(gov.Session{AccessToken: "fixture-session-canary", UserID: 5})
	require.NoError(t, err)
	sessionCipher, err := encryptor.Encrypt(string(sessionJSON))
	require.NoError(t, err)
	store := gov.NewSQLStore(db)
	site := &gov.Site{Name: "Fixture", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true, IntervalMinutes: 15, SessionCipher: sessionCipher, Status: "connected", NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(t.Context(), site))
	snapshot := &gov.Snapshot{SiteID: site.ID, SiteVersion: site.Version, Catalog: gov.Catalog{Groups: []gov.RemoteGroup{{ID: "8", Name: "Visible", Platform: "openai"}, {ID: "9", Name: "Legacy", Platform: "openai"}}}}
	require.NoError(t, store.SaveSnapshot(t.Context(), snapshot, nil))
	posts := 0
	remoteMarker := ""
	connector := gov.NewConnector(func(context.Context, gov.Site) (gov.HTTPDoer, error) {
		return keyFixtureDoer(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "upstream.example", r.URL.Host)
			require.Equal(t, "Bearer fixture-session-canary", r.Header.Get("Authorization"))
			status, body := 200, ""
			switch {
			case r.URL.Path == "/api/v1/user/profile":
				body = `{"code":0,"data":{"id":5}}`
			case r.URL.Path == "/api/v1/keys" && r.Method == "GET":
				body = `{"code":0,"data":{"total":0,"items":[]}}`
				if remoteMarker != "" {
					body = fmt.Sprintf(`{"code":0,"data":{"total":1,"items":[{"id":12,"name":%q,"group_id":8,"group":{"id":8,"name":"Visible","platform":"openai"},"key":"inference-secret-canary"}]}}`, remoteMarker)
				}
			case r.URL.Path == "/api/v1/keys" && r.Method == "POST":
				posts++
				var input struct {
					Name    string `json:"name"`
					GroupID int64  `json:"group_id"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				require.Equal(t, int64(8), input.GroupID)
				remoteMarker = input.Name
				// The upstream commits but its response fails. Explicit retry must
				// reconcile the stable marker instead of issuing a second POST.
				status, body = 502, `{"message":"uncertain-secret-canary"}`
			default:
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}), nil
	})
	service := gov.NewService(store, connector, nil, encryptor, true)
	request := gov.CreateKeysInput{SnapshotID: snapshot.ID, Selections: []gov.KeySelection{{RemoteGroupID: "8", Platform: "openai"}}}
	result, err := service.CreateKeys(t.Context(), site.ID, request)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	require.Empty(t, result.Items[0].Key)
	pending, err := store.ListManagedKeys(t.Context(), site.ID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, int64(5), pending[0].OwnerUserID)
	require.False(t, pending[0].HasKey)
	require.ErrorIs(t, store.DeleteSite(t.Context(), site.ID), gov.ErrConflict)
	_, err = db.Exec(`DELETE FROM upstream_governance_sites WHERE id=$1`, site.ID)
	require.Error(t, err, "the foreign key must also protect pending ownership")
	result, err = service.CreateKeys(t.Context(), site.ID, request)
	require.NoError(t, err)
	require.Equal(t, "created", result.Items[0].Status)
	require.Equal(t, "inference-secret-canary", result.Items[0].Key)
	keyID := result.Items[0].ManagedKey.ID
	result, err = service.CreateKeys(t.Context(), site.ID, request)
	require.NoError(t, err)
	require.Equal(t, "reused", result.Items[0].Status)
	require.Equal(t, keyID, result.Items[0].ManagedKey.ID)
	require.Equal(t, 1, posts)
	record, err := store.GetManagedKey(t.Context(), site.ID, keyID)
	require.NoError(t, err)
	require.NotContains(t, record.KeyCipher, "canary")
	plain, err := encryptor.Decrypt(record.KeyCipher)
	require.NoError(t, err)
	require.Contains(t, plain, "inference-secret-canary")
	_, err = store.GetManagedKey(t.Context(), site.ID+1, keyID)
	require.ErrorIs(t, err, gov.ErrNotFound)
	record.OwnerUserID = 6
	require.ErrorIs(t, store.SaveManagedKey(t.Context(), record), gov.ErrConflict)
	record.OwnerUserID = 5
	record.KeyCipher = "replacement-cipher"
	require.ErrorIs(t, store.SaveManagedKey(t.Context(), record), gov.ErrConflict)
	revealed, err := service.RevealKey(t.Context(), site.ID, keyID)
	require.NoError(t, err)
	require.Equal(t, "inference-secret-canary", revealed.Key)
	var dump string
	require.NoError(t, db.QueryRow(`SELECT COALESCE(json_agg(t)::text,'[]') FROM upstream_governance_keys t`).Scan(&dump))
	require.NotContains(t, dump, "canary")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count))
	require.Zero(t, count)
	bindings, err := store.ListBindings(t.Context(), site.ID)
	require.NoError(t, err)
	require.Empty(t, bindings)
}
