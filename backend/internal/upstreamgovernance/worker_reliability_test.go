package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledNextAtAddsStableJitterPerSite(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	first := scheduledNextAt(now, 60, 0, 1, scheduleFastObservation)
	second := scheduledNextAt(now, 60, 0, 2, scheduleFastObservation)

	require.GreaterOrEqual(t, first, now.Add(60*time.Second))
	require.LessOrEqual(t, first, now.Add(66*time.Second))
	require.GreaterOrEqual(t, second, now.Add(60*time.Second))
	require.LessOrEqual(t, second, now.Add(66*time.Second))
	require.NotEqual(t, first, second, "different sites should not all wake on the same second")
	require.Equal(t, first, scheduledNextAt(now, 60, 0, 1, scheduleFastObservation), "jitter must be deterministic across restarts")
}

func TestScheduledBackoffSaturatesInsteadOfFallingBackToCadence(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		interval int64
		failures int
		want     time.Duration
	}{
		{60, 1, 120 * time.Second}, {60, 4, 15 * time.Minute},
		{600, 1, 15 * time.Minute}, {600, 5, 15 * time.Minute},
		{1200, 5, 20 * time.Minute}, {60, 0, time.Minute},
	} {
		require.Equal(t, now.Add(tc.want), scheduledNextAt(now, tc.interval, tc.failures, 1, scheduleCollection))
	}
}

type deadlineMemoryStore struct {
	*fastMemoryStore
	nextSite *time.Time
	err      error
}

func (m *deadlineMemoryStore) NextSiteDueAt(context.Context) (*time.Time, error) {
	return m.nextSite, m.err
}

func TestWorkerWaitIncludesFullCollectionWhenFastObservationIsDisabled(t *testing.T) {
	now := time.Now().UTC()
	full := now.Add(5 * time.Second)
	store := &deadlineMemoryStore{fastMemoryStore: &fastMemoryStore{memoryStore: &memoryStore{}}, nextSite: &full}
	s := NewService(store, nil, nil, nil, false)
	s.now = func() time.Time { return now }
	require.Equal(t, 5*time.Second, s.workerWait(t.Context()))
	store.fastDue = []Site{{NextFastObserveAt: now.Add(2 * time.Second)}}
	require.Equal(t, 2*time.Second, s.workerWait(t.Context()))
	store.nextSite = &now
	require.Equal(t, 100*time.Millisecond, s.workerWait(t.Context()))
	store.fastDue = nil
	store.err = errors.New("database unavailable")
	require.Equal(t, time.Minute, s.workerWait(t.Context()))
}

func TestScheduledNextAtBacksOffFailuresAndCapsRetryDelay(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	first := scheduledNextAt(now, 5, 1, 7, scheduleCollection)
	second := scheduledNextAt(now, 5, 2, 7, scheduleCollection)
	large := scheduledNextAt(now, 5, maxCollectionFailures+4, 7, scheduleCollection)

	require.GreaterOrEqual(t, first, now.Add(10*time.Second))
	require.GreaterOrEqual(t, second, now.Add(20*time.Second))
	require.LessOrEqual(t, large, now.Add(maxCollectionBackoff+6*time.Second))
	// A configured cadence remains the floor even after a failure.
	require.GreaterOrEqual(t, scheduledNextAt(now, 1200, 1, 7, scheduleCollection), now.Add(1200*time.Second))
}
