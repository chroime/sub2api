//go:build unit

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func monitorUsagePostgres(t *testing.T) (*sql.DB, time.Time) {
	t.Helper()
	db := monitorIntelligencePostgres(t)
	_, err := db.Exec(`CREATE TEMP TABLE usage_logs(
 id bigint,group_id bigint,account_id bigint,requested_model text,model text,
 cache_read_tokens bigint,input_tokens bigint,cache_creation_tokens bigint,
 first_token_ms bigint,created_at timestamptz,actual_cost numeric,stream boolean,
 upstream_input_tokens bigint,
 upstream_cache_read_tokens bigint DEFAULT 0,upstream_cache_creation_tokens bigint DEFAULT 0,
 upstream_cache_creation_5m_tokens bigint DEFAULT 0,upstream_cache_creation_1h_tokens bigint DEFAULT 0,
 cache_creation_5m_tokens bigint DEFAULT 0,cache_creation_1h_tokens bigint DEFAULT 0,
 cache_creation_30m_tokens bigint DEFAULT 0,cache_creation_12h_tokens bigint DEFAULT 0);
UPDATE channel_monitors SET primary_model='usage-model';`)
	require.NoError(t, err)
	return db, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
}

func recordFixtureUpstreamInput(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`UPDATE usage_logs SET upstream_input_tokens=input_tokens`)
	require.NoError(t, err)
}

func TestMonitorUsagePostgresLatest24StreamingTokenWeight(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`INSERT INTO usage_logs
SELECT n,1,9,'usage-model','mapped-model',CASE WHEN n%2=0 THEN 900 ELSE 0 END,
 100,0,n*100,$1::timestamptz-interval '1 minute',1,TRUE
FROM generate_series(1,26) n;`, now)
	require.NoError(t, err)
	_, err = db.Exec(`
-- Newer synchronous, other model/group and unbilled rows must not displace samples.
INSERT INTO usage_logs VALUES
 (100,1,9,'usage-model','usage-model',1000,0,0,999999,$1::timestamptz-interval '1 second',1,FALSE),
 (101,1,9,'other-model','usage-model',1000,0,0,999999,$1::timestamptz-interval '1 second',1,TRUE),
 (102,2,9,'usage-model','usage-model',1000,0,0,999999,$1::timestamptz-interval '1 second',1,TRUE),
 (103,1,9,'usage-model','usage-model',1000,0,0,999999,$1::timestamptz-interval '1 second',0,TRUE),
 (104,1,9,'usage-model','usage-model',1000,0,0,999999,$1::timestamptz+interval '1 second',1,TRUE);`, now)
	require.NoError(t, err)
	recordFixtureUpstreamInput(t, db)
	metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	for _, window := range []string{"7d", "15d", "30d"} {
		metric := metrics[1][window]
		require.NotNil(t, metric.CacheRatePct)
		require.InDelta(t, 100.0*10800/13200, *metric.CacheRatePct, 0.00001, "zero-hit input belongs in the weighted denominator")
		require.NotNil(t, metric.FirstTokenMs)
		require.Equal(t, 1450.0, *metric.FirstTokenMs, "equal timestamps use descending IDs to select records 3..26")
	}
}

func TestMonitorUsagePostgresSparseAndInvalidMeasurements(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`INSERT INTO usage_logs VALUES
 (1,1,9,'usage-model','mapped',60,20,20,0,$1::timestamptz-interval '40 days',1,TRUE),
 (2,1,9,'','usage-model',0,100,0,200,$1::timestamptz-interval '35 days',1,TRUE),
 (3,1,9,NULL,'usage-model',0,0,0,NULL,$1::timestamptz-interval '34 days',1,TRUE),
 (4,1,9,'usage-model','usage-model',0,0,0,-1,$1::timestamptz-interval '33 days',1,TRUE);`, now)
	require.NoError(t, err)
	recordFixtureUpstreamInput(t, db)
	metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1, 2}, now)
	require.NoError(t, err)
	require.NotNil(t, metrics[1]["7d"].CacheRatePct)
	require.InDelta(t, 30, *metrics[1]["7d"].CacheRatePct, 0.00001)
	require.Equal(t, 100.0, *metrics[1]["7d"].FirstTokenMs, "zero is measured; null/negative times are omitted")
	require.Equal(t, metrics[1]["7d"], metrics[1]["30d"], "availability windows do not restrict recent request samples")
	require.Nil(t, metrics[2], "no samples must not produce fabricated zero metrics")
}

