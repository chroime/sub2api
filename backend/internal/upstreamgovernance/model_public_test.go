package upstreamgovernance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestExtractIQHTMLArtifactKeepsOnlyHTMLOrSVG(t *testing.T) {
	require.Equal(t, "", extractIQHTMLArtifact("plain response"))
	require.Contains(t, extractIQHTMLArtifact("```html\n<html><body><svg></svg></body></html>\n```"), "<svg>")
	require.Equal(t, "<html><body><svg></svg></body></html>", extractIQHTMLArtifact("<html><body><svg></svg></body></html>"))
}

func TestBuildIQTimelineUsesDynamicWindowAndFailurePrecedence(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 8, 0, 0, time.UTC)
	base := now.Add(-24 * time.Hour)
	points := buildIQTimeline(now, []iqTimelineSample{
		{At: base.Add(10 * time.Minute), Status: "pass"},
		{At: base.Add(20 * time.Minute), Status: "fail"},
	}, 24)
	require.Len(t, points, 49)
	require.Equal(t, "fail", points[0].Status)
	require.Equal(t, base.Truncate(30*time.Minute), points[0].At)
	require.Equal(t, now.Truncate(30*time.Minute), points[len(points)-1].At)
}

func TestPublicIQDashboardSeparatesTemplateLimitsAndMarksWrongCandyAsFailed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 10, 2, 12, 8, 0, 0, time.UTC)
	svc := NewService(NewSQLStore(db), nil, nil, nil, false)
	svc.now = func() time.Time { return now }
	since := now.Add(-24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta(iqTimelineQuery)).
		WithArgs(since, now).
		WillReturnRows(sqlmock.NewRows([]string{"bucket_at", "quality_status"}).
			AddRow(now.Truncate(30*time.Minute), 2).
			AddRow(now.Add(-12*time.Hour).Truncate(30*time.Minute), 1))
	mock.ExpectQuery("SELECT r.id,r.created_at,s.name,r.status,").
		WithArgs(since, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "name", "status", "group_name", "managed_key_id", "model", "effort", "answer", "verdict", "duration_ms", "template_version"}).
			AddRow("candy-wrong", now.Add(-2*time.Minute), "site", "succeeded", "GPT Lite", "4", "gpt-6", "low", "20", "numeric_incorrect", "10", "v1").
			AddRow("candy-right", now.Add(-4*time.Minute), "site", "succeeded", "GPT Lite", "21", "gpt-6", "high", "21", "numeric_correct", "20", "v1"))
	mock.ExpectQuery("SELECT r.id,r.created_at,s.name,r.status,").
		WithArgs(since, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "name", "status", "group_name", "managed_key_id", "model", "effort", "html", "response_text", "duration_ms"}).
			AddRow("pelican-1", now.Add(-3*time.Minute), "site", "succeeded", "GPT Lite", "5", "gpt-6", "medium", "<svg />", "", "30"))

	dashboard, err := svc.PublicIQDashboard(t.Context(), 24, 2)
	require.NoError(t, err)
	require.Len(t, dashboard.CandyResults, 2)
	require.Len(t, dashboard.PelicanWorks, 1)
	require.Equal(t, "GPT Lite", dashboard.CandyResults[0].GroupName)
	require.Equal(t, "GPT Lite", dashboard.PelicanWorks[0].GroupName)
	require.Equal(t, "fail", dashboard.Timeline[len(dashboard.Timeline)-1].Status)
	// The wrong candy run shares the current half-hour with the successful run;
	// failure must take precedence over the successful HTTP status.
	foundFail := false
	for _, point := range dashboard.Timeline {
		if point.Status == "fail" {
			foundFail = true
			break
		}
	}
	require.True(t, foundFail)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPublicIQDashboardRejectsInvalidBounds(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	svc := NewService(NewSQLStore(db), nil, nil, nil, false)
	for _, tc := range []struct {
		hours int
		limit int
	}{
		{0, 1}, {169, 1}, {1, 0}, {1, 21}, {-1, 1}, {1, -1},
	} {
		_, err := svc.PublicIQDashboard(t.Context(), tc.hours, tc.limit)
		require.ErrorIs(t, err, ErrInvalid)
	}
}

func TestPublicIQDashboardHTMLCapsItemAndTotalBytes(t *testing.T) {
	require.Empty(t, extractIQHTMLArtifact("<svg>"+strings.Repeat("x", maxIQHTMLBytes)+"</svg>"))
	require.Equal(t, 512*1024, maxIQHTMLBytes)
	require.Equal(t, 2*1024*1024, maxIQHTMLTotalBytes)
}

