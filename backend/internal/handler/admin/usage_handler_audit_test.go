package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminUsageAuditRepo struct {
	service.UsageLogRepository
	records []service.UsageLog
}

func (r *adminUsageAuditRepo) ListWithFilters(_ context.Context, params pagination.PaginationParams, _ usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return r.records, &pagination.PaginationResult{Total: int64(len(r.records)), Page: params.Page, PageSize: params.PageSize}, nil
}

func TestAdminUsageListIncludesReadOnlyRawAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	row := service.UsageLog{
		ID: 1, Model: "gpt-5.5", InputTokens: 2000, OutputTokens: 200,
		UpstreamInputTokens: 1000, UpstreamOutputTokens: 100, CacheReadTokens: 9000, UpstreamCacheReadTokens: 9000,
		InputCost: .1, OutputCost: .03, TotalCost: .13, ActualCost: .0065, RateMultiplier: .05,
	}
	missing := service.UsageLog{
		ID: 2, Model: "gpt-5.5", InputTokens: 500, OutputTokens: 20,
		InputCost: .1, TotalCost: .1, ActualCost: .1, RateMultiplier: 1,
	}
	repo := &adminUsageAuditRepo{records: []service.UsageLog{row, missing}}
	usage := service.NewUsageService(repo, nil, nil, nil)
	billing := service.NewBillingService(&config.Config{}, nil)
	audit := service.NewUsageCostAuditService(billing, nil, nil, nil)
	h := NewUsageHandler(usage, nil, nil, nil, audit, nil)
	router := gin.New()
	router.GET("/admin/usage", h.List)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/usage", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data struct {
			Items []dto.AdminUsageLog `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data.Items, 2)
	got := body.Data.Items[0]
	require.NotNil(t, got.RawCostAudit)
	require.Equal(t, "calculated", got.RawCostAudit.Status)
	require.InDelta(t, .000625, *got.RawCostAudit.Amount, 1e-12)
	require.InDelta(t, .0125, *got.RawCostAudit.TotalCost, 1e-12)
	require.InDelta(t, row.ActualCost, *got.RawCostAudit.BilledAmount, 1e-12)
	require.InDelta(t, 0.005875, *got.RawCostAudit.Difference, 1e-12)
	var rawBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rawBody))
	data := rawBody["data"].(map[string]any)
	items := data["items"].([]any)
	firstAudit := items[0].(map[string]any)["raw_cost_audit"].(map[string]any)
	require.InDelta(t, 0.005875, firstAudit["difference"], 1e-12)
	require.Equal(t, row.ActualCost, got.ActualCost, "audit must not replace the recorded debit")
	require.Equal(t, row.InputTokens, got.InputTokens)
	require.Equal(t, row.UpstreamInputTokens, got.UpstreamInputTokens)
	require.NotNil(t, got.UsageLimitAudit)
	require.Equal(t, "unknown", got.UsageLimitAudit.Status, "missing capacity metadata is not proof of validity")
	require.EqualValues(t, 10000, *got.UsageLimitAudit.TotalInputTokens)
	require.EqualValues(t, 10100, *got.UsageLimitAudit.TotalContextTokens)
	require.Contains(t, got.UsageLimitAudit.Issues, "missing_context_limit")
	require.Equal(t, "unavailable", body.Data.Items[1].RawCostAudit.Status)
	require.Nil(t, body.Data.Items[1].RawCostAudit.Amount)
	require.Contains(t, body.Data.Items[1].UsageLimitAudit.Issues, "upstream_input_unavailable")
	require.Equal(t, []service.UsageLog{row, missing}, repo.records, "listing must not mutate stored evidence")

	userJSON, err := json.Marshal(dto.UsageLogFromService(&row))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "raw_cost_audit")
	require.NotContains(t, string(userJSON), "usage_limit_audit")
	require.NotContains(t, rec.Body.String(), "Breakdown")
}

func TestAdminUsageListAuditDependenciesUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminUsageAuditRepo{records: []service.UsageLog{{
		ID: 7, Model: "gpt-5.5", InputTokens: 200, OutputTokens: 20,
		UpstreamInputTokens: 100, UpstreamOutputTokens: 10, RateMultiplier: 1,
	}}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/admin/usage", h.List)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/usage", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data struct {
			Items []dto.AdminUsageLog `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data.Items, 1)
	require.Equal(t, "unavailable", body.Data.Items[0].RawCostAudit.Status)
	require.Nil(t, body.Data.Items[0].RawCostAudit.Amount)
	require.Equal(t, "unknown", body.Data.Items[0].UsageLimitAudit.Status)
}
