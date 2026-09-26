package upstreamgovernance

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSQLGovernancePlatformMigration(t *testing.T) {
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
	schema := fmt.Sprintf("governance_platforms_%d", time.Now().UnixNano())
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
	apply := func(name string) {
		t.Helper()
		raw, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql"} {
		apply(migration)
	}
	store := NewSQLStore(db)
	site := &Site{Name: "Platform fixture", Platform: "sub2api", BaseURL: "https://fixture.example", IntervalMinutes: 15, Status: "disconnected", NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(t.Context(), site))
	legacy := &ManagedKey{SiteID: site.ID, RemoteGroupID: "legacy-grok", Platform: "openai", Marker: "legacy-openai-marker", RemoteKeyID: "legacy-key", OwnerUserID: 7, KeyCipher: "fixture-cipher"}
	require.NoError(t, store.SaveManagedKey(t.Context(), legacy))
	apply("250_upstream_governance_platforms.sql")
	for _, platform := range governanceTestPlatforms {
		key := &ManagedKey{SiteID: site.ID, RemoteGroupID: platform, Platform: platform, Marker: "key-" + platform, OwnerUserID: 7}
		require.NoError(t, store.SaveManagedKey(t.Context(), key), platform)
		binding := &Binding{SiteID: site.ID, RemoteGroupID: platform, Platform: platform, LocalGroupID: 1, Marker: "binding-" + platform, ProbeIntervalMinutes: 30, NextProbeAt: time.Now()}
		require.NoError(t, store.SaveBinding(t.Context(), binding), platform)
	}
	for _, invalid := range []string{"composite", "custom"} {
		err = store.SaveManagedKey(t.Context(), &ManagedKey{SiteID: site.ID, RemoteGroupID: invalid, Platform: invalid, Marker: "invalid-" + invalid, OwnerUserID: 7})
		require.Error(t, err)
		err = store.SaveBinding(t.Context(), &Binding{SiteID: site.ID, RemoteGroupID: invalid, Platform: invalid, LocalGroupID: 1, Marker: "invalid-binding-" + invalid, ProbeIntervalMinutes: 30, NextProbeAt: time.Now()})
		require.Error(t, err)
	}
	stored, err := store.GetManagedKey(t.Context(), site.ID, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, legacy, stored, "platform migration must not rewrite legacy managed keys")
}
