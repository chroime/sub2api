//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAvailabilityResetRemainingDecaysLinearly(t *testing.T) {
	resetAt := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	require.Equal(t, 1.0, availabilityResetRemaining(resetAt, resetAt))
	require.Equal(t, 0.5, availabilityResetRemaining(resetAt, resetAt.Add(3*24*time.Hour+12*time.Hour)))
	require.Zero(t, availabilityResetRemaining(resetAt, resetAt.Add(7*24*time.Hour)))
	require.Zero(t, availabilityResetRemaining(resetAt, resetAt.Add(8*24*time.Hour)))
}

func TestApplyAvailabilityResetCombinesDecayingBaselineWithRealChecks(t *testing.T) {
	resetAt := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{
		Version:       1,
		Model:         "gpt-5.5",
		TargetPct:     98,
		ResetAt:       resetAt,
		BaselineTotal: 100,
		BaselineOK:    98,
	}
	real := &ChannelMonitorAvailability{Model: "gpt-5.5", TotalChecks: 10, OperationalChecks: 8, AvailabilityPct: 80}

	result := applyAvailabilityReset(real, reset, resetAt.Add(3*24*time.Hour+12*time.Hour))

	// Half of the virtual baseline remains: (49 + 8) / (50 + 10) = 95%.
	require.InDelta(t, 95.0, result.AvailabilityPct, 0.0001)
	require.Equal(t, 60, result.TotalChecks)
	require.Equal(t, 57, result.OperationalChecks)
}

func TestApplyAvailabilityResetFallsBackToRealDataAfterSevenDays(t *testing.T) {
	resetAt := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{
		Version:       1,
		Model:         "gpt-5.5",
		TargetPct:     99,
		ResetAt:       resetAt,
		BaselineTotal: 100,
		BaselineOK:    99,
	}
	real := &ChannelMonitorAvailability{Model: "gpt-5.5", TotalChecks: 5, OperationalChecks: 4, AvailabilityPct: 80}

	result := applyAvailabilityReset(real, reset, resetAt.Add(7*24*time.Hour))

	require.Equal(t, 80.0, result.AvailabilityPct)
	require.Equal(t, 5, result.TotalChecks)
	require.Equal(t, 4, result.OperationalChecks)
}

func TestBuildResetTimelineHidesPreResetFailuresAndAddsSyntheticBars(t *testing.T) {
	resetAt := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{
		Version:      1,
		Model:        "gpt-5.5",
		TargetPct:    98,
		DegradedBars: 2,
		ResetAt:      resetAt,
	}
	oldFailure := &ChannelMonitorHistoryEntry{Model: "gpt-5.5", Status: MonitorStatusFailed, CheckedAt: resetAt.Add(-time.Minute)}
	newFailure := &ChannelMonitorHistoryEntry{Model: "gpt-5.5", Status: MonitorStatusFailed, CheckedAt: resetAt.Add(time.Minute)}
	newSuccess := &ChannelMonitorHistoryEntry{Model: "gpt-5.5", Status: MonitorStatusOperational, CheckedAt: resetAt.Add(2 * time.Minute)}

	result := buildResetTimeline([]*ChannelMonitorHistoryEntry{newSuccess, newFailure, oldFailure}, reset, resetAt.Add(time.Hour), 5)

	require.Len(t, result, 5)
	require.Same(t, newSuccess, result[0])
	require.Same(t, newFailure, result[1])
	// The default even layout spreads degraded bars across the synthetic slots.
	require.Equal(t, []string{
		MonitorStatusOperational,
		MonitorStatusFailed,
		MonitorStatusDegraded,
		MonitorStatusOperational,
		MonitorStatusDegraded,
	}, timelineStatuses(result))
	require.NotContains(t, result, oldFailure)
	for _, point := range result[2:] {
		require.Equal(t, reset.Model, point.Model)
		require.True(t, point.CheckedAt.Before(resetAt))
	}
}

func TestBuildResetTimelineRestoresRealHistoryWhenResetIsNilOrExpired(t *testing.T) {
	resetAt := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	entries := []*ChannelMonitorHistoryEntry{
		{Model: "gpt-5.5", Status: MonitorStatusFailed, CheckedAt: resetAt.Add(-time.Minute)},
		{Model: "gpt-5.5", Status: MonitorStatusOperational, CheckedAt: resetAt.Add(time.Minute)},
	}

	require.Equal(t, entries, buildResetTimeline(entries, nil, resetAt, 60))
	expired := &ChannelMonitorAvailabilityReset{Version: 1, Model: "gpt-5.5", ResetAt: resetAt}
	require.Equal(t, entries, buildResetTimeline(entries, expired, resetAt.Add(7*24*time.Hour), 60))
}

