package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Use explicit, active group IDs only; an unbound monitor has no inferred
// usage or billing multiplier. Both metrics use the same latest 24 streaming
// records, independent of availability windows. Cache rate weights actual
// upstream input tokens; TTFT remains the mean of measured request latencies.
const monitorStreamingSampleSelect = `
 SELECT ul.input_tokens,ul.upstream_input_tokens,
 ul.cache_creation_tokens,ul.cache_read_tokens,ul.first_token_ms,
 ul.cache_creation_5m_tokens,ul.cache_creation_1h_tokens,
 ul.cache_creation_30m_tokens,ul.cache_creation_12h_tokens,
 ul.upstream_cache_read_tokens,ul.upstream_cache_creation_tokens,
 ul.upstream_cache_creation_5m_tokens,ul.upstream_cache_creation_1h_tokens
 FROM usage_logs ul
 WHERE ul.group_id=g.id AND ul.stream=TRUE
 AND COALESCE(NULLIF(BTRIM(ul.requested_model),''),NULLIF(BTRIM(ul.model),''))=m.primary_model
 AND ul.created_at < $2::timestamptz
 AND ` + usageLogSuccessFilterUL

const monitorStreamingSampleOrder = `
 ORDER BY ul.created_at DESC,ul.id DESC
 LIMIT 24`

// Separate guarded branches let PostgreSQL use the account-specific index
// without an OR predicate that scans other accounts' streaming history.
// Filter cache eligibility only after selecting the latest 24 requests;
// cache-free requests must not be replaced with older cache-bearing requests.
const monitorUsageMetricsQuery = `
SELECT m.id,
 100.0*SUM(tokens.cache_read_tokens) FILTER (WHERE measurement.valid)
 /NULLIF(SUM(tokens.input_tokens+tokens.cache_read_tokens+GREATEST(tokens.cache_creation_tokens,tokens.ttl_creation_tokens))
  FILTER (WHERE measurement.valid),0),
 AVG(ul.first_token_ms) FILTER (WHERE ul.first_token_ms >= 0)
FROM channel_monitors m
` + monitorBoundGroupJoin + `
JOIN LATERAL (
 (` + monitorStreamingSampleSelect + ` AND m.account_id IS NULL` + monitorStreamingSampleOrder + `)
 UNION ALL
 (` + monitorStreamingSampleSelect + ` AND m.account_id IS NOT NULL AND ul.account_id=m.account_id` + monitorStreamingSampleOrder + `)
) ul ON TRUE
CROSS JOIN LATERAL (
 SELECT (ul.upstream_cache_read_tokens<>0 OR ul.upstream_cache_creation_tokens<>0
  OR ul.upstream_cache_creation_5m_tokens<>0 OR ul.upstream_cache_creation_1h_tokens<>0) AS present
) upstream_cache
CROSS JOIN LATERAL (
 SELECT ul.upstream_input_tokens::bigint AS input_tokens,
 CASE WHEN upstream_cache.present THEN ul.upstream_cache_read_tokens ELSE ul.cache_read_tokens END::bigint AS cache_read_tokens,
 CASE WHEN upstream_cache.present THEN ul.upstream_cache_creation_tokens ELSE ul.cache_creation_tokens END::bigint AS cache_creation_tokens,
 CASE WHEN upstream_cache.present THEN ul.upstream_cache_creation_5m_tokens::bigint+ul.upstream_cache_creation_1h_tokens
  ELSE ul.cache_creation_5m_tokens::bigint+ul.cache_creation_1h_tokens+ul.cache_creation_30m_tokens+ul.cache_creation_12h_tokens END AS ttl_creation_tokens,
 CASE WHEN upstream_cache.present THEN ul.upstream_cache_creation_5m_tokens>=0 AND ul.upstream_cache_creation_1h_tokens>=0
  ELSE ul.cache_creation_5m_tokens>=0 AND ul.cache_creation_1h_tokens>=0 AND ul.cache_creation_30m_tokens>=0 AND ul.cache_creation_12h_tokens>=0 END AS ttl_valid
) tokens
CROSS JOIN LATERAL (
 SELECT tokens.input_tokens IS NOT NULL AND tokens.input_tokens>=0
  AND NOT(tokens.input_tokens=0 AND ul.input_tokens>0)
  AND tokens.cache_read_tokens>=0 AND tokens.cache_creation_tokens>=0 AND tokens.ttl_valid
  AND NOT(tokens.cache_creation_tokens>0 AND tokens.ttl_creation_tokens>0 AND tokens.cache_creation_tokens<>tokens.ttl_creation_tokens) AS valid
) measurement
WHERE m.id=ANY($1) AND m.enabled=TRUE
GROUP BY m.id`

const monitorUserRatesQuery = `
SELECT m.id,COALESCE(ugr.rate_multiplier,g.rate_multiplier)
FROM channel_monitors m
` + monitorBoundGroupJoin + `
LEFT JOIN user_group_rate_multipliers ugr ON ugr.group_id=g.id AND ugr.user_id=$2
WHERE m.id=ANY($1) AND m.enabled=TRUE`

func (r *channelMonitorRepository) ListUsageMetricsForMonitors(ctx context.Context, ids []int64, now time.Time) (map[int64]map[string]service.MonitorUsageMetric, error) {
	out := make(map[int64]map[string]service.MonitorUsageMetric)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, monitorUsageMetricsQuery, pq.Array(ids), now)
	if err != nil {
		return nil, fmt.Errorf("query monitor usage metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var cacheRate, firstToken sql.NullFloat64
		if err := rows.Scan(&id, &cacheRate, &firstToken); err != nil {
			return nil, err
		}
		metric := service.MonitorUsageMetric{}
		if cacheRate.Valid {
			metric.CacheRatePct = &cacheRate.Float64
		}
		if firstToken.Valid {
			metric.FirstTokenMs = &firstToken.Float64
		}
		// Keep the existing response shape; only availability uses date windows.
		out[id] = map[string]service.MonitorUsageMetric{"7d": metric, "15d": metric, "30d": metric}
	}
	return out, rows.Err()
}

func (r *channelMonitorRepository) ListUserRatesForMonitors(ctx context.Context, ids []int64, userID int64) (map[int64]float64, error) {
	out := make(map[int64]float64)
	if len(ids) == 0 || userID <= 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, monitorUserRatesQuery, pq.Array(ids), userID)
	if err != nil {
		return nil, fmt.Errorf("query monitor user rates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var rate float64
		if err := rows.Scan(&id, &rate); err != nil {
			return nil, err
		}
		out[id] = rate
	}
	return out, rows.Err()
}
