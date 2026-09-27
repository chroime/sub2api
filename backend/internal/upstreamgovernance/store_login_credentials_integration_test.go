package upstreamgovernance_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestSQLGovernanceLoginCredentialsMigrationAndAtomicCAS(t *testing.T) {
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
	schema := fmt.Sprintf("governance_login_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	apply := func(name string) {
		t.Helper()
		raw, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "255_upstream_governance_key_creation_plans.sql"} {
		apply(migration)
	}
	encryptor, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	sessionCipher, err := encryptor.Encrypt(`{"access_token":"fixture-session","user_id":5}`)
	require.NoError(t, err)
	var siteID int64
	err = db.QueryRow(`INSERT INTO upstream_governance_sites(name,platform,base_url,session_cipher,status) VALUES('Legacy','sub2api','https://fixture.example',$1,'connected') RETURNING id`, sessionCipher).Scan(&siteID)
	require.NoError(t, err)
	apply("251_upstream_governance_login_credentials.sql")
	apply("252_upstream_governance_multiple_target_groups.sql")
	store := gov.NewSQLStore(db)
	svc := gov.NewService(store, nil, nil, encryptor, true)
	legacy, err := svc.LoginCredentials(t.Context(), siteID)
	require.NoError(t, err)
	require.Equal(t, &gov.LoginCredentialsResult{Version: 1}, legacy)
	site, err := store.GetSite(t.Context(), siteID)
	require.NoError(t, err)
	stale := *site
	site.Name = "Saved details"
	saved, err := svc.UpdateSiteWithLogin(t.Context(), siteID, *site, &gov.LoginCredentials{Username: "fixture-administrator", Password: "credential-canary"})
	require.NoError(t, err)
	require.EqualValues(t, 2, saved.Version)
	require.Equal(t, sessionCipher, saved.SessionCipher)
	restarted := gov.NewService(gov.NewSQLStore(db), nil, nil, encryptor, true)
	read, err := restarted.LoginCredentials(t.Context(), siteID)
	require.NoError(t, err)
	require.Equal(t, "credential-canary", read.Password)
	var persisted string
	require.NoError(t, db.QueryRow(`SELECT login_cipher FROM upstream_governance_sites WHERE id=$1`, siteID).Scan(&persisted))
	require.Equal(t, saved.LoginCipher, persisted)
	require.NotContains(t, persisted, "credential-canary")
	sites, err := store.ListSites(t.Context())
	require.NoError(t, err)
	raw, err := json.Marshal(sites)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "credential-canary")
	require.NotContains(t, string(raw), persisted)
	_, err = svc.UpdateSiteWithLogin(t.Context(), siteID, stale, &gov.LoginCredentials{Username: "stale", Password: "stale-canary"})
	require.ErrorIs(t, err, gov.ErrConflict)
	require.ErrorIs(t, store.StageLoginChallenge(t.Context(), siteID, stale.Version, "must-not-persist"), gov.ErrConflict)
	require.NoError(t, store.StageLoginChallenge(t.Context(), siteID, saved.Version, saved.LoginCipher))
	unchanged, err := store.GetSite(t.Context(), siteID)
	require.NoError(t, err)
	require.Equal(t, saved.Version, unchanged.Version, "challenge staging does not advance edit version")
	_, err = db.Exec(`ALTER TABLE upstream_governance_sites ADD CONSTRAINT fixture_reject_name CHECK(name <> 'reject')`)
	require.NoError(t, err)
	rejected := *saved
	rejected.Name = "reject"
	_, err = svc.UpdateSiteWithLogin(t.Context(), siteID, rejected, &gov.LoginCredentials{Username: "rejected", Password: "rejected-canary"})
	require.Error(t, err)
	unchanged, err = store.GetSite(t.Context(), siteID)
	require.NoError(t, err)
	require.Equal(t, saved.Name, unchanged.Name)
	require.Equal(t, saved.LoginCipher, unchanged.LoginCipher, "login and metadata must commit or fail together")
	unchanged.BaseURL = "https://replacement.example"
	_, err = svc.UpdateSite(t.Context(), siteID, *unchanged)
	require.NoError(t, err)
	read, err = svc.LoginCredentials(t.Context(), siteID)
	require.NoError(t, err)
	require.Equal(t, gov.LoginCredentials{}, read.LoginCredentials)
	cleared, err := store.GetSite(t.Context(), siteID)
	require.NoError(t, err)
	require.Empty(t, cleared.SessionCipher)
}
