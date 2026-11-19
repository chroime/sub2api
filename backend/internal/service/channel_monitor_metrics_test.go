//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type metricsMonitorRepo struct {
	sortChannelMonitorServiceRepo
	metrics                  map[int64]map[string]MonitorUsageMetric
	rates                    map[int64]float64
	err                      error
	metricsCalls, ratesCalls int
	userID                   int64
	ids                      []int64
}

func (r *metricsMonitorRepo) ListUsageMetricsForMonitors(_ context.Context, ids []int64, _ time.Time) (map[int64]map[string]MonitorUsageMetric, error) {
	r.metricsCalls++
	r.ids = append([]int64{}, ids...)
	return r.metrics, r.err
}

func (r *metricsMonitorRepo) ListUserRatesForMonitors(_ context.Context, _ []int64, userID int64) (map[int64]float64, error) {
	r.ratesCalls++
	r.userID = userID
	return r.rates, r.err
}

func TestChannelMonitorMetricsBatchEnrichmentUsesRequestUser(t *testing.T) {
	cache, ttft := 86.2, 3000.0
	repo := &metricsMonitorRepo{
		sortChannelMonitorServiceRepo: sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{{ID: 5, Enabled: true}, {ID: 9, Enabled: true}}},
		metrics:                       map[int64]map[string]MonitorUsageMetric{5: {"7d": {CacheRatePct: &cache, FirstTokenMs: &ttft}}},
		rates:                         map[int64]float64{5: 0},
	}
	monitorService := NewChannelMonitorService(repo, nil)
	analytics, err := monitorService.ListUserAnalytics(context.Background())
	require.NoError(t, err)
	views, err := monitorService.ListUserViewForUser(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, 1, repo.metricsCalls)
	require.Equal(t, 1, repo.ratesCalls)
	require.Equal(t, int64(42), repo.userID)
	require.Equal(t, []int64{5, 9}, repo.ids)
	require.Equal(t, 86.2, *analytics[0].UsageMetrics["7d"].CacheRatePct)
	require.Equal(t, 3000.0, *analytics[0].UsageMetrics["7d"].FirstTokenMs)
	require.Nil(t, views[0].UsageMetrics, "base monitor response must not wait for usage aggregation")
	require.NotNil(t, views[0].UserRateMultiplier)
	require.Zero(t, *views[0].UserRateMultiplier)
	require.Nil(t, analytics[1].UsageMetrics)
	require.Nil(t, views[1].UserRateMultiplier)
}

func TestChannelMonitorMetricsAnonymousListNeverLoadsUserRates(t *testing.T) {
	repo := &metricsMonitorRepo{sortChannelMonitorServiceRepo: sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{{ID: 5, Enabled: true}}}}
	views, err := NewChannelMonitorService(repo, nil).ListUserView(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, repo.ratesCalls)
	require.Nil(t, views[0].UserRateMultiplier)
}

func TestChannelMonitorMetricsFailureDoesNotHideProbeCards(t *testing.T) {
	repo := &metricsMonitorRepo{sortChannelMonitorServiceRepo: sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{{ID: 5, Name: "probe", Enabled: true}}}, err: errors.New("unavailable")}
	monitorService := NewChannelMonitorService(repo, nil)
	views, err := monitorService.ListUserViewForUser(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, "probe", views[0].Name)
	require.Nil(t, views[0].UsageMetrics)
	require.Nil(t, views[0].UserRateMultiplier)
	analytics, err := monitorService.ListUserAnalytics(context.Background())
	require.NoError(t, err)
	require.Nil(t, analytics[0].UsageMetrics)
}

func TestChannelMonitorMetricsShareOnlyUsageAggregation(t *testing.T) {
	repo := &metricsMonitorRepo{sortChannelMonitorServiceRepo: sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{{ID: 5, PrimaryModel: "model", Enabled: true}}}, rates: map[int64]float64{5: 0.1}}
	s := NewChannelMonitorService(repo, nil)
	_, err := s.ListUserAnalytics(context.Background())
	require.NoError(t, err)
	first, err := s.ListUserViewForUser(context.Background(), 42)
	require.NoError(t, err)
	repo.rates = map[int64]float64{5: 0.2}
	_, err = s.ListUserAnalytics(context.Background())
	require.NoError(t, err)
	second, err := s.ListUserViewForUser(context.Background(), 99)
	require.NoError(t, err)
	require.Equal(t, 1, repo.metricsCalls, "viewers should share the recent streaming sample query")
	require.Equal(t, 2, repo.ratesCalls, "personalized multipliers must never use the shared cache")
	require.Equal(t, 0.1, *first[0].UserRateMultiplier)
	require.Equal(t, 0.2, *second[0].UserRateMultiplier)
	repo.monitors[0].PrimaryModel = "different-model"
	_, err = s.ListUserAnalytics(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, repo.metricsCalls, "monitor configuration changes invalidate the usage cache")
}
