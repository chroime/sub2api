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

func TestSQLStoreDeleteSitePostgresIntegration(t *testing.T) {
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
	schema := fmt.Sprintf("governance_delete_fixture_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY);
CREATE TABLE groups(id BIGSERIAL PRIMARY KEY);
CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,credentials JSONB NOT NULL DEFAULT '{}',extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ);
INSERT INTO groups(id) VALUES(1);
INSERT INTO accounts(id,credentials,extra,deleted_at) VALUES
 (1,'{"api_key":"active-fixture-key","base_url":"https://fixture.example"}','{"upstream_governance_marker":"active-marker"}',NULL),
 (2,'{"api_key":"retired-fixture-key","base_url":"https://fixture.example"}','{"upstream_governance_marker":"retired-marker"}',NOW());`)
	require.NoError(t, err)
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql", "258_upstream_governance_key_health.sql"} {
		raw, err := os.ReadFile("../../migrations/" + migration)
		require.NoError(t, err)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	_, err = db.Exec(`CREATE TABLE fixture_delete_blocker(site_id BIGINT REFERENCES upstream_governance_sites(id) ON DELETE RESTRICT)`)
	require.NoError(t, err)
	store := NewSQLStore(db)
	var accountsBefore string
	require.NoError(t, db.QueryRow(`SELECT json_agg(a ORDER BY id)::text FROM accounts a`).Scan(&accountsBefore))

	for _, test := range []struct {
		name       string
		accountIDs []int64
		key        string
		blocked    bool
		rollback   bool
	}{
		{name: "active account", accountIDs: []int64{1}, blocked: true},
		{name: "pending import", accountIDs: []int64{0}, blocked: true},
		{name: "orphan beside active account", accountIDs: []int64{2, 1}, blocked: true},
		{name: "orphan beside pending import", accountIDs: []int64{999, 0}, blocked: true},
		{name: "orphan beside pending key", accountIDs: []int64{2}, key: "pending", blocked: true},
		{name: "orphan beside managed key", accountIDs: []int64{999}, key: "managed", blocked: true},
		{name: "soft deleted account", accountIDs: []int64{2}},
		{name: "missing account", accountIDs: []int64{999}},
		{name: "both kinds of orphan", accountIDs: []int64{2, 999}},
		{name: "no bindings"},
		{name: "site failure rolls back orphan cleanup", accountIDs: []int64{2, 999}, rollback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			site := &Site{Name: test.name, Platform: "sub2api", BaseURL: "https://fixture.example", IntervalMinutes: 15, Status: "disconnected", NextSyncAt: time.Now()}
			require.NoError(t, store.CreateSite(t.Context(), site))
			for index, accountID := range test.accountIDs {
				binding := &Binding{SiteID: site.ID, RemoteGroupID: fmt.Sprint(index), Platform: "openai", LocalGroupID: 1, AccountID: accountID, Marker: fmt.Sprintf("site-%d-binding-%d", site.ID, index), ProbeIntervalMinutes: 30, NextProbeAt: time.Now()}
				require.NoError(t, store.SaveBinding(t.Context(), binding))
				require.NoError(t, store.AddCheck(t.Context(), &Check{SiteID: site.ID, BindingID: binding.ID, Model: "fixture", ProbeResult: ProbeResult{Success: true}}))
			}
			if test.key != "" {
				key := &ManagedKey{SiteID: site.ID, RemoteGroupID: "key", Platform: "openai", Marker: fmt.Sprintf("site-%d-key", site.ID), OwnerUserID: 1}
				if test.key == "managed" {
					key.RemoteKeyID, key.KeyCipher = "remote-fixture-key", "encrypted-fixture-key"
				}
				require.NoError(t, store.SaveManagedKey(t.Context(), key))
			}
			require.NoError(t, store.SaveSnapshot(t.Context(), &Snapshot{SiteID: site.ID, SiteVersion: 1}, []Event{{Kind: "fixture"}}))
			require.NoError(t, store.SavePreview(t.Context(), &Preview{ID: fmt.Sprintf("site-%d-preview", site.ID), SiteID: site.ID, ExpiresAt: time.Now().Add(time.Hour)}))
			if test.rollback {
				_, err = db.Exec(`INSERT INTO fixture_delete_blocker(site_id) VALUES($1)`, site.ID)
				require.NoError(t, err)
			}

			tables := []string{"sites", "bindings", "keys", "snapshots", "events", "checks", "previews"}
			before := make(map[string]int)
			siteRows := make(map[string]int)
			for _, table := range tables {
				var count int
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM upstream_governance_`+table).Scan(&count))
				before[table] = count
				column := "site_id"
				if table == "sites" {
					column = "id"
				}
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM upstream_governance_`+table+` WHERE `+column+`=$1`, site.ID).Scan(&count))
				siteRows[table] = count
			}
			err := store.DeleteSite(t.Context(), site.ID)
			if test.blocked {
				require.Equal(t, "site_in_use", ErrorCode(err))
			} else if test.rollback {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				_, err = store.GetSite(t.Context(), site.ID)
				require.ErrorIs(t, err, ErrNotFound)
			}
			for _, table := range tables {
				var count int
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM upstream_governance_`+table).Scan(&count))
				expected := before[table]
				if !test.blocked && !test.rollback {
					expected -= siteRows[table]
				}
				require.Equal(t, expected, count, "unexpected rows changed in %s", table)
			}
			var accountsAfter string
			require.NoError(t, db.QueryRow(`SELECT json_agg(a ORDER BY id)::text FROM accounts a`).Scan(&accountsAfter))
			require.Equal(t, accountsBefore, accountsAfter, "site deletion must never alter local accounts")
		})
	}
	require.ErrorIs(t, store.DeleteSite(t.Context(), 999999), ErrNotFound)
}
