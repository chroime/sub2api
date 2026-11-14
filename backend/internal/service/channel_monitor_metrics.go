package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// MonitorUsageMetric contains aggregate successful usage for the monitor's
// exact group/platform/primary model. Nil means no measured samples, not zero.
type MonitorUsageMetric struct {
	CacheRatePct *float64 `json:"cache_rate_pct"` // 0–100, token-weighted
	FirstTokenMs *float64 `json:"first_token_ms"` // mean measured time to first token
}

// Optional readers keep lightweight probe-only repository implementations valid.
type monitorUsageMetricsReader interface {
	ListUsageMetricsForMonitors(context.Context, []int64, time.Time) (map[int64]map[string]MonitorUsageMetric, error)
}

type monitorUserRatesReader interface {
	ListUserRatesForMonitors(context.Context, []int64, int64) (map[int64]float64, error)
}

type monitorUsageMetricsCache struct {
	mu        sync.Mutex
	key       string
	expiresAt time.Time
	metrics   map[int64]map[string]MonitorUsageMetric
}

func (s *ChannelMonitorService) usageMetricsForMonitors(ctx context.Context, ids []int64, monitors []*ChannelMonitor) map[int64]map[string]MonitorUsageMetric {
	reader, ok := s.repo.(monitorUsageMetricsReader)
	if !ok {
		return nil
	}
	var key strings.Builder
	for _, m := range monitors {
		var accountID, groupID, keyID int64
		if m.AccountID != nil {
			accountID = *m.AccountID
		}
		if m.GroupID != nil {
			groupID = *m.GroupID
		}
		if m.LocalAPIKeyID != nil {
			keyID = *m.LocalAPIKeyID
		}
		fmt.Fprintf(&key, "%d:%q:%q:%q:%d:%d:%d:%d:%s:%s;", m.ID, m.Provider, m.GroupName, m.PrimaryModel, accountID, m.UpdatedAt.UnixNano(), groupID, keyID, m.GroupBindingMode, m.GroupBindingStatus)
	}
	// Share only public aggregate statistics, never the user-specific rates.
	// Serialize cache misses so concurrent viewers cannot repeat a 30-day scan.
	cache := &s.usageMetrics
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.key == key.String() && time.Now().Before(cache.expiresAt) {
		return cache.metrics
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := reader.ListUsageMetricsForMonitors(queryCtx, ids, time.Now().UTC())
	if err != nil {
		slog.Warn("channel_monitor: usage metrics unavailable", "error", err)
		return nil
	}
	cache.key, cache.metrics, cache.expiresAt = key.String(), result, time.Now().Add(30*time.Second)
	return result
}

func (s *ChannelMonitorService) userRatesForMonitors(ctx context.Context, ids []int64, userID int64) map[int64]float64 {
	reader, ok := s.repo.(monitorUserRatesReader)
	if !ok || userID <= 0 {
		return nil
	}
	result, err := reader.ListUserRatesForMonitors(ctx, ids, userID)
	if err != nil {
		slog.Warn("channel_monitor: user rates unavailable", "error", err)
		return nil
	}
	return result
}
