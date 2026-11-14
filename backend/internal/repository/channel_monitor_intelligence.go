package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Local group policies are the sole source of channel IQ configuration. Account
// diagnostics and upstream-site tests do not describe the local group's quality.
// Match the exact group/platform/model/effort, and publish only verdicts from
// visible, finished candy/Pelican runs. A cadence-only policy update retains
// checks with the same evaluation config. Generation success alone never passes
// an IQ check. Keep the last 60 actual checks per series, including older checks
// and separate samples completed in the same minute.
const monitorIntelligenceQuery = `
WITH configured AS (
 SELECT DISTINCT m.id AS monitor_id,p.id AS policy_id,p.site_id,p.version,p.config,
  p.config->>'model' AS model,e.effort
 FROM channel_monitors m
 ` + monitorBoundGroupJoin + `
 JOIN upstream_governance_model_policies p
  ON p.config->>'target_type'='local_group'
  AND p.config->>'local_group_id'=g.id::text
  AND p.config->>'platform'=g.platform
 JOIN upstream_governance_sites s ON s.id=p.site_id AND s.status='local_model'
  AND p.config->>'target_owner_user_id' ~ '^[1-9][0-9]*$'
  AND s.login_cipher='local-model-owner:'||(p.config->>'target_owner_user_id')
 CROSS JOIN LATERAL jsonb_array_elements_text(
  CASE WHEN jsonb_typeof(p.config->'efforts')='array' THEN p.config->'efforts' ELSE '[]'::jsonb END
 ) AS e(effort)
 WHERE m.id=ANY($1) AND m.enabled=TRUE
  AND NULLIF(TRIM(p.config->>'model'),'') IS NOT NULL
  AND p.config->'templates' ?| ARRAY['candy','pelican']
), targets AS (
 SELECT DISTINCT monitor_id,model,effort FROM configured
)
SELECT t.monitor_id,t.model,t.effort,history.finished_at,history.verdict,history.id
FROM targets t
LEFT JOIN LATERAL (
 SELECT sample.* FROM configured c
 CROSS JOIN LATERAL (
 SELECT r.id,r.finished_at,
  CASE
   WHEN r.status<>'succeeded' THEN 'error'
   WHEN r.review IN ('pass','fail') THEN r.review
   WHEN r.request->>'template'='candy' AND r.result->>'candy_verdict'='numeric_correct' THEN 'pass'
   WHEN r.request->>'template'='candy' AND r.result->>'candy_verdict'='numeric_incorrect' THEN 'fail'
   ELSE 'pending' END AS verdict
 FROM upstream_governance_model_runs r
 JOIN upstream_governance_model_batches b ON b.id=r.batch_id AND b.site_id=c.site_id
  AND b.policy_id=c.policy_id AND b.policy_version<=c.version
  AND (b.policy_version=c.version OR b.config=c.config)
 WHERE r.policy_id=c.policy_id AND r.site_id=c.site_id
  AND r.request->'config'->>'target_type'='local_group'
  AND r.request->'config'->>'local_group_id'=c.config->>'local_group_id'
  AND r.request->'config'->>'platform'=c.config->>'platform'
  AND r.request->'config'->>'model'=c.model AND r.request->>'effort'=c.effort
  AND r.request->>'template' IN ('candy','pelican')
  AND c.config->'templates' ? (r.request->>'template')
  AND r.public_visible AND r.status IN ('succeeded','failed','indeterminate')
  AND r.finished_at <= $2::timestamptz
 ORDER BY r.finished_at DESC,r.id DESC LIMIT 60
 ) sample
 WHERE c.monitor_id=t.monitor_id AND c.model=t.model AND c.effort=t.effort
 ORDER BY sample.finished_at DESC,sample.id DESC LIMIT 60
) history ON TRUE
ORDER BY t.monitor_id,t.model,t.effort,history.finished_at DESC NULLS LAST,history.id DESC`

func (r *channelMonitorRepository) ListIntelligenceForMonitors(ctx context.Context, ids []int64, now time.Time) (map[int64]*service.MonitorIntelligence, error) {
	out := make(map[int64]*service.MonitorIntelligence)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, monitorIntelligenceQuery, pq.Array(ids), now)
	if err != nil {
		return nil, fmt.Errorf("query monitor intelligence: %w", err)
	}
	defer rows.Close()
	type seriesKey struct {
		monitorID int64
		model     string
		effort    string
	}
	indices := make(map[seriesKey]int)
	for rows.Next() {
		var id int64
		var model, effort string
		var status, checkID sql.NullString
		var checkedAt sql.NullTime
		if err := rows.Scan(&id, &model, &effort, &checkedAt, &status, &checkID); err != nil {
			return nil, err
		}
		summary := out[id]
		if summary == nil {
			summary = &service.MonitorIntelligence{WindowEnd: now, Series: []service.MonitorIntelligenceSeries{}}
			out[id] = summary
		}
		key := seriesKey{id, model, effort}
		index, exists := indices[key]
		if !exists {
			index = len(summary.Series)
			indices[key] = index
			summary.Series = append(summary.Series, service.MonitorIntelligenceSeries{Model: model, Effort: effort, Points: []service.MonitorIntelligencePoint{}})
		}
		if checkedAt.Valid && status.Valid {
			summary.Series[index].Points = append(summary.Series[index].Points, service.MonitorIntelligencePoint{ID: checkID.String, Status: status.String, CheckedAt: checkedAt.Time})
		}
	}
	return out, rows.Err()
}
