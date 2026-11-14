//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUserMonitorProbeDeadlineFollowsActualInterval(t *testing.T) {
	checked := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		interval int
		jitter   int
		validFor time.Duration
	}{
		{"short probe scheduling tolerance", 15, 0, 120 * time.Second},
		{"interval and jitter", 60, 15, 135 * time.Second},
		{"hourly probe", 3600, 60, 7260 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := buildUserViewFromSummary(&ChannelMonitor{IntervalSeconds: tc.interval, JitterSeconds: tc.jitter}, MonitorStatusSummary{}, nil,
				[]*ChannelMonitorHistoryEntry{{CheckedAt: checked, Status: "operational"}})
			require.NotNil(t, view.ProbeValidUntil)
			require.Equal(t, checked.Add(tc.validFor), *view.ProbeValidUntil)
		})
	}
}

func TestUserMonitorWithoutProbeHistoryHasNoDeadline(t *testing.T) {
	view := buildUserViewFromSummary(&ChannelMonitor{IntervalSeconds: 60}, MonitorStatusSummary{}, nil, nil)
	require.Nil(t, view.ProbeValidUntil)
}
