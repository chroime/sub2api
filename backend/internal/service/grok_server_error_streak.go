package service

import (
	"sync"
	"time"
)

// Grok transient 5xx streak.
//
// xAI 502/503 "Internal error during token generation" responses are usually
// isolated: the next request on the same OAuth account succeeds. Cooling the
// whole account on a single 5xx removed healthy accounts from the pool (the
// request itself already fails over). Only a burst of consecutive 5xx within a
// short window is treated as an account-level outage. Any successful upstream
// response resets the streak. State is process-local, like the other Grok
// soft-blocks.
const (
	grokServerErrorStreakWindow    = 2 * time.Minute
	grokServerErrorStreakThreshold = 3
)

type grokServerErrorStreak struct {
	count int
	first time.Time
}

type grokServerErrorStreakStore struct {
	mu    sync.Mutex
	items map[int64]*grokServerErrorStreak
}

var globalGrokServerErrorStreaks = &grokServerErrorStreakStore{
	items: make(map[int64]*grokServerErrorStreak),
}

// recordGrokServerError counts one upstream 5xx for the account and reports
// whether the streak reached the threshold (the account should be cooled).
// Reaching the threshold resets the streak so the next cooldown needs a new
// burst after the account comes back.
func recordGrokServerError(accountID int64, now time.Time) (int, bool) {
	return globalGrokServerErrorStreaks.record(accountID, now)
}

// resetGrokServerErrorStreak clears the streak after a successful response.
func resetGrokServerErrorStreak(accountID int64) {
	globalGrokServerErrorStreaks.reset(accountID)
}

func (s *grokServerErrorStreakStore) record(accountID int64, now time.Time) (int, bool) {
	if accountID <= 0 {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	streak, ok := s.items[accountID]
	if !ok || now.Sub(streak.first) > grokServerErrorStreakWindow {
		streak = &grokServerErrorStreak{first: now}
		s.items[accountID] = streak
	}
	streak.count++
	count := streak.count
	if count >= grokServerErrorStreakThreshold {
		delete(s.items, accountID)
		return count, true
	}
	return count, false
}

func (s *grokServerErrorStreakStore) reset(accountID int64) {
	if accountID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.items, accountID)
	s.mu.Unlock()
}
