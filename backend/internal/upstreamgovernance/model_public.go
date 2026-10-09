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

const iqPublicGroupProjection = "COALESCE(NULLIF(local_group.name,''), NULLIF(imported_group.name,''), '检测分组')"

// Imported upstream keys are bound to one or more local groups. Public IQ
// responses must use those local names, never the names from an upstream
// catalog. The direct local_group join covers local-model runs; imported_group
// resolves legacy and multi-target bindings deterministically by local ID.
const iqPublicGroupJoins = `
LEFT JOIN groups local_group
  ON local_group.id = CASE WHEN r.request->'config'->>'local_group_id' ~ '^[0-9]+$' THEN (r.request->'config'->>'local_group_id')::bigint END
 AND local_group.deleted_at IS NULL
LEFT JOIN LATERAL (
  SELECT string_agg(mapped.name, ', ' ORDER BY mapped.id) AS name
  FROM (
    SELECT DISTINCT g.id, g.name
    FROM upstream_governance_bindings binding
    CROSS JOIN LATERAL (
      SELECT binding.local_group_id AS group_id
      UNION
      SELECT CASE WHEN group_value ~ '^[0-9]+$' THEN group_value::bigint END
      FROM jsonb_array_elements_text(COALESCE(binding.local_group_ids, jsonb_build_array(binding.local_group_id))) AS group_values(group_value)
    ) mapped_id
    JOIN groups g ON g.id=mapped_id.group_id AND g.deleted_at IS NULL
    WHERE binding.site_id=r.site_id
      AND binding.remote_group_id=managed_key.remote_group_id
      AND binding.platform=managed_key.platform
  ) mapped
) imported_group ON TRUE`

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
WHERE r.public_visible AND r.created_at >= $1 AND r.created_at <= $2 AND r.request->>'template' IN ('candy','pelican')
GROUP BY bucket_at ORDER BY bucket_at`

// IQDashboard is a credential-free projection of model monitoring results for
// the authenticated user-facing IQ page. It intentionally omits prompts,
// request bodies, API keys, session data and raw JSON evidence.
type IQDashboard struct {
	CandyResults    []IQCandyResult   `json:"candy_results"`
	PelicanWorks    []IQPelicanWork   `json:"pelican_works"`
	PelicanPage     int               `json:"pelican_page"`
	PelicanPageSize int               `json:"pelican_page_size"`
	PelicanHasMore  bool              `json:"pelican_has_more"`
	Timeline        []IQTimelinePoint `json:"timeline"`
	StandardAnswer  int               `json:"standard_answer"`
	WindowHours     int               `json:"window_hours"`
	GeneratedAt     time.Time         `json:"generated_at"`
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
	return s.PublicIQDashboardPage(ctx, hours, limit, 1, limit)
}

// PublicIQDashboardPage keeps the candy/timeline window stable while paging
// only the potentially large Pelican artifact list. One extra row is fetched
// to determine whether the next page exists without a second COUNT query.
func (s *Service) PublicIQDashboardPage(ctx context.Context, hours, limit, pelicanPage, pelicanPageSize int) (*IQDashboard, error) {
	if hours <= 0 || hours > 168 || limit <= 0 || limit > 20 || pelicanPage <= 0 || pelicanPage > 1000000 || pelicanPageSize <= 0 || pelicanPageSize > 20 {
		return nil, ErrInvalid
	}
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	since := now.Add(-time.Duration(hours) * time.Hour)
	dashboard := &IQDashboard{CandyResults: []IQCandyResult{}, PelicanWorks: []IQPelicanWork{}, PelicanPage: pelicanPage, PelicanPageSize: pelicanPageSize, StandardAnswer: 21, WindowHours: hours, GeneratedAt: now}

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
       `+iqPublicGroupProjection+`,
       r.request->'config'->>'managed_key_id',r.request->'config'->>'model',r.request->>'effort',
       r.result->>'candy_answer',r.result->>'candy_verdict',r.result->>'duration_ms',r.result->>'template_version'
FROM upstream_governance_model_runs r
JOIN upstream_governance_sites s ON s.id=r.site_id
LEFT JOIN upstream_governance_keys managed_key
  ON managed_key.id = CASE WHEN r.request->'config'->>'managed_key_id' ~ '^[0-9]+$' THEN (r.request->'config'->>'managed_key_id')::bigint END
`+iqPublicGroupJoins+`
WHERE r.public_visible AND r.created_at >= $1 AND r.request->>'template'='candy'
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
       `+iqPublicGroupProjection+`,
       r.request->'config'->>'managed_key_id',r.request->'config'->>'model',r.request->>'effort',
       CASE WHEN octet_length(r.result->>'html') <= %d THEN r.result->>'html' ELSE '' END,
       CASE WHEN octet_length(r.result->>'response_text') <= %d THEN r.result->>'response_text' ELSE '' END,
       r.result->>'duration_ms'
FROM upstream_governance_model_runs r
JOIN upstream_governance_sites s ON s.id=r.site_id
LEFT JOIN upstream_governance_keys managed_key
  ON managed_key.id = CASE WHEN r.request->'config'->>'managed_key_id' ~ '^[0-9]+$' THEN (r.request->'config'->>'managed_key_id')::bigint END
`+iqPublicGroupJoins+`
WHERE r.public_visible AND r.created_at >= $1 AND r.request->>'template'='pelican'
ORDER BY r.created_at DESC,r.id DESC LIMIT $2 OFFSET $3`, maxIQHTMLBytes, maxIQHTMLBytes), since, pelicanPageSize+1, (pelicanPage-1)*pelicanPageSize)
	if err != nil {
		return nil, err
	}
	totalHTMLBytes := 0
	pelicanRowsSeen := 0
	for pelicanRows.Next() {
		pelicanRowsSeen++
		var id, siteName, runStatus string
		var createdAt time.Time
		var groupName, managedID, model, effort, htmlRaw, responseRaw, duration sql.NullString
		if err = pelicanRows.Scan(&id, &createdAt, &siteName, &runStatus, &groupName, &managedID, &model, &effort, &htmlRaw, &responseRaw, &duration); err != nil {
			pelicanRows.Close()
			return nil, err
		}
		// The extra SQL row is only a bounded look-ahead marker. It must not
		// become part of this page, even when an earlier row has no safe HTML.
		if pelicanRowsSeen > pelicanPageSize {
			continue
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
	// Pagination follows the rows selected by SQL rather than the number of
	// sanitized artifacts that survived the per-page HTML budget. This keeps
	// later rows reachable when an earlier response is prose or oversized.
	dashboard.PelicanHasMore = pelicanRowsSeen > pelicanPageSize
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
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = stripIQMarkdownFenceLines(value)
	lower := strings.ToLower(value)
	start := strings.Index(lower, "<!doctype")
	if start < 0 {
		start = findIQTagStart(lower, "html")
	}
	if start >= 0 {
		if end, ok := findIQClosingTag(lower, start, "html"); ok {
			value = strings.TrimSpace(value[start:end])
		} else {
			return ""
		}
	} else if svgStart := findIQTagStart(lower, "svg"); svgStart >= 0 {
		if end, ok := findIQClosingTag(lower, svgStart, "svg"); ok {
			value = strings.TrimSpace(value[svgStart:end])
		} else {
			// A self-closing SVG is a complete artifact even without a closing
			// </svg> element. Anything else is incomplete and is omitted.
			tagEnd := strings.IndexByte(lower[svgStart:], '>')
			if tagEnd < 0 {
				return ""
			}
			tagEnd += svgStart + 1
			if !strings.HasSuffix(strings.TrimSpace(lower[svgStart:tagEnd]), "/>") {
				return ""
			}
			value = strings.TrimSpace(value[svgStart:tagEnd])
		}
	}
	value = strings.TrimSpace(stripIQMarkdownFenceLines(value))
	lower = strings.ToLower(value)
	if !strings.Contains(lower, "<svg") {
		return ""
	}
	if !strings.Contains(lower, "<html") {
		value = "<!doctype html><html><head><meta charset=\"utf-8\"><style>html,body{margin:0;min-height:100%;overflow:hidden}body{display:grid;place-items:center;background:#f8fafc}</style></head><body>" + value + "</body></html>"
	}
	if len(value) > maxIQHTMLBytes {
		return ""
	}
	return value
}

func stripIQMarkdownFenceLines(value string) string {
	lines := strings.Split(value, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func findIQTagStart(lower, tag string) int {
	needle := "<" + tag
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], needle)
		if index < 0 {
			return -1
		}
		index += offset
		next := index + len(needle)
		if next >= len(lower) || strings.ContainsRune(" \t\r\n/>", rune(lower[next])) {
			return index
		}
		offset = index + 1
	}
	return -1
}

func findIQClosingTag(lower string, start int, tag string) (int, bool) {
	needle := "</" + tag
	for offset := start; offset < len(lower); {
		index := strings.Index(lower[offset:], needle)
		if index < 0 {
			return 0, false
		}
		index += offset
		next := index + len(needle)
		if next < len(lower) && !strings.ContainsRune(" \t\r\n>", rune(lower[next])) {
			offset = index + 1
			continue
		}
		end := strings.IndexByte(lower[next:], '>')
		if end < 0 {
			return 0, false
		}
		return next + end + 1, true
	}
	return 0, false
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
