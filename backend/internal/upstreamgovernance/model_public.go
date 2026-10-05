package upstreamgovernance

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	maxIQHTMLBytes      = 512 * 1024
	maxIQHTMLTotalBytes = 2 * 1024 * 1024
)

// Reduce all runs in the requested time window in Postgres. A successful HTTP
// request is not a passing candy result: only a validated correct answer passes.
// Queued/running/cancelled/skipped runs are not quality evidence.
const iqTimelineQuery = `SELECT to_timestamp(floor(extract(epoch FROM r.created_at)/1800)*1800) AS bucket_at,
MAX(CASE
 WHEN r.status IN ('failed','indeterminate') THEN 2
 WHEN r.status='succeeded' AND r.request->>'template'='candy' AND COALESCE(r.result->>'candy_verdict','') NOT IN ('numeric_correct','pass') THEN 2
 WHEN r.status='succeeded' AND (r.request->>'template'='pelican' OR r.result->>'candy_verdict' IN ('numeric_correct','pass')) THEN 1
 ELSE 0 END) AS quality_status
FROM upstream_governance_model_runs r
WHERE r.created_at >= $1 AND r.created_at <= $2 AND r.request->>'template' IN ('candy','pelican')
GROUP BY bucket_at ORDER BY bucket_at`

// IQDashboard is a credential-free projection of model monitoring results for
// the authenticated user-facing IQ page. It intentionally omits prompts,
// request bodies, API keys, session data and raw JSON evidence.
type IQDashboard struct {
	CandyResults   []IQCandyResult   `json:"candy_results"`
	PelicanWorks   []IQPelicanWork   `json:"pelican_works"`
	Timeline       []IQTimelinePoint `json:"timeline"`
	StandardAnswer int               `json:"standard_answer"`
	WindowHours    int               `json:"window_hours"`
	GeneratedAt    time.Time         `json:"generated_at"`
}

type IQCandyResult struct {
	ID              string    `json:"id"`
	SiteName        string    `json:"site_name"`
	GroupName       string    `json:"group_name"`
	AccountLabel    string    `json:"account_label"`
	Model           string    `json:"model"`
	Effort          string    `json:"effort"`
	Answer          *int      `json:"answer,omitempty"`
	Verdict         string    `json:"verdict"`
	Status          string    `json:"status"`
	DurationMS      int64     `json:"duration_ms"`
	CreatedAt       time.Time `json:"created_at"`
	TemplateVersion string    `json:"template_version,omitempty"`
}

type IQPelicanWork struct {
	ID           string    `json:"id"`
	SiteName     string    `json:"site_name"`
	GroupName    string    `json:"group_name"`
	AccountLabel string    `json:"account_label"`
	Model        string    `json:"model"`
	Effort       string    `json:"effort"`
	HTML         string    `json:"html"`
	Status       string    `json:"status"`
	DurationMS   int64     `json:"duration_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

type IQTimelinePoint struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"` // pass, fail or empty
}

type iqTimelineSample struct {
	At     time.Time
	Status string
}

