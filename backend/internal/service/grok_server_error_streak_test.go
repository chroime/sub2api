package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokServerErrorStreakRequiresBurst(t *testing.T) {
	store := &grokServerErrorStreakStore{items: make(map[int64]*grokServerErrorStreak)}
	now := time.Now()

	_, burst := store.record(1, now)
	require.False(t, burst, "single 5xx must not cool the account")
	_, burst = store.record(1, now.Add(10*time.Second))
	require.False(t, burst)
	count, burst := store.record(1, now.Add(20*time.Second))
	require.True(t, burst, "third consecutive 5xx within the window cools the account")
	require.Equal(t, grokServerErrorStreakThreshold, count)

	// Threshold resets the streak.
	_, burst = store.record(1, now.Add(30*time.Second))
	require.False(t, burst)
}

func TestGrokServerErrorStreakResetsOnSuccessAndWindow(t *testing.T) {
	store := &grokServerErrorStreakStore{items: make(map[int64]*grokServerErrorStreak)}
	now := time.Now()

	store.record(2, now)
	store.record(2, now.Add(time.Second))
	store.reset(2)
	_, burst := store.record(2, now.Add(2*time.Second))
	require.False(t, burst, "success clears the streak")

	store.record(3, now)
	store.record(3, now.Add(time.Second))
	_, burst = store.record(3, now.Add(grokServerErrorStreakWindow+2*time.Second))
	require.False(t, burst, "errors outside the window start a new streak")

	_, burst = store.record(4, now)
	require.False(t, burst, "streaks are per account")
}
