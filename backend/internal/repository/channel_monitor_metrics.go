package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Use explicit, active group IDs only; an unbound monitor has no inferred
// usage or billing multiplier. The cache ratio
// uses aggregate tokens (the same denominator as channel monitor V2).
const monitorUsageMetricsQuery = `
SELECT m.id,w.days,
 100.0*SUM(ul.cache_read_tokens)/NULLIF(SUM(ul.input_tokens::bigint+ul.cache_creation_tokens+ul.cache_read_tokens),0),
 AVG(ul.first_token_ms) FILTER (WHERE ul.first_token_ms >= 0)
FROM channel_monitors m
` + monitorBoundGroupJoin + `
JOIN usage_logs ul ON ul.group_id=g.id
 AND COALESCE(NULLIF(TRIM(ul.requested_model),''),NULLIF(TRIM(ul.model),''))=m.primary_model
 AND (m.account_id IS NULL OR ul.account_id=m.account_id)
 AND ul.created_at >= $2::timestamptz-INTERVAL '30 days' AND ul.created_at < $2
CROSS JOIN (VALUES (7),(15),(30)) AS w(days)
WHERE m.id=ANY($1) AND m.enabled=TRUE
 AND ul.created_at >= $2::timestamptz-w.days*INTERVAL '1 day'
 AND ` + usageLogSuccessFilterUL + `
GROUP BY m.id,w.days`

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
		var days int
		var cacheRate, firstToken sql.NullFloat64
		if err := rows.Scan(&id, &days, &cacheRate, &firstToken); err != nil {
			return nil, err
		}
		metric := service.MonitorUsageMetric{}
		if cacheRate.Valid {
			metric.CacheRatePct = &cacheRate.Float64
		}
		if firstToken.Valid {
			metric.FirstTokenMs = &firstToken.Float64
		}
		if out[id] == nil {
			out[id] = make(map[string]service.MonitorUsageMetric, 3)
		}
		out[id][strconv.Itoa(days)+"d"] = metric
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
