package upstreamgovernance

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSQLGovernanceIntervalMigrationPreservesDataAndAcceptsFlexibleCadence(t *testing.T) {
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
	schema := fmt.Sprintf("governance_intervals_%d", time.Now().UnixNano())
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
		raw, readErr := os.ReadFile("../../migrations/" + name)
		require.NoError(t, readErr)
		_, execErr := db.Exec(string(raw))
		require.NoError(t, execErr)
	}
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "253_upstream_governance_automation.sql", "254_upstream_governance_recharge_plans.sql", "255_upstream_governance_key_creation_plans.sql", "258_upstream_governance_key_health.sql"} {
		apply(migration)
	}
	var siteID, bindingID int64
	require.NoError(t, db.QueryRow(`INSERT INTO upstream_governance_sites(name,platform,base_url,session_cipher,login_cipher) VALUES('Legacy intervals','sub2api','https://fixture.example','fixture-session-cipher','fixture-login-cipher') RETURNING id`).Scan(&siteID))
	require.NoError(t, db.QueryRow(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,marker,key_cipher,account_id) VALUES($1,'8','openai',1,'fixture-intervals','fixture-key-cipher',5) RETURNING id`, siteID).Scan(&bindingID))
	var siteBefore, bindingBefore, siteAfter, bindingAfter string
	require.NoError(t, db.QueryRow(`SELECT to_jsonb(s)::text FROM upstream_governance_sites s WHERE id=$1`, siteID).Scan(&siteBefore))
	require.NoError(t, db.QueryRow(`SELECT to_jsonb(b)::text FROM upstream_governance_bindings b WHERE id=$1`, bindingID).Scan(&bindingBefore))
	_, err = db.Exec(`UPDATE upstream_governance_sites SET interval_minutes=1 WHERE id=$1`, siteID)
	require.Error(t, err, "legacy constraint rejects a one-minute interval")
	_, err = db.Exec(`UPDATE upstream_governance_bindings SET probe_interval_minutes=1441 WHERE id=$1`, bindingID)
	require.Error(t, err, "legacy probe constraint rejects intervals beyond one day")
	apply("256_upstream_governance_flexible_intervals.sql")
	require.NoError(t, db.QueryRow(`SELECT to_jsonb(s)::text FROM upstream_governance_sites s WHERE id=$1`, siteID).Scan(&siteAfter))
	require.NoError(t, db.QueryRow(`SELECT to_jsonb(b)::text FROM upstream_governance_bindings b WHERE id=$1`, bindingID).Scan(&bindingAfter))
	require.JSONEq(t, siteBefore, siteAfter, "migration must not change credentials, policy, due times, or any other saved field")
	require.JSONEq(t, bindingBefore, bindingAfter)

	store := NewSQLStore(db)
	svc := NewService(store, nil, nil, fakeCipher{}, true)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, minutes := range []int{1, 2, 1441, 10081, 43200, maxIntervalMinutes} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			site, getErr := store.GetSite(t.Context(), siteID)
			require.NoError(t, getErr)
			site.IntervalMinutes = minutes
			site.NextSyncAt = time.Unix(now.Unix()+int64(minutes)*60, int64(now.Nanosecond()))
			require.NoError(t, store.UpdateSite(t.Context(), site, site.Version))
			site, getErr = store.GetSite(t.Context(), siteID)
			require.NoError(t, getErr)
			require.Equal(t, minutes, site.IntervalMinutes)
			require.Equal(t, now.Unix()+int64(minutes)*60, site.NextSyncAt.Unix())
			_, marshalErr := json.Marshal(site)
			require.NoError(t, marshalErr)

			bindings, getErr := store.ListBindings(t.Context(), siteID)
			require.NoError(t, getErr)
			require.Len(t, bindings, 1)
			binding := bindings[0]
			binding.ProbeIntervalMinutes, binding.NextProbeAt = minutes, site.NextSyncAt
			require.NoError(t, store.SaveBinding(t.Context(), &binding))
			bindings, getErr = store.ListBindings(t.Context(), siteID)
			require.NoError(t, getErr)
			require.Equal(t, minutes, bindings[0].ProbeIntervalMinutes)
			require.True(t, site.NextSyncAt.Equal(bindings[0].NextProbeAt))

			monitor := defaultBalanceMonitor(site.Platform)
			monitor.CooldownMinutes = minutes
			_, getErr = svc.ConfigureBalanceMonitor(t.Context(), siteID, site.Version, monitor)
			require.NoError(t, getErr)
			site, getErr = store.GetSite(t.Context(), siteID)
			require.NoError(t, getErr)
			require.Equal(t, minutes, site.BalanceMonitor.CooldownMinutes)
			plan, getErr := svc.RechargePlan(t.Context(), siteID)
			require.NoError(t, getErr)
			plan.Policy.CooldownMinutes = minutes
			_, getErr = svc.ConfigureRechargePlan(t.Context(), siteID, plan.Version, plan.Policy)
			require.NoError(t, getErr)
			plan, getErr = svc.RechargePlan(t.Context(), siteID)
			require.NoError(t, getErr)
			require.Equal(t, minutes, plan.Policy.CooldownMinutes)
		})
	}
	for _, invalid := range []int64{-1, 0, int64(maxIntervalMinutes) + 1} {
		_, err = db.Exec(`UPDATE upstream_governance_sites SET interval_minutes=$2 WHERE id=$1`, siteID, invalid)
		require.Error(t, err)
		_, err = db.Exec(`UPDATE upstream_governance_bindings SET probe_interval_minutes=$2 WHERE id=$1`, bindingID, invalid)
		require.Error(t, err)
	}
}