func TestMonitorUsagePostgresDoesNotBackfillInvalidRecentSamples(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`INSERT INTO usage_logs
SELECT n,1,9,'usage-model','usage-model',CASE WHEN n=25 THEN 100 ELSE 0 END,
 0,0,CASE WHEN n=25 THEN 1000 ELSE NULL END,$1::timestamptz-n*interval '1 second',1,TRUE
FROM generate_series(1,25) n;`, now)
	require.NoError(t, err)
	recordFixtureUpstreamInput(t, db)
	metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.Contains(t, metrics, int64(1))
	require.Nil(t, metrics[1]["7d"].CacheRatePct)
	require.Nil(t, metrics[1]["7d"].FirstTokenMs)
}

func TestMonitorUsagePostgresDoesNotBackfillCacheFreeRecentSamples(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`INSERT INTO usage_logs
SELECT n,1,9,'usage-model','usage-model',CASE WHEN n=25 THEN 100 ELSE 0 END,
 100,0,n*100,$1::timestamptz-n*interval '1 second',1,TRUE
FROM generate_series(1,25) n;`, now)
	require.NoError(t, err)
	recordFixtureUpstreamInput(t, db)
	metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.Contains(t, metrics, int64(1))
	require.NotNil(t, metrics[1]["7d"].CacheRatePct)
	require.Zero(t, *metrics[1]["7d"].CacheRatePct, "valid zero-hit input is measured zero; do not backfill older hits")
	require.NotNil(t, metrics[1]["7d"].FirstTokenMs)
	require.Equal(t, 1250.0, *metrics[1]["7d"].FirstTokenMs, "cache-free requests still contribute to first-token time")
}

func TestMonitorUsagePostgresInputTokensDetermineWeightIncludingMisses(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`INSERT INTO usage_logs VALUES
 (1,1,9,'usage-model','usage-model',900,100,0,100,$1::timestamptz-interval '3 seconds',1,TRUE),
 (2,1,9,'usage-model','usage-model',5,3,2,300,$1::timestamptz-interval '2 seconds',1,TRUE),
 (3,1,9,'usage-model','usage-model',0,1000,0,500,$1::timestamptz-interval '1 second',1,TRUE);`, now)
	require.NoError(t, err)
	recordFixtureUpstreamInput(t, db)
	metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.NotNil(t, metrics[1]["7d"].CacheRatePct)
	require.InDelta(t, 100.0*905/2010, *metrics[1]["7d"].CacheRatePct, 0.00001, "weight complete input, including the zero-hit request")
	require.NotNil(t, metrics[1]["7d"].FirstTokenMs)
	require.Equal(t, 300.0, *metrics[1]["7d"].FirstTokenMs)
}