func TestPublicIQDashboardEnforcesTotalHTMLBudgetAndSafeProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 10, 2, 12, 8, 0, 0, time.UTC)
	svc := NewService(NewSQLStore(db), nil, nil, nil, false)
	svc.now = func() time.Time { return now }
	since := now.Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(iqTimelineQuery)).WithArgs(since, now).
		WillReturnRows(sqlmock.NewRows([]string{"bucket_at", "quality_status"}))
	mock.ExpectQuery("SELECT r.id,r.created_at,s.name,r.status,[\\s\\S]*r.request->>'template'='candy'[\\s\\S]*LIMIT \\$2").WithArgs(since, 8).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "name", "status", "group_name", "managed_key_id", "model", "effort", "answer", "verdict", "duration_ms", "template_version"}))
	rows := sqlmock.NewRows([]string{"id", "created_at", "name", "status", "group_name", "managed_key_id", "model", "effort", "html", "response_text", "duration_ms"})
	html := "<html><body><svg>" + strings.Repeat("x", maxIQHTMLBytes-len("<html><body><svg></svg></body></html>")) + "</svg></body></html>"
	require.Len(t, html, maxIQHTMLBytes)
	for i := 0; i < 6; i++ {
		rows.AddRow(fmt.Sprint(i), now, "site", "succeeded", "GPT Lite", "4", "gpt-model", "low", html, "", "10")
	}
	mock.ExpectQuery("SELECT r.id,r.created_at,s.name,r.status,[\\s\\S]*octet_length[\\s\\S]*r.request->>'template'='pelican'[\\s\\S]*LIMIT \\$2").WithArgs(since, 8).WillReturnRows(rows)
	dashboard, err := svc.PublicIQDashboard(t.Context(), 1, 8)
	require.NoError(t, err)
	require.Len(t, dashboard.PelicanWorks, 4)
	var total int
	for _, work := range dashboard.PelicanWorks {
		total += len(work.HTML)
	}
	require.Equal(t, maxIQHTMLTotalBytes, total)
	raw, err := json.Marshal(dashboard)
	require.NoError(t, err)
	for _, secretField := range []string{"base_url", "access_token", "request_body", "session", "raw_usage", "api_key", "input_text"} {
		require.NotContains(t, string(raw), `"`+secretField+`"`)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildIQTimelineHandlesRequestedHoursAndWindowEdges(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, hours := range []int{1, 24, 168} {
		start := now.Add(-time.Duration(hours) * time.Hour)
		points := buildIQTimeline(now, []iqTimelineSample{{At: start, Status: "pass"}, {At: now, Status: "fail"}, {At: start.Add(-time.Minute), Status: "fail"}, {At: now.Add(time.Minute), Status: "pass"}}, hours)
		require.Len(t, points, hours*2+1)
		require.Equal(t, "pass", points[0].Status)
		require.Equal(t, "fail", points[len(points)-1].Status)
	}
}

// This optional PostgreSQL fixture verifies the actual CASE/MAX expression,
// independently of the sqlmock projection tests above.
func TestPublicIQDashboardPostgresAggregateIncludesOldFailedCandyBeyondCardLimit(t *testing.T) {
	f := newModelEngineFixture(t)
	cfg := f.config
	cfg.Templates = []string{"candy", "pelican"}
	cfg.Samples = 3
	batch := f.batch(t, "iq-public-aggregate", cfg)
	now := time.Now().UTC()
	f.service.now = func() time.Time { return now }
	_, err := f.db.Exec(`UPDATE upstream_governance_model_runs SET status='succeeded',created_at=$2,result='{"candy_verdict":"numeric_correct","candy_answer":21,"html":"<html><svg/></html>","duration_ms":15}'::jsonb WHERE batch_id=$1`, batch.ID, now.Add(-time.Minute))
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE upstream_governance_model_runs SET created_at=$2,result='{"candy_verdict":"numeric_incorrect","candy_answer":20}'::jsonb WHERE id=(SELECT id FROM upstream_governance_model_runs WHERE batch_id=$1 AND request->>'template'='candy' ORDER BY sequence LIMIT 1)`, batch.ID, now.Add(-12*time.Hour))
	require.NoError(t, err)
	dashboard, err := f.service.PublicIQDashboard(t.Context(), 24, 1)
	require.NoError(t, err)
	require.Len(t, dashboard.CandyResults, 1)
	require.Equal(t, "numeric_correct", dashboard.CandyResults[0].Verdict)
	require.Len(t, dashboard.PelicanWorks, 1)
	failedAt := now.Add(-12 * time.Hour).Truncate(30 * time.Minute)
	found := false
	for _, point := range dashboard.Timeline {
		if point.At.Equal(failedAt) {
			require.Equal(t, "fail", point.Status)
			found = true
		}
	}
	require.True(t, found)
}