func TestBuildResetTimelineKeepsNewestFirstForRecentPointSelection(t *testing.T) {
	resetAt := time.Date(2026, 11, 14, 3, 39, 20, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{Version: 1, Model: "claude-opus-5", ResetAt: resetAt}
	for _, limit := range []int{1, 18, 60} {
		t.Run(fmt.Sprintf("%d points", limit), func(t *testing.T) {
			result := buildResetTimeline(nil, reset, resetAt.Add(5*time.Minute), limit)
			require.Len(t, result, limit)
			for index, point := range result {
				require.Equal(t, resetAt.Add(-time.Duration(index+1)*time.Minute), point.CheckedAt,
					"the API must return newest first so the page selects the most recent 18 points")
			}
		})
	}
}

func TestBuildResetTimelineKeepsDescendingTimeAcrossRealAndResetPoints(t *testing.T) {
	resetAt := time.Date(2026, 11, 14, 3, 39, 20, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{Version: 1, Model: "claude-opus-5", ResetAt: resetAt}
	newest := &ChannelMonitorHistoryEntry{Model: reset.Model, Status: MonitorStatusOperational, CheckedAt: resetAt.Add(2 * time.Minute)}
	older := &ChannelMonitorHistoryEntry{Model: reset.Model, Status: MonitorStatusFailed, CheckedAt: resetAt.Add(time.Minute)}
	preReset := &ChannelMonitorHistoryEntry{Model: reset.Model, Status: MonitorStatusFailed, CheckedAt: resetAt.Add(-time.Minute)}
	result := buildResetTimeline([]*ChannelMonitorHistoryEntry{newest, older, preReset}, reset, resetAt.Add(5*time.Minute), 60)
	require.Len(t, result, 60)
	require.Same(t, newest, result[0])
	require.Same(t, older, result[1])
	require.NotContains(t, result, preReset)
	for index := 1; index < len(result); index++ {
		require.True(t, result[index].CheckedAt.Before(result[index-1].CheckedAt), "all real and baseline points must follow one descending order")
	}
}

func TestBuildResetTimelineYellowLayoutsFollowChronologicalSides(t *testing.T) {
	resetAt := time.Date(2026, 11, 14, 3, 39, 20, 0, time.UTC)
	for _, layout := range []string{AvailabilityResetLayoutBlockOldest, AvailabilityResetLayoutBlockNewest, AvailabilityResetLayoutEven, AvailabilityResetLayoutRandom} {
		t.Run(layout, func(t *testing.T) {
			reset := &ChannelMonitorAvailabilityReset{Version: 1, Model: "claude-opus-5", ResetAt: resetAt, DegradedBars: 3, DegradedBarLayout: layout}
			result := buildResetTimeline(nil, reset, resetAt, 6)
			yellow := 0
			for index, point := range result {
				require.Equal(t, resetAt.Add(-time.Duration(index+1)*time.Minute), point.CheckedAt)
				if point.Status == MonitorStatusDegraded {
					yellow++
				}
				switch layout {
				case AvailabilityResetLayoutBlockOldest:
					require.Equal(t, index >= 3, point.Status == MonitorStatusDegraded, "oldest yellow bars belong on the left of the displayed timeline")
				case AvailabilityResetLayoutBlockNewest:
					require.Equal(t, index < 3, point.Status == MonitorStatusDegraded, "newest yellow bars belong on the right of the displayed timeline")
				}
			}
			require.Equal(t, 3, yellow)
			require.Equal(t, result, buildResetTimeline(nil, reset, resetAt, 6), "random layout must remain stable across refreshes")
		})
	}
}

func TestBuildResetTimelineKeepsMeasuredLatencyAndSimulatesResetBars(t *testing.T) {
	resetAt := time.Date(2026, 11, 14, 3, 39, 20, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{Version: 1, Model: "claude-opus-5", ResetAt: resetAt}
	latency := 1234
	measured := &ChannelMonitorHistoryEntry{Model: reset.Model, Status: MonitorStatusOperational, LatencyMs: &latency, CheckedAt: resetAt.Add(time.Minute)}
	result := buildTimelinePoints(buildResetTimeline([]*ChannelMonitorHistoryEntry{measured}, reset, resetAt.Add(2*time.Minute), 3))
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var points []map[string]any
	require.NoError(t, json.Unmarshal(raw, &points))
	require.Equal(t, float64(latency), points[0]["latency_ms"], "reset must preserve real measurements")
	for _, point := range points[1:] {
		require.IsType(t, float64(0), point["latency_ms"], "the user requested a simulated latency on reset bars")
		require.GreaterOrEqual(t, point["latency_ms"].(float64), float64(1500))
		require.LessOrEqual(t, point["latency_ms"].(float64), float64(2500))
	}
}

func TestBuildResetTimelineSimulatedLatencyIsStableWithinEachReset(t *testing.T) {
	resetAt := time.Date(2026, 11, 14, 3, 39, 20, 0, time.UTC)
	reset := &ChannelMonitorAvailabilityReset{Version: 1, Model: "claude-opus-5", ResetAt: resetAt}
	first := buildResetTimeline(nil, reset, resetAt, 60)
	refreshed := buildResetTimeline(nil, reset, resetAt.Add(time.Hour), 60)
	shorter := buildResetTimeline(nil, reset, resetAt.Add(time.Hour), 18)
	require.Equal(t, first, refreshed, "refreshing must not change the displayed simulation")
	require.Equal(t, first[:18], shorter, "selecting fewer recent points must retain each point's latency")
	latencies := make(map[int]bool)
	for _, point := range first {
		require.NotNil(t, point.LatencyMs)
		require.GreaterOrEqual(t, *point.LatencyMs, 1500)
		require.LessOrEqual(t, *point.LatencyMs, 2500)
		latencies[*point.LatencyMs] = true
	}
	require.Greater(t, len(latencies), 1, "all reset bars should not share one latency")
	nextReset := *reset
	nextReset.ResetAt = resetAt.Add(time.Minute)
	require.NotEqual(t, *first[0].LatencyMs, *buildResetTimeline(nil, &nextReset, nextReset.ResetAt, 60)[0].LatencyMs, "a new reset should produce new display values")
}

func timelineStatuses(entries []*ChannelMonitorHistoryEntry) []string {
	statuses := make([]string, 0, len(entries))
	for _, entry := range entries {
		statuses = append(statuses, entry.Status)
	}
	return statuses
}
