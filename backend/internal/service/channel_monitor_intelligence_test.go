//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type intelligenceMonitorRepo struct {
	sortChannelMonitorServiceRepo
	rows  map[int64]*MonitorIntelligence
	err   error
	calls int
}

func (r *intelligenceMonitorRepo) ListIntelligenceForMonitors(_ context.Context, ids []int64, _ time.Time) (map[int64]*MonitorIntelligence, error) {
	r.calls++
	return r.rows, r.err
}
func TestChannelMonitorIntelligenceBatchEnrichment(t *testing.T) {
	now := time.Now().UTC()
	repo := &intelligenceMonitorRepo{sortChannelMonitorServiceRepo: sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{{ID: 1, Enabled: true}, {ID: 2, Enabled: true}}}, rows: map[int64]*MonitorIntelligence{
		1: {WindowEnd: now, Series: []MonitorIntelligenceSeries{{Model: "local-model", Effort: "medium", Points: []MonitorIntelligencePoint{{Status: "pass", CheckedAt: now}}}}},
	}}
	views, err := NewChannelMonitorService(repo, nil).ListUserView(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, "local-model", views[0].Intelligence.Series[0].Model)
	require.Equal(t, "pass", views[0].Intelligence.Series[0].Points[0].Status)
	require.Nil(t, views[1].Intelligence)
}
func TestChannelMonitorIntelligenceFailureKeepsProbeView(t *testing.T) {
	repo := &intelligenceMonitorRepo{sortChannelMonitorServiceRepo: sortChannelMonitorServiceRepo{monitors: []*ChannelMonitor{{ID: 1, Name: "probe", Enabled: true}}}, err: errors.New("unavailable")}
	views, err := NewChannelMonitorService(repo, nil).ListUserView(context.Background())
	require.NoError(t, err)
	require.Equal(t, "probe", views[0].Name)
	require.Nil(t, views[0].Intelligence)
}
