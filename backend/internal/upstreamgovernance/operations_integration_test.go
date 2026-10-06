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

// No application configuration is read. This opt-in fixture owns a random
// schema and drops only that schema. Never point this at an application role.
func TestOperationsPostgresIntegration(t *testing.T) {
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
	schema := fmt.Sprintf("governance_operations_fixture_%d", time.Now().UnixNano())
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
	exec(`CREATE TABLE upstream_governance_sites(id BIGINT PRIMARY KEY,name TEXT NOT NULL,base_url TEXT NOT NULL,enabled BOOLEAN DEFAULT true,session_cipher TEXT DEFAULT 'fixture-cipher-canary',status TEXT DEFAULT 'connected',version BIGINT DEFAULT 1,interval_minutes INT DEFAULT 1,full_interval_seconds BIGINT DEFAULT 60,last_sync_at TIMESTAMPTZ,fast_observe_enabled BOOLEAN DEFAULT false,fast_interval_seconds BIGINT DEFAULT 5,fast_observe_status TEXT DEFAULT 'idle',last_fast_observe_at TIMESTAMPTZ,next_fast_observe_at TIMESTAMPTZ,fast_observe_source_user_id BIGINT DEFAULT 0,fast_observe_complete BOOLEAN DEFAULT false,fast_observe_revision BIGINT DEFAULT 0,balance_monitor JSONB DEFAULT '{}',balance_monitor_state JSONB DEFAULT '{}');
CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT NOT NULL,deleted_at TIMESTAMPTZ);
CREATE TABLE upstream_governance_bindings(id BIGINT PRIMARY KEY,site_id BIGINT,local_group_id BIGINT,local_group_ids JSONB);
CREATE TABLE upstream_governance_snapshots(id BIGSERIAL PRIMARY KEY,site_id BIGINT,site_version BIGINT,created_at TIMESTAMPTZ,catalog JSONB DEFAULT '{"account":{"user_id":1}}');
CREATE TABLE upstream_governance_keys(id BIGINT PRIMARY KEY,site_id BIGINT,key_health JSONB);
CREATE TABLE upstream_governance_events(id BIGINT PRIMARY KEY,site_id BIGINT,kind TEXT,resource TEXT DEFAULT '',before_value TEXT DEFAULT '',after_value TEXT DEFAULT '',acknowledged BOOLEAN DEFAULT false,created_at TIMESTAMPTZ)`)
	for _, name := range []string{"262_upstream_governance_change_notifications.sql", "263_upstream_governance_pricing_policies.sql"} {
		raw, e := os.ReadFile("../../migrations/" + name)
		require.NoError(t, e)
		exec(string(raw))
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	exec(`INSERT INTO upstream_governance_sites(id,name,base_url) VALUES(1,'Healthy','https://healthy.example.test'),(2,'Failing','https://failing.example.test'),(3,'Stopped','https://stopped.example.test'); UPDATE upstream_governance_sites SET status='error' WHERE id IN (2,3);UPDATE upstream_governance_sites SET enabled=false WHERE id=3`)
	exec(`INSERT INTO upstream_governance_snapshots(site_id,site_version,created_at) VALUES(1,1,$1),(2,1,$1),(3,1,$1)`, now)
	exec(`INSERT INTO upstream_governance_keys VALUES(1,2,'{"status":"confirmed_missing"}');INSERT INTO groups VALUES(12,'Shared',NULL),(13,'Other site only',NULL),(14,'Disabled protection',NULL);INSERT INTO upstream_governance_bindings VALUES(1,1,12,'[12]'),(2,2,12,'[12]'),(3,3,13,'[13]'),(4,1,14,'[14]')`)
	exec(`INSERT INTO upstream_governance_pricing_policies(local_group_id,enabled,baseline_cost,baseline_sale,protected,protection_reason) VALUES(12,true,0.2,0.3,true,'unknown_cost'),(13,false,0.2,0.3,true,'unknown_cost'),(14,false,0.2,0.3,true,'unknown_cost')`)
	exec(`INSERT INTO upstream_governance_events(id,site_id,kind,resource,before_value,after_value,acknowledged,created_at) VALUES(1,1,'sync_failed','','','old-failure-canary',false,$1),(2,1,'rate_changed','g','{"Name":"Group A","Resolved":0.8,"token":"payload-secret-canary"}','{"Name":"Group A","Resolved":0.9}',true,$1),(3,2,'sync_failed','','','other-site-canary',false,$1)`, now)
	exec(`INSERT INTO upstream_governance_pricing_operations(operation_id,local_group_id,policy_version,cost_fact_revision,before_sale,target_sale,before_cost,after_cost,status,protected,reason,created_at) VALUES('p1',12,1,1,0.3,0.4,0.2,0.25,'applied',false,'increase',$1),('p2',12,1,1,0.4,0.9,0.25,0.6,'protected',true,'increase_review',$1),('p3',13,1,1,0.3,0.4,0.2,0.25,'applied',false,'increase',$1)`, now)
	exec(`INSERT INTO upstream_governance_change_notifications(id,site_id,dedup_key,recipient,kind,subject,body,status,attempts,last_error,created_at) VALUES(1,1,'site:1:pricing:group:12:revision:1','recipient-secret@example.test','pricing_change','subject-secret-canary','body-secret-canary','pending',1,'smtp-secret-canary',$1),(2,1,'pending-baseline','recipient-secret@example.test','group_change','subject-secret-canary','body-secret-canary','pending',0,'',$1),(3,1,'sent-fixture','recipient-secret@example.test','group_change','subject-secret-canary','body-secret-canary','sent',1,'old-smtp-secret-canary',$1),(4,2,'other-retry','recipient-secret@example.test','group_change','subject-secret-canary','body-secret-canary','sending',1,'smtp-secret-canary',$1)`, now)
	s := &sqlStore{db: fixture}
	ctx := context.Background()
	tables := []string{"upstream_governance_sites", "upstream_governance_keys", "upstream_governance_snapshots", "upstream_governance_events", "upstream_governance_pricing_policies", "upstream_governance_pricing_operations", "upstream_governance_change_notifications"}
	snapshot := func() []string {
		t.Helper()
		out := []string{}
		for _, table := range tables {
			var value string
			e := fixture.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM ` + table + ` t`).Scan(&value)
			require.NoError(t, e)
			out = append(out, value)
		}
		return out
	}
	before := snapshot()
	board, err := s.ReadWorkbench(ctx, OperationsQuery{Page: 1, PageSize: 20}, now)
	require.NoError(t, err)
	require.Equal(t, int64(5), board.Total)
	require.Len(t, board.Items, 5)
	for _, item := range board.Items {
		require.NotEqual(t, int64(3), item.SiteID)
		if item.SiteID == 1 {
			require.NotEqual(t, "collection", item.Kind)
		}
		if item.Kind == "pricing" {
			require.Equal(t, "12", item.ResourceID)
			require.True(t, item.Shared)
			require.Equal(t, 2, item.ImpactCount)
		}
	}
	scoped, err := s.ReadWorkbench(ctx, OperationsQuery{SiteID: 1, Page: 1, PageSize: 1}, now)
	require.NoError(t, err)
	require.Equal(t, int64(2), scoped.Total)
	require.Len(t, scoped.Items, 1)
	all, err := s.ReadTimeline(ctx, OperationsQuery{SiteID: 1, Page: 1, PageSize: 20, Kind: "all"}, now)
	require.NoError(t, err)
	require.Equal(t, int64(7), all.Total)
	ids := []string{}
	for _, item := range all.Items {
		ids = append(ids, item.ID)
		require.Nil(t, item.RelatedRecordID)
		if item.Kind == "pricing" {
			require.True(t, item.Shared)
			if item.Status == "protected" {
				require.Nil(t, item.AfterRate)
			}
		}
		if item.Kind == "notification" && item.Status == "sent" {
			require.Equal(t, "notification_accepted", item.Reason)
		}
	}
	for page := 1; page <= len(ids); page++ {
		one, e := s.ReadTimeline(ctx, OperationsQuery{SiteID: 1, Page: page, PageSize: 1, Kind: "all"}, now)
		require.NoError(t, e)
		require.Equal(t, ids[page-1], one.Items[0].ID)
		require.Equal(t, int64(7), one.Total)
	}
	notes, err := s.ReadTimeline(ctx, OperationsQuery{SiteID: 1, Page: 1, PageSize: 20, Kind: "notification"}, now)
	require.NoError(t, err)
	require.Equal(t, int64(3), notes.Total)
	raw, err := json.Marshal(struct {
		Board    WorkbenchResult
		Timeline TimelineResult
	}{board, all})
	require.NoError(t, err)
	for _, secret := range []string{"secret-canary", "old-failure-canary", "other-site-canary", "recipient-secret", "fixture-cipher-canary"} {
		require.NotContains(t, string(raw), secret)
	}
	require.Equal(t, before, snapshot(), "both read models must leave all persisted records unchanged")
	// Further fixture-only changes exercise metadata that must not disappear
	// just because it is uncertain rather than confirmed-invalid.
	exec(`INSERT INTO upstream_governance_keys VALUES(2,1,'{"status":"suspected_missing","missing_count":1}'),(3,1,'{"status":"present","protection_error":"protection-secret-canary"}'),(4,1,'{"status":"present","error_code":"verification-secret-canary"}'),(5,1,'{"status":"unknown"}')`)
	board, err = s.ReadWorkbench(ctx, OperationsQuery{SiteID: 1, Page: 1, PageSize: 20}, now)
	require.NoError(t, err)
	keyItems := map[string]WorkbenchItem{}
	for _, item := range board.Items {
		if item.Kind == "key" {
			keyItems[item.Reason] = item
		}
	}
	require.Len(t, keyItems, 3)
	require.Equal(t, 1, keyItems["key_verification_pending"].ImpactCount)
	require.Equal(t, 2, keyItems["key_verification_unknown"].ImpactCount)
	require.Equal(t, 1, keyItems["key_protection_failed"].ImpactCount)
	for _, item := range keyItems {
		require.NotEqual(t, "critical", item.Severity)
	}
	exec(`UPDATE upstream_governance_sites SET fast_observe_enabled=true,fast_observe_status='healthy',last_fast_observe_at=$1,fast_observe_source_user_id=2,fast_observe_complete=true,fast_observe_revision=1 WHERE id=1`, now.Add(time.Second))
	fastReason := func() string {
		t.Helper()
		board, e := s.ReadWorkbench(ctx, OperationsQuery{SiteID: 1, Page: 1, PageSize: 20}, now)
		require.NoError(t, e)
		for _, item := range board.Items {
			if item.Kind == "fast_observation" {
				return item.Reason
			}
		}
		return ""
	}
	require.Equal(t, "observation_unknown", fastReason())
	exec(`UPDATE upstream_governance_sites SET fast_observe_source_user_id=1 WHERE id=1`)
	require.Empty(t, fastReason())
	exec(`UPDATE upstream_governance_sites SET version=2 WHERE id=1`)
	require.Equal(t, "observation_unknown", fastReason())
	exec(`UPDATE upstream_governance_sites SET version=1,last_fast_observe_at=$1 WHERE id=1`, now.Add(-time.Second))
	require.Equal(t, "observation_unknown", fastReason())
	before = snapshot()
	_, err = s.ReadWorkbench(ctx, OperationsQuery{Page: 1, PageSize: 20}, now)
	require.NoError(t, err)
	require.Equal(t, before, snapshot())
}
