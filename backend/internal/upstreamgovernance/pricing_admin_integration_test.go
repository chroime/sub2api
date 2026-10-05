package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Opt-in only: never read application configuration or use its database.
// Every table and row is created in a fresh fixture-owned schema.
func TestPricingAdminPostgresIntegration(t *testing.T) {
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
	schema := fmt.Sprintf("governance_pricing_fixture_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer fixture.Close()
	exec := func(statement string, args ...any) {
		t.Helper()
		_, e := fixture.Exec(statement, args...)
		require.NoError(t, e)
	}
	exec(`CREATE TABLE upstream_governance_sites(id BIGINT PRIMARY KEY,name TEXT NOT NULL); CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT NOT NULL,rate_multiplier NUMERIC(20,8) NOT NULL,deleted_at TIMESTAMPTZ); CREATE TABLE upstream_governance_bindings(id BIGINT PRIMARY KEY,site_id BIGINT NOT NULL,remote_group_id TEXT NOT NULL,local_group_id BIGINT,local_group_ids JSONB,account_id BIGINT NOT NULL)`)
	for _, name := range []string{"263_upstream_governance_pricing_policies.sql", "264_upstream_governance_pricing_notifications.sql"} {
		raw, e := os.ReadFile("../../migrations/" + name)
		require.NoError(t, e)
		exec(string(raw))
	}
	exec(`INSERT INTO upstream_governance_sites VALUES(1,'Fixture A'),(2,'Fixture B'),(3,'Unrelated'); INSERT INTO groups(id,name,rate_multiplier) VALUES(12,'Shared',0.3),(13,'Other',0.7); INSERT INTO upstream_governance_bindings VALUES(3,1,'a',12,'[12]',4),(4,2,'b',12,'[12]',5),(5,1,'c',13,'[13]',6)`)
	exec(`INSERT INTO upstream_governance_pricing_policies(local_group_id,enabled,mode,baseline_cost,baseline_sale,manual_owner,manual_version,active_cost,protected,protection_reason) VALUES(12,true,'keep_margin',0.22,0.3,true,9,0.22,true,'manual_owner'),(13,false,'keep_margin',0.4,0.7,false,0,0.4,false,'')`)
	exec(`INSERT INTO upstream_governance_pricing_cost_facts(local_group_id,source_id,site_id,binding_id,cost,unit,currency,comparable,eligible,unknown) VALUES(12,'a',1,3,0.22,'multiplier','relative',true,true,false),(12,'b',2,4,0.20,'multiplier','relative',true,true,false),(13,'c',1,5,0.4,'multiplier','relative',true,true,false)`)
	s := &sqlStore{db: fixture}
	ctx := context.Background()
	draft, _ := pricingAdminFixture()
	snapshot := func(table string) string {
		t.Helper()
		var raw string
		e := fixture.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM ` + table + ` t`).Scan(&raw)
		require.NoError(t, e)
		return raw
	}
	beforePolicies, beforeFacts, beforeGroups := snapshot("upstream_governance_pricing_policies"), snapshot("upstream_governance_pricing_cost_facts"), snapshot("groups")
	preview, err := s.PreviewPricingPolicy(ctx, 1, 12, draft)
	require.NoError(t, err)
	require.Equal(t, .3385, *preview.TargetSale)
	require.Len(t, preview.Bindings, 2)
	require.Equal(t, beforePolicies, snapshot("upstream_governance_pricing_policies"))
	require.Equal(t, beforeFacts, snapshot("upstream_governance_pricing_cost_facts"))
	require.Equal(t, beforeGroups, snapshot("groups"))
	_, err = s.PreviewPricingPolicy(ctx, 3, 12, draft)
	require.ErrorIs(t, err, ErrNotFound)
	exec(`UPDATE upstream_governance_pricing_cost_facts SET observed_at=NOW(),revision=revision+1 WHERE local_group_id=12`)
	exec(`UPDATE upstream_governance_pricing_policies SET version=version+1,cost_fact_revision=cost_fact_revision+1 WHERE local_group_id=12`)
	refreshed, err := s.PreviewPricingPolicy(ctx, 1, 12, draft)
	require.NoError(t, err)
	require.Equal(t, preview.Fingerprint, refreshed.Fingerprint)
	config, err := s.SavePricingPolicy(ctx, 1, 12, draft, preview.Fingerprint)
	require.NoError(t, err)
	require.Len(t, config.Policies, 2)
	require.Equal(t, beforeGroups, snapshot("groups"), "saving policy must not change production sale")
	var saved PricingPolicy
	saved, err = scanPricingPolicy(fixture.QueryRow(`SELECT ` + pricingPolicyColumns + ` FROM upstream_governance_pricing_policies WHERE local_group_id=12`))
	require.NoError(t, err)
	require.True(t, saved.ManualOwner)
	require.Equal(t, int64(9), saved.ManualVersion)
	require.True(t, saved.Protected)
	require.Equal(t, "manual_owner", saved.ProtectionReason)
	require.Equal(t, .22, saved.ActiveCost)
	require.Equal(t, .25, saved.MinMargin)
	var otherVersion int64
	require.NoError(t, fixture.QueryRow(`SELECT version FROM upstream_governance_pricing_policies WHERE local_group_id=13`).Scan(&otherVersion))
	require.Equal(t, int64(1), otherVersion)
	var notificationCount int
	require.NoError(t, fixture.QueryRow(`SELECT count(*) FROM upstream_governance_pricing_notifications`).Scan(&notificationCount))
	require.Zero(t, notificationCount)
	policies := snapshot("upstream_governance_pricing_policies")
	config, err = s.SavePricingNotifications(ctx, 1, 1, PricingNotificationPolicy{Recipients: []string{"admin@example.test"}, PricingChanges: true})
	require.NoError(t, err)
	require.Equal(t, int64(2), config.Version)
	require.Equal(t, policies, snapshot("upstream_governance_pricing_policies"))
	_, err = s.SavePricingNotifications(ctx, 1, 1, PricingNotificationPolicy{})
	require.ErrorIs(t, err, ErrConflict)
	preview, err = s.PreviewPricingPolicy(ctx, 1, 12, draft)
	require.NoError(t, err)
	exec(`UPDATE upstream_governance_pricing_cost_facts SET cost=0.24 WHERE source_id='a'`)
	_, err = s.SavePricingPolicy(ctx, 1, 12, draft, preview.Fingerprint)
	require.ErrorIs(t, err, ErrConflict)
	exec(`UPDATE upstream_governance_pricing_cost_facts SET unknown=true WHERE source_id='a'`)
	preview, err = s.PreviewPricingPolicy(ctx, 1, 12, draft)
	require.NoError(t, err)
	require.True(t, preview.Blocked)
	require.Nil(t, preview.TargetSale)
	_, err = s.SavePricingPolicy(ctx, 1, 12, draft, preview.Fingerprint)
	require.ErrorIs(t, err, ErrPricingUnknownCost)
	draft.Enabled = false
	preview, err = s.PreviewPricingPolicy(ctx, 1, 12, draft)
	require.NoError(t, err)
	_, err = s.SavePricingPolicy(ctx, 1, 12, draft, preview.Fingerprint)
	require.NoError(t, err)
	// Confirm the fixture audit snapshot itself remains valid JSON, rather than
	// relying on ordering of SQL rows or non-semantic whitespace.
	require.True(t, json.Valid([]byte(snapshot("upstream_governance_pricing_policies"))))
}
