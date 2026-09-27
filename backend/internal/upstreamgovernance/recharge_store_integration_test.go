package upstreamgovernance

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSQLGovernanceRechargeMigrationCASAndPersistentSimulation(t *testing.T) {
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
	schema := fmt.Sprintf("governance_recharge_%d", time.Now().UnixNano())
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
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "253_upstream_governance_automation.sql", "254_upstream_governance_recharge_plans.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql"} {
		raw, readErr := os.ReadFile("../../migrations/" + migration)
		require.NoError(t, readErr)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	store := NewSQLStore(db)
	cipher := fakeCipher{}
	session, err := cipher.Encrypt(`{"access_token":"fixture-session","user_id":5}`)
	require.NoError(t, err)
	site := &Site{Name: "Recharge fixture", BaseURL: "https://fixture.example", Platform: "sub2api", Enabled: true, IntervalMinutes: 15, SessionCipher: session, Status: "healthy", NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(t.Context(), site))
	svc := NewService(store, nil, nil, cipher, true)
	initial, err := svc.RechargePlan(t.Context(), site.ID)
	require.NoError(t, err)
	require.Zero(t, initial.Version)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM upstream_governance_recharge_plans`).Scan(&count))
	require.Zero(t, count, "GET must not create a policy row")
	policy := initial.Policy
	policy.Mode = "plan_only"
	saved, err := svc.ConfigureRechargePlan(t.Context(), site.ID, 0, policy)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Version)
	_, err = svc.ConfigureRechargePlan(t.Context(), site.ID, 0, policy)
	require.ErrorIs(t, err, ErrConflict)
	balance := 2.0
	snapshot := &Snapshot{SiteID: site.ID, SiteVersion: site.Version, Catalog: Catalog{Account: &RemoteAccount{UserID: 5, Balance: &balance, Unit: "usd"}}}
	require.NoError(t, store.SaveSnapshot(t.Context(), snapshot, nil))
	first, err := svc.EvaluateRechargePlan(t.Context(), site.ID)
	require.NoError(t, err)
	require.True(t, first.Evaluation.PolicyMatched)
	require.Equal(t, "blocked", first.Status)
	restarted := NewService(NewSQLStore(db), nil, nil, cipher, true)
	again, err := restarted.EvaluateRechargePlan(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, first.Evaluation.ID, again.Evaluation.ID)
	require.Equal(t, first.Version, again.Version)
	health, err := restarted.BalanceHealth(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, snapshot.CreatedAt, *health.ObservedAt)
	policy.Threshold = 3
	updated, err := restarted.ConfigureRechargePlan(t.Context(), site.ID, saved.Version, policy)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Version)
	require.Nil(t, updated.Evaluation)
	recordStore := store.(rechargeStore)
	require.ErrorIs(t, recordStore.SaveRechargeEvaluation(t.Context(), site.ID, saved.Version, RechargeState{}), ErrConflict)
	afterChange, err := restarted.EvaluateRechargePlan(t.Context(), site.ID)
	require.NoError(t, err)
	require.NotEqual(t, first.Evaluation.EpisodeID, afterChange.Evaluation.EpisodeID)
	// Corrupt execution modes cannot be stored even by a direct SQL writer.
	_, err = db.Exec(`UPDATE upstream_governance_recharge_plans SET policy=jsonb_set(policy,'{mode}','"execute"') WHERE site_id=$1`, site.ID)
	require.Error(t, err)
	unchanged, err := store.GetSite(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, site.Version, unchanged.Version)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count))
	require.Zero(t, count)
}
