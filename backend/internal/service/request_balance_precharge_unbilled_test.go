package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Upstream failures without a bill must release the reservation only after the
// request ends, then wake the queue without depending on its polling fallback.
func TestRequestBalancePrechargeUnbilledOutcomesReleaseOnceAndWakeWaiters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		observe func(context.Context)
	}{
		{name: "successful response without usage", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
		}},
		{name: "connection reset", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, 0, errors.New("connection reset"))
		}},
		{name: "upstream deadline", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, 0, context.DeadlineExceeded)
		}},
		{name: "client canceled without usage", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, 0, context.Canceled)
		}},
		{name: "HTTP rate limit", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, http.StatusTooManyRequests, nil)
		}},
		{name: "HTTP server error", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, http.StatusServiceUnavailable, nil)
		}},
		{name: "streamed rate limit without usage", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
			SetBalancePrechargeFailureEvidence(ctx, BalancePrechargeFailureEvidence{Reason: "responses_stream_usage_missing"})
		}},
		{name: "exhausted failover without usage", observe: func(ctx context.Context) {
			ObserveBalancePrechargeUpstream(ctx, http.StatusServiceUnavailable, nil)
			StartBalancePrechargeUpstream(ctx)
			ObserveBalancePrechargeUpstream(ctx, http.StatusTooManyRequests, nil)
		}},
		{name: "ended request with unmatched upstream observation", observe: func(context.Context) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, policy, wallet, key := prechargeFixture(100)
			policy.policy.Amount, policy.policy.Threshold = 100, 1000
			waiter := waitQueueTestWaiter()
			var releaseCalls atomic.Int64
			repo := &waitQueueTestRepository{
				reserve: wallet.ReserveBalancePrecharge,
				release: func(ctx context.Context, id string, userID int64) (bool, error) {
					releaseCalls.Add(1)
					return wallet.ReleaseBalancePrecharge(ctx, id, userID)
				},
			}
			owner := newRequestBalancePrecharge(context.Background(), policy, repo, nil)
			requestPrecharge(owner).waiter = waiter
			require.NoError(t, ensureRequestBalancePrecharge(owner, key.User, key))
			StartBalancePrechargeUpstream(owner)
			tc.observe(owner)
			parent, cancel := context.WithCancel(context.Background())
			second := waitQueueTestContext(parent, policy, wallet, waiter)
			results := make(chan waitQueueTestAdmission, 1)
			var worker sync.WaitGroup
			worker.Add(1)
			go func() {
				defer worker.Done()
				results <- waitQueueTestAdmission{second, ensureRequestBalancePrecharge(second, key.User, key)}
			}()
			defer func() { cancel(); worker.Wait() }()
			waitQueueTestWaitLength(t, waiter, key.UserID, 1)
			require.Zero(t, releaseCalls.Load(), "an observed upstream error must not release a live request")
			FinishBalancePrecharge(owner)
			FinishBalancePrecharge(owner)
			require.Equal(t, int64(1), releaseCalls.Load(), "an ended unbilled request refunds exactly once")
			result := waitQueueTestReceive(t, results)
			require.NoError(t, result.err, "refund must wake the next request immediately")
			require.True(t, HasBalancePrecharge(second))
			FinishBalancePrecharge(second)
			require.Zero(t, waitQueueTestLength(waiter, key.UserID))
			wallet.mu.Lock()
			defer wallet.mu.Unlock()
			require.Equal(t, float64(100), wallet.balance)
			require.Zero(t, wallet.frozen)
			require.Empty(t, wallet.holds)
		})
	}
}

func TestRequestBalancePrechargeUnbilledFailureWaitsForOwnedTask(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
	SetBalancePrechargeFailureEvidence(ctx, BalancePrechargeFailureEvidence{Reason: "stream_timeout_without_usage"})
	task := WrapBalancePrechargeTask(ctx, func(context.Context) {
		// No billable result was produced; the owner still must finish this
		// detached task before releasing its reservation.
	})
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .1, wallet.frozen, 1e-8)
	task(context.Background())
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}

func TestRequestBalancePrechargeOneHundredUnbilledFailuresDoNotStrandWaiters(t *testing.T) {
	_, policy, wallet, key := prechargeFixture(400)
	policy.policy.Amount, policy.policy.Threshold = 100, 1000
	waiter := waitQueueTestWaiter()
	parent, cancel := context.WithCancel(context.Background())
	start := make(chan struct{})
	results := make(chan waitQueueTestAdmission, 100)
	var workers sync.WaitGroup
	for range 100 {
		ctx := waitQueueTestContext(parent, policy, wallet, waiter)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- waitQueueTestAdmission{ctx, ensureRequestBalancePrecharge(ctx, key.User, key)}
		}()
	}
	defer func() { cancel(); workers.Wait() }()
	close(start)
	firstWave := make([]context.Context, 0, 4)
	for range 4 {
		result := waitQueueTestReceive(t, results)
		require.NoError(t, result.err)
		firstWave = append(firstWave, result.ctx)
	}
	waitQueueTestWaitLength(t, waiter, key.UserID, 96)
	finishUnbilled := func(ctx context.Context) {
		StartBalancePrechargeUpstream(ctx)
		ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
		SetBalancePrechargeFailureEvidence(ctx, BalancePrechargeFailureEvidence{Reason: "responses_stream_usage_missing"})
		FinishBalancePrecharge(ctx)
		FinishBalancePrecharge(ctx)
	}
	for _, ctx := range firstWave {
		finishUnbilled(ctx)
	}
	for range 96 {
		result := waitQueueTestReceive(t, results)
		require.NoError(t, result.err)
		finishUnbilled(result.ctx)
	}
	workers.Wait()
	require.Zero(t, waitQueueTestLength(waiter, key.UserID))
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.Equal(t, float64(400), wallet.balance, "unbilled failures do not charge or strand money")
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}
