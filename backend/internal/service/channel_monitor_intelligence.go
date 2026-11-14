package service

import (
	"context"
	"log/slog"
	"time"
)

// MonitorIntelligence is a public, aggregate-only view of local group IQ checks.
// It deliberately contains no accounts, prompts, responses, or credentials.
type MonitorIntelligence struct {
	WindowEnd time.Time                   `json:"window_end"`
	Series    []MonitorIntelligenceSeries `json:"series"`
}

// Separate model/effort histories keep one test target's verdict from being
// presented as evidence about a different target in the same local group.
type MonitorIntelligenceSeries struct {
	Model  string                     `json:"model"`
	Effort string                     `json:"effort"`
	Points []MonitorIntelligencePoint `json:"points"`
}
type MonitorIntelligencePoint struct {
	ID        string    `json:"id,omitempty"` // opaque run ID for deduplicating monitor projections
	Status    string    `json:"status"`       // pass, fail, error, pending; missing checks are unknown
	CheckedAt time.Time `json:"checked_at"`
}
type monitorIntelligenceReader interface {
	ListIntelligenceForMonitors(context.Context, []int64, time.Time) (map[int64]*MonitorIntelligence, error)
}

func (s *ChannelMonitorService) intelligenceForMonitors(ctx context.Context, ids []int64) map[int64]*MonitorIntelligence {
	reader, ok := s.repo.(monitorIntelligenceReader)
	if !ok {
		return nil
	}
	result, err := reader.ListIntelligenceForMonitors(ctx, ids, time.Now().UTC())
	if err != nil {
		slog.Warn("channel_monitor: intelligence summary unavailable", "error", err)
		return nil
	}
	return result
}
