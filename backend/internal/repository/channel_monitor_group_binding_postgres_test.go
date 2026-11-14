//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMonitorGroupBindingPostgresUsesCurrentKeyGroupForAllEnrichment(t *testing.T) {
	db := monitorIntelligencePostgres(t)
	_, err := db.Exec(`
ALTER TABLE groups ADD COLUMN rate_multiplier double precision DEFAULT 0.2;
CREATE TEMP TABLE user_group_rate_multipliers(group_id bigint,user_id bigint,rate_multiplier double precision);
CREATE TEMP TABLE usage_logs(group_id bigint,account_id bigint,requested_model text,model text,
 cache_read_tokens bigint,input_tokens bigint,cache_creation_tokens bigint,first_token_ms bigint,created_at timestamptz,actual_cost numeric);
INSERT INTO user_group_rate_multipliers VALUES (2,42,0.06);
UPDATE groups SET name='duplicate-name';
INSERT INTO api_keys VALUES (101,2,NULL);
UPDATE channel_monitors SET group_binding_mode='key',local_api_key_id=101,group_id=1 WHERE id=1;
INSERT INTO usage_logs VALUES
 (1,9,'usage-model','usage-model',20,80,0,1000,'2026-11-14T11:59:00Z',1),
 (2,9,'usage-model','usage-model',80,20,0,2000,'2026-11-14T11:59:00Z',1);`)
	require.NoError(t, err)
	repo := &channelMonitorRepository{db: db}
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
	rates, err := repo.ListUserRatesForMonitors(ctx, []int64{1}, 42)
	require.NoError(t, err)
	require.Equal(t, 0.06, rates[1], "use the current key group, even if the stored name and group snapshot are stale")
	metrics, err := repo.ListUsageMetricsForMonitors(ctx, []int64{1}, now)
	require.NoError(t, err)
	require.InDelta(t, 80, *metrics[1]["7d"].CacheRatePct, 0.001)
	require.Equal(t, 2000.0, *metrics[1]["7d"].FirstTokenMs)
	iq, err := repo.ListIntelligenceForMonitors(ctx, []int64{1}, now)
	require.NoError(t, err)
	require.Nil(t, iq[1], "the new group has no local IQ policy")
	_, err = db.Exec(`UPDATE api_keys SET group_id=1`)
	require.NoError(t, err)
	iq, err = repo.ListIntelligenceForMonitors(ctx, []int64{1}, now)
	require.NoError(t, err)
	require.Equal(t, "pass", iq[1].Series[0].Points[0].Status)
	_, err = db.Exec(`UPDATE api_keys SET deleted_at=now()`)
	require.NoError(t, err)
	rates, err = repo.ListUserRatesForMonitors(ctx, []int64{1}, 42)
	require.NoError(t, err)
	require.Empty(t, rates, "deleted keys must never fall back to a group snapshot or name")
	metrics, err = repo.ListUsageMetricsForMonitors(ctx, []int64{1}, now)
	require.NoError(t, err)
	require.Empty(t, metrics)
	iq, err = repo.ListIntelligenceForMonitors(ctx, []int64{1}, now)
	require.NoError(t, err)
	require.Empty(t, iq)
	_, err = db.Exec(`UPDATE channel_monitors SET group_binding_mode='legacy',local_api_key_id=NULL,group_id=1 WHERE id=1`)
	require.NoError(t, err)
	iq, err = repo.ListIntelligenceForMonitors(ctx, []int64{1}, now)
	require.NoError(t, err)
	require.NotNil(t, iq[1], "a uniquely migrated explicit ID remains usable while legacy credential repair is pending")
}

func TestMonitorGroupBindingPostgresScopesEveryHistoryProjection(t *testing.T) {
	db := monitorIntelligencePostgres(t)
	_, err := db.Exec(`CREATE TEMP TABLE channel_monitor_histories(id bigint,monitor_id bigint,model text,status text,
 latency_ms bigint,ping_latency_ms bigint,checked_at timestamptz,quota jsonb);
UPDATE channel_monitors SET group_binding_changed_at=now()-interval '30 seconds' WHERE id=1;
INSERT INTO channel_monitor_histories VALUES
 (1,1,'usage-model','failed',999,NULL,now()-interval '1 minute',NULL),
 (2,1,'usage-model','operational',333,NULL,now()-interval '10 seconds',NULL);`)
	require.NoError(t, err)
	repo := &channelMonitorRepository{db: db}
	ctx := context.Background()
	latest, err := repo.ListLatestPerModel(ctx, 1)
	require.NoError(t, err)
	require.Len(t, latest, 1)
	require.Equal(t, 333, *latest[0].LatencyMs)
	batch, err := repo.ListLatestForMonitorIDs(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, 333, *batch[1][0].LatencyMs)
	availability, err := repo.ComputeAvailability(ctx, 1, 7)
	require.NoError(t, err)
	require.Len(t, availability, 1)
	require.Equal(t, 1, availability[0].TotalChecks)
	require.Equal(t, 100.0, availability[0].AvailabilityPct)
	all, err := repo.ComputeAvailabilityForMonitors(ctx, []int64{1}, 7)
	require.NoError(t, err)
	require.Equal(t, 1, all[1][0].TotalChecks)
	models := map[int64]string{1: "usage-model"}
	history, err := repo.ListRecentHistoryForMonitors(ctx, []int64{1}, models, 60)
	require.NoError(t, err)
	require.Len(t, history[1], 1)
	require.Equal(t, 333, *history[1][0].LatencyMs)
	afterReset, err := repo.ComputeAvailabilityForMonitorsSince(ctx, []int64{1}, models, map[int64]time.Time{1: time.Now().Add(-2 * time.Minute)}, 7)
	require.NoError(t, err)
	require.Equal(t, 1, afterReset[1][0].TotalChecks)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM channel_monitor_histories`).Scan(&count))
	require.Equal(t, 2, count, "all historical evidence remains stored")
}

func TestMonitorGroupBindingPostgresMigrationOnlyBackfillsUniqueMatches(t *testing.T) {
	db := monitorIntelligencePostgres(t)
	_, err := db.Exec(`INSERT INTO groups VALUES
 (3,'unique','openai','active',NULL),(4,'duplicate','openai','active',NULL),(5,'duplicate','openai','active',NULL);
INSERT INTO channel_monitors(id,provider,group_name,enabled,group_binding_mode) VALUES
 (3,'openai','unique',TRUE,'legacy'),(4,'openai','duplicate',TRUE,'legacy');`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("291_channel_monitor_key_group_binding.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	var id *int64
	require.NoError(t, db.QueryRow(`SELECT group_id FROM channel_monitors WHERE id=3`).Scan(&id))
	require.NotNil(t, id)
	require.Equal(t, int64(3), *id)
	require.NoError(t, db.QueryRow(`SELECT group_id FROM channel_monitors WHERE id=4`).Scan(&id))
	require.Nil(t, id)
}
