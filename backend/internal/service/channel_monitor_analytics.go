package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ListUserAnalytics loads IQ history and usage aggregates independently from
// the availability-first list so slow analytical queries cannot delay cards.
func (s *ChannelMonitorService) ListUserAnalytics(ctx context.Context) ([]*UserMonitorAnalytics, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	monitors, err := s.repo.ListEnabled(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors for analytics: %w", err)
	}
	if len(monitors) == 0 {
		return []*UserMonitorAnalytics{}, nil
	}
	if err := s.refreshMonitorBindings(queryCtx, monitors); err != nil {
		return nil, fmt.Errorf("resolve monitor groups for analytics: %w", err)
	}

	ids, _, _ := collectMonitorIndexes(monitors)
	intelligence := make(map[int64]*MonitorIntelligence)
	usageMetrics := make(map[int64]map[string]MonitorUsageMetric)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		intelligence = s.intelligenceForMonitors(queryCtx, ids)
	}()
	go func() {
		defer workers.Done()
		usageMetrics = s.usageMetricsForMonitors(queryCtx, ids, monitors)
	}()
	workers.Wait()

	items := make([]*UserMonitorAnalytics, 0, len(monitors))
	for _, monitor := range monitors {
		items = append(items, &UserMonitorAnalytics{
			ID:           monitor.ID,
			Intelligence: intelligence[monitor.ID],
			UsageMetrics: usageMetrics[monitor.ID],
		})
	}
	return items, nil
}
