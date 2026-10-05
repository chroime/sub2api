package upstreamgovernance

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

type ModelStatsPoint struct {
	At        time.Time `json:"at"`
	Samples   int64     `json:"samples"`
	Successes int64     `json:"successes"`
	Failures  int64     `json:"failures"`
	TTFTMS    *float64  `json:"ttft_ms"`
}
type ModelStatsGroup struct {
	ConfigHash      string            `json:"config_hash"`
	ManagedKeyID    int64             `json:"managed_key_id"`
	Model           string            `json:"model"`
	APIMode         string            `json:"api_mode"`
	Template        string            `json:"template"`
	Effort          string            `json:"effort"`
	TemplateVersion string            `json:"template_version"`
	AdapterVersion  string            `json:"adapter_version"`
	Comparable      bool              `json:"comparable"`
	Samples         int64             `json:"samples"`
	Successes       int64             `json:"successes"`
	Failures        int64             `json:"failures"`
	Unknown         int64             `json:"unknown"`
	TTFTSamples     int64             `json:"ttft_samples"`
	P50TTFTMS       *float64          `json:"p50_ttft_ms"`
	P95TTFTMS       *float64          `json:"p95_ttft_ms"`
	AvgDurationMS   *float64          `json:"avg_duration_ms"`
	LastRunAt       time.Time         `json:"last_run_at"`
	Points          []ModelStatsPoint `json:"points"`
}
type ModelStats struct {
	Days      int               `json:"days"`
	Groups    []ModelStatsGroup `json:"groups"`
	Truncated bool              `json:"truncated"`
}

// A baseline is the complete request configuration, immutable target identity
// and runner/template versions. Concurrency and output limits remain separate.
const modelStatsBase = `WITH samples AS (
 SELECT r.created_at,r.status,r.request,r.result,
 md5(jsonb_build_object('request',r.request-'sample','identity',b.identity,
 'template_version',r.result->>'template_version','adapter_version',r.result->>'adapter_version')::text) AS signature,
 CASE WHEN r.status='succeeded' THEN (r.result->>'ttft_ms')::double precision END AS ttft,
 CASE WHEN r.status='succeeded' THEN (r.result->>'duration_ms')::double precision END AS duration
 FROM upstream_governance_model_runs r JOIN upstream_governance_model_batches b ON b.id=r.batch_id AND b.site_id=r.site_id
 WHERE r.site_id=$1 AND r.created_at >= $2
 ) `

func (s *Service) ModelStats(ctx context.Context, siteID int64, days int) (*ModelStats, error) {
	if siteID <= 0 || (days != 1 && days != 7 && days != 30) {
		return nil, ErrInvalid
	}
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, err
	}
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	since := s.now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := m.db.QueryContext(ctx, modelStatsBase+`SELECT signature,request-'sample',COALESCE(result->>'template_version',''),COALESCE(result->>'adapter_version',''),
 count(*),count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status='failed'),count(*) FILTER(WHERE status NOT IN ('succeeded','failed')),
 count(ttft) FILTER(WHERE ttft>=0),percentile_cont(0.5) WITHIN GROUP(ORDER BY ttft) FILTER(WHERE ttft>=0),
 percentile_cont(0.95) WITHIN GROUP(ORDER BY ttft) FILTER(WHERE ttft>=0),avg(duration),max(created_at)
 FROM samples GROUP BY signature,request-'sample',result->>'template_version',result->>'adapter_version' ORDER BY max(created_at) DESC,signature LIMIT 51`, siteID, since)
	if err != nil {
		return nil, err
	}
	out := &ModelStats{Days: days, Groups: []ModelStatsGroup{}}
	indices := map[string]int{}
	for rows.Next() {
		var g ModelStatsGroup
		var request []byte
		if err = rows.Scan(&g.ConfigHash, &request, &g.TemplateVersion, &g.AdapterVersion, &g.Samples, &g.Successes, &g.Failures, &g.Unknown, &g.TTFTSamples, &g.P50TTFTMS, &g.P95TTFTMS, &g.AvgDurationMS, &g.LastRunAt); err != nil {
			rows.Close()
			return nil, err
		}
		if len(out.Groups) == 50 {
			out.Truncated = true
			continue
		}
		var req ModelRunRequest
		if err = json.Unmarshal(request, &req); err != nil {
			rows.Close()
			return nil, err
		}
		g.ManagedKeyID, g.Model, g.APIMode = req.Config.ManagedKeyID, req.Config.Model, req.Config.APIMode
		g.Template, g.Effort = req.Template, req.Effort
		g.Comparable = g.TemplateVersion != "" && g.AdapterVersion != ""
		if !g.Comparable || g.TTFTSamples < 5 {
			g.P50TTFTMS = nil
			g.P95TTFTMS = nil
		}
		g.Points = []ModelStatsPoint{}
		indices[g.ConfigHash] = len(out.Groups)
		out.Groups = append(out.Groups, g)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(out.Groups) == 0 {
		return out, nil
	}
	bucket := "hour"
	if days > 1 {
		bucket = "day"
	}
	signatures := make([]string, 0, len(out.Groups))
	for _, g := range out.Groups {
		signatures = append(signatures, g.ConfigHash)
	}
	rows, err = m.db.QueryContext(ctx, modelStatsBase+`SELECT signature,date_trunc($3,created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
 count(*),count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status='failed'),avg(ttft) FILTER(WHERE ttft>=0)
 FROM samples WHERE signature=ANY($4) GROUP BY signature,2 ORDER BY 2,signature`, siteID, since, bucket, pq.Array(signatures))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var signature string
		var point ModelStatsPoint
		if err = rows.Scan(&signature, &point.At, &point.Samples, &point.Successes, &point.Failures, &point.TTFTMS); err != nil {
			return nil, err
		}
		if i, ok := indices[signature]; ok {
			if !out.Groups[i].Comparable {
				point.TTFTMS = nil
			}
			out.Groups[i].Points = append(out.Groups[i].Points, point)
		}
	}
	return out, rows.Err()
}
