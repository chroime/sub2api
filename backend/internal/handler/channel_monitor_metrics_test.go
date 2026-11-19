//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type metricsUserMonitorRepo struct {
	sortChannelMonitorUserRepo
}

func (*metricsUserMonitorRepo) ListUsageMetricsForMonitors(context.Context, []int64, time.Time) (map[int64]map[string]service.MonitorUsageMetric, error) {
	cache, first := 86.2, 3000.0
	return map[int64]map[string]service.MonitorUsageMetric{41: {"7d": {CacheRatePct: &cache, FirstTokenMs: &first}}}, nil
}

func (*metricsUserMonitorRepo) ListUserRatesForMonitors(_ context.Context, _ []int64, userID int64) (map[int64]float64, error) {
	return map[int64]float64{41: map[int64]float64{42: 0.075, 99: 1.5}[userID]}, nil
}

func TestMonitorMetricsHandlerUsesAuthIdentityWithoutCrossUserLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewChannelMonitorUserHandler(service.NewChannelMonitorService(&metricsUserMonitorRepo{}, nil), nil)
	for _, userID := range []int64{42, 99, 0} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/channel-monitors?user_id=99", nil)
		if userID > 0 {
			ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		h.List(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var result struct {
			Data struct {
				Items []channelMonitorUserListItem `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		item := result.Data.Items[0]
		require.Nil(t, item.UsageMetrics, "the base list must return before usage aggregation finishes")
		if userID == 0 {
			require.Nil(t, item.UserRateMultiplier)
		} else {
			require.Equal(t, map[int64]float64{42: 0.075, 99: 1.5}[userID], *item.UserRateMultiplier)
		}

		analyticsRecorder := httptest.NewRecorder()
		analyticsContext, _ := gin.CreateTestContext(analyticsRecorder)
		analyticsContext.Request = httptest.NewRequest(http.MethodGet, "/channel-monitors/analytics", nil)
		h.Analytics(analyticsContext)
		require.Equal(t, http.StatusOK, analyticsRecorder.Code)
		var analyticsResult struct {
			Data struct {
				Items []service.UserMonitorAnalytics `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(analyticsRecorder.Body.Bytes(), &analyticsResult))
		require.Equal(t, int64(41), analyticsResult.Data.Items[0].ID)
		require.Equal(t, 86.2, *analyticsResult.Data.Items[0].UsageMetrics["7d"].CacheRatePct)
		require.Equal(t, 3000.0, *analyticsResult.Data.Items[0].UsageMetrics["7d"].FirstTokenMs)
		for _, sensitive := range []string{"secret-key", "private-token", "account_id", "user_id", "api_key", "endpoint"} {
			require.NotContains(t, recorder.Body.String()+analyticsRecorder.Body.String(), `"`+sensitive+`"`)
		}
	}
}
