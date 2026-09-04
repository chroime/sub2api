//go:build unit

package service

import (
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
	require.Equal(t, []string{
		MonitorStatusOperational,
		MonitorStatusFailed,
		MonitorStatusDegraded,
		MonitorStatusDegraded,
		MonitorStatusOperational,
	}, timelineStatuses(result))
	for _, point := range result {
		require.NotEqual(t, oldFailure.CheckedAt, point.CheckedAt)
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

func timelineStatuses(entries []*ChannelMonitorHistoryEntry) []string {
	statuses := make([]string, 0, len(entries))
	for _, entry := range entries {
		statuses = append(statuses, entry.Status)
	}
	return statuses
}