func TestMonitorUsagePostgresAccountAndBindingIsolation(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`UPDATE channel_monitors SET account_id=9 WHERE id=1`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO usage_logs VALUES
 (1,1,9,'usage-model','usage-model',20,80,0,100,$1::timestamptz-interval '1 minute',1,TRUE),
 (2,1,10,'usage-model','usage-model',100,0,0,9999,$1::timestamptz-interval '1 second',1,TRUE);`, now)
	require.NoError(t, err)
	recordFixtureUpstreamInput(t, db)
	repo := &channelMonitorRepository{db: db}
	metrics, err := repo.ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.Equal(t, 20.0, *metrics[1]["7d"].CacheRatePct)
	require.Equal(t, 100.0, *metrics[1]["7d"].FirstTokenMs)
	_, err = db.Exec(`UPDATE groups SET status='disabled' WHERE id=1`)
	require.NoError(t, err)
	metrics, err = repo.ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.Empty(t, metrics)
}

func TestMonitorUsagePostgresUsesRawUpstreamTokensAndCountsTTLOneTime(t *testing.T) {
	db, now := monitorUsagePostgres(t)
	_, err := db.Exec(`INSERT INTO usage_logs VALUES
 (1,1,9,'usage-model','usage-model',5,9999,50,100,$1::timestamptz-interval '3 seconds',1,TRUE),
 (2,1,9,'usage-model','usage-model',0,9999,0,200,$1::timestamptz-interval '2 seconds',1,TRUE),
 (3,1,9,'usage-model','usage-model',100,100,0,300,$1::timestamptz-interval '1 second',1,TRUE);`, now)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE usage_logs SET upstream_input_tokens=100,upstream_cache_read_tokens=800,
 upstream_cache_creation_tokens=100,upstream_cache_creation_5m_tokens=60,upstream_cache_creation_1h_tokens=40 WHERE id=1;
UPDATE usage_logs SET upstream_input_tokens=1000 WHERE id=2;`)
	require.NoError(t, err)
	metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
	require.NoError(t, err)
	require.NotNil(t, metrics[1]["7d"].CacheRatePct)
	require.InDelta(t, 40, *metrics[1]["7d"].CacheRatePct, 0.00001, "800 / (100 + 800 + 100 + 1000); billed tokens and missing input excluded")
	require.Equal(t, 200.0, *metrics[1]["7d"].FirstTokenMs, "cache eligibility must not change TTFT samples")
}

func TestMonitorUsagePostgresMissingOrConflictingUpstreamIsUnavailable(t *testing.T) {
	for _, measurement := range []string{
		`NULL,0,0,0,0`,
		`0,0,0,0,0`,
		`100,800,100,60,30`,
		`100,-1,0,0,0`,
	} {
		t.Run(measurement, func(t *testing.T) {
			db, now := monitorUsagePostgres(t)
			_, err := db.Exec(`INSERT INTO usage_logs(id,group_id,account_id,requested_model,model,cache_read_tokens,input_tokens,cache_creation_tokens,first_token_ms,created_at,actual_cost,stream,upstream_input_tokens,upstream_cache_read_tokens,upstream_cache_creation_tokens,upstream_cache_creation_5m_tokens,upstream_cache_creation_1h_tokens)
VALUES(1,1,9,'usage-model','usage-model',100,100,0,100,$1,1,TRUE,`+measurement+`)`, now.Add(-time.Second))
			require.NoError(t, err)
			metrics, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{1}, now)
			require.NoError(t, err)
			require.Nil(t, metrics[1]["7d"].CacheRatePct, "never substitute adjusted billing input for invalid raw measurements")
		})
	}
}

func TestMonitorUsagePostgresStreamingIndexes(t *testing.T) {
	db, _ := monitorUsagePostgres(t)
	content, err := migrations.FS.ReadFile("294_channel_monitor_stream_samples_notx.sql")
	require.NoError(t, err)
	// Temporary fixtures exercise the same index definitions without concurrent
	// builds (PostgreSQL does not support concurrent indexes on temporary tables).
	ddl := strings.ReplaceAll(string(content), "CONCURRENTLY ", "")
	for range 2 {
		_, err = db.Exec(ddl)
		require.NoError(t, err, "index migration must be repeatable")
	}
	_, err = db.Exec(`SET enable_seqscan=off`)
	require.NoError(t, err)
	for _, tc := range []struct{ predicate, index string }{
		{"", "idx_usage_logs_monitor_stream_recent"},
		{" AND account_id=9", "idx_usage_logs_monitor_account_stream_recent"},
	} {
		rows, err := db.Query(`EXPLAIN (COSTS OFF) SELECT first_token_ms FROM usage_logs
WHERE group_id=1 AND COALESCE(NULLIF(BTRIM(requested_model),''),NULLIF(BTRIM(model),''))='usage-model'
 AND stream=TRUE AND actual_cost>0` + tc.predicate + ` ORDER BY created_at DESC,id DESC LIMIT 24`)
		require.NoError(t, err)
		var lines []string
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			lines = append(lines, line)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		plan := strings.Join(lines, "\n")
		require.Contains(t, plan, tc.index)
		require.NotContains(t, plan, "Sort", "latest records should come directly from the ordered partial index")
	}
}