// PublicIQDashboard returns only model quality artifacts that are safe to show
// outside the administrator workbench. Model monitoring remains opt-in and an
// empty result is a valid response when no administrator run has completed.
// The timeline query is deliberately separate from the two detail queries:
// candy and pelican each receive their own limit, while every run in the time
// window contributes to the status timeline.
func (s *Service) PublicIQDashboard(ctx context.Context, hours, limit int) (*IQDashboard, error) {
	if hours <= 0 || hours > 168 || limit <= 0 || limit > 20 {
		return nil, ErrInvalid
	}
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	since := now.Add(-time.Duration(hours) * time.Hour)
	dashboard := &IQDashboard{CandyResults: []IQCandyResult{}, PelicanWorks: []IQPelicanWork{}, StandardAnswer: 21, WindowHours: hours, GeneratedAt: now}

	timelineRows, err := m.db.QueryContext(ctx, iqTimelineQuery, since, now)
	if err != nil {
		return nil, err
	}
	timelineSamples := make([]iqTimelineSample, 0)
	for timelineRows.Next() {
		var at time.Time
		var quality int
		if err = timelineRows.Scan(&at, &quality); err != nil {
			timelineRows.Close()
			return nil, err
		}
		status := "empty"
		if quality == 2 {
			status = "fail"
		} else if quality == 1 {
			status = "pass"
		}
		timelineSamples = append(timelineSamples, iqTimelineSample{At: at, Status: status})
	}
	if err = timelineRows.Err(); err != nil {
		timelineRows.Close()
		return nil, err
	}
	timelineRows.Close()

	candyRows, err := m.db.QueryContext(ctx, `SELECT r.id,r.created_at,s.name,r.status,
       COALESCE(NULLIF(remote_group.group_name,''), '检测分组'),
       r.request->'config'->>'managed_key_id',r.request->'config'->>'model',r.request->>'effort',
       r.result->>'candy_answer',r.result->>'candy_verdict',r.result->>'duration_ms',r.result->>'template_version'
FROM upstream_governance_model_runs r
JOIN upstream_governance_sites s ON s.id=r.site_id
LEFT JOIN upstream_governance_keys managed_key
  ON managed_key.id = CASE WHEN r.request->'config'->>'managed_key_id' ~ '^[0-9]+$' THEN (r.request->'config'->>'managed_key_id')::bigint END
LEFT JOIN LATERAL (
  SELECT group_item->>'name' AS group_name
  FROM upstream_governance_snapshots snapshot
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(snapshot.catalog->'groups','[]'::jsonb)) group_item
  WHERE snapshot.site_id=r.site_id AND group_item->>'id'=managed_key.remote_group_id
  ORDER BY snapshot.id DESC LIMIT 1
) remote_group ON TRUE
WHERE r.created_at >= $1 AND r.request->>'template'='candy'
ORDER BY r.created_at DESC,r.id DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	for candyRows.Next() {
		var id, siteName, runStatus string
		var createdAt time.Time
		var groupName, managedID, model, effort, answer, verdict, duration, version sql.NullString
		if err = candyRows.Scan(&id, &createdAt, &siteName, &runStatus, &groupName, &managedID, &model, &effort, &answer, &verdict, &duration, &version); err != nil {
			candyRows.Close()
			return nil, err
		}
		label := iqGroupLabel(groupName)
		dashboard.CandyResults = append(dashboard.CandyResults, IQCandyResult{ID: id, SiteName: siteName, GroupName: label, AccountLabel: label, Model: model.String, Effort: effort.String, Answer: iqIntPointer(answer), Verdict: verdict.String, Status: runStatus, DurationMS: iqInt64(duration), CreatedAt: createdAt, TemplateVersion: version.String})
	}
	if err = candyRows.Err(); err != nil {
		candyRows.Close()
		return nil, err
	}
	candyRows.Close()

	pelicanRows, err := m.db.QueryContext(ctx, fmt.Sprintf(`SELECT r.id,r.created_at,s.name,r.status,
       COALESCE(NULLIF(remote_group.group_name,''), '检测分组'),
       r.request->'config'->>'managed_key_id',r.request->'config'->>'model',r.request->>'effort',
       CASE WHEN octet_length(r.result->>'html') <= %d THEN r.result->>'html' ELSE '' END,
       CASE WHEN octet_length(r.result->>'response_text') <= %d THEN r.result->>'response_text' ELSE '' END,
       r.result->>'duration_ms'
FROM upstream_governance_model_runs r
JOIN upstream_governance_sites s ON s.id=r.site_id
LEFT JOIN upstream_governance_keys managed_key
  ON managed_key.id = CASE WHEN r.request->'config'->>'managed_key_id' ~ '^[0-9]+$' THEN (r.request->'config'->>'managed_key_id')::bigint END
LEFT JOIN LATERAL (
  SELECT group_item->>'name' AS group_name
  FROM upstream_governance_snapshots snapshot
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(snapshot.catalog->'groups','[]'::jsonb)) group_item
  WHERE snapshot.site_id=r.site_id AND group_item->>'id'=managed_key.remote_group_id
  ORDER BY snapshot.id DESC LIMIT 1
) remote_group ON TRUE
WHERE r.created_at >= $1 AND r.request->>'template'='pelican'
ORDER BY r.created_at DESC,r.id DESC LIMIT $2`, maxIQHTMLBytes, maxIQHTMLBytes), since, limit)
	if err != nil {
		return nil, err
	}
	totalHTMLBytes := 0
	for pelicanRows.Next() {
		var id, siteName, runStatus string
		var createdAt time.Time
		var groupName, managedID, model, effort, htmlRaw, responseRaw, duration sql.NullString
		if err = pelicanRows.Scan(&id, &createdAt, &siteName, &runStatus, &groupName, &managedID, &model, &effort, &htmlRaw, &responseRaw, &duration); err != nil {
			pelicanRows.Close()
			return nil, err
		}
		html := extractIQHTMLArtifact(htmlRaw.String)
		if html == "" {
			html = extractIQHTMLArtifact(responseRaw.String)
		}
		if html == "" || totalHTMLBytes+len(html) > maxIQHTMLTotalBytes {
			continue
		}
		totalHTMLBytes += len(html)
		label := iqGroupLabel(groupName)
		dashboard.PelicanWorks = append(dashboard.PelicanWorks, IQPelicanWork{ID: id, SiteName: siteName, GroupName: label, AccountLabel: label, Model: model.String, Effort: effort.String, HTML: html, Status: runStatus, DurationMS: iqInt64(duration), CreatedAt: createdAt})
	}
	if err = pelicanRows.Err(); err != nil {
		pelicanRows.Close()
		return nil, err
	}
	pelicanRows.Close()
	dashboard.Timeline = buildIQTimeline(now, timelineSamples, hours)
	return dashboard, nil
}

func iqGroupLabel(raw sql.NullString) string {
	if raw.Valid && strings.TrimSpace(raw.String) != "" {
		return strings.TrimSpace(raw.String)
	}
	return "检测分组"
}

func iqIntPointer(raw sql.NullString) *int {
	if !raw.Valid {
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw.String))
	if err != nil {
		return nil
	}
	return &v
}

func iqInt64(raw sql.NullString) int64 {
	if !raw.Valid {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw.String), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func extractIQHTMLArtifact(raw string) string {
	if len(raw) > maxIQHTMLBytes {
		return ""
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(value), "```html") || strings.HasPrefix(strings.ToLower(value), "```htm") {
		if newline := strings.IndexByte(value, '\n'); newline >= 0 {
			value = strings.TrimSpace(value[newline+1:])
		}
		value = strings.TrimSuffix(value, "```")
		value = strings.TrimSpace(value)
	}
	lower := strings.ToLower(value)
	start := strings.Index(lower, "<!doctype")
	if start < 0 {
		start = strings.Index(lower, "<html")
	}
	if start >= 0 {
		end := strings.LastIndex(lower, "</html>")
		if end >= start {
			value = strings.TrimSpace(value[start : end+len("</html>")])
		}
	}
	if !strings.Contains(strings.ToLower(value), "<svg") {
		return ""
	}
	if !strings.Contains(strings.ToLower(value), "<html") {
		value = "<!doctype html><html><head><meta charset=\"utf-8\"><style>html,body{margin:0;min-height:100%;overflow:hidden}body{display:grid;place-items:center;background:#f8fafc}</style></head><body>" + value + "</body></html>"
	}
	if len(value) > maxIQHTMLBytes {
		return ""
	}
	return value
}

func buildIQTimeline(now time.Time, samples []iqTimelineSample, hours int) []IQTimelinePoint {
	now = now.UTC()
	start := now.Add(-time.Duration(hours) * time.Hour).Truncate(30 * time.Minute)
	end := now.Truncate(30 * time.Minute)
	if end.Before(start) {
		end = start
	}
	points := make([]IQTimelinePoint, int(end.Sub(start)/(30*time.Minute))+1)
	for i := range points {
		points[i] = IQTimelinePoint{At: start.Add(time.Duration(i) * 30 * time.Minute), Status: "empty"}
	}
	for _, sample := range samples {
		if sample.At.Before(start) || sample.At.After(now) {
			continue
		}
		index := int(sample.At.UTC().Sub(start) / (30 * time.Minute))
		if index < 0 || index >= len(points) {
			continue
		}
		if sample.Status == "fail" {
			points[index].Status = "fail"
		} else if sample.Status == "pass" && points[index].Status == "empty" {
			points[index].Status = "pass"
		}
	}
	return points
}
