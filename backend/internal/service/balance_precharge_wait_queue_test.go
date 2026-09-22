package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type waitQueueTestRepository struct {
	reserve func(context.Context, *BalancePrechargeCommand) (*BalancePrechargeResult, error)
	release func(context.Context, string, int64) (bool, error)
}

func (r *waitQueueTestRepository) ReserveBalancePrecharge(ctx context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
	return r.reserve(ctx, cmd)
}

func (r *waitQueueTestRepository) ReleaseBalancePrecharge(ctx context.Context, id string, userID int64) (bool, error) {
	if r.release != nil {
		return r.release(ctx, id, userID)
	}
	return false, nil
}

type waitQueueTestAdmission struct {
	ctx context.Context
	err error
}

func waitQueueTestWaiter() *balancePrechargeWaiter {
	w := newBalancePrechargeWaiter()
	w.timeout = 3 * time.Second
	w.pollInterval = time.Hour // Notifications must make progress in these tests.
	return w
}

func waitQueueTestLength(w *balancePrechargeWaiter, userID int64) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if queue := w.users[userID]; queue != nil {
		return queue.requests.Len()
	}
	return 0
}

func waitQueueTestWaitLength(t *testing.T, w *balancePrechargeWaiter, userID int64, length int) {
	t.Helper()
	require.Eventually(t, func() bool { return waitQueueTestLength(w, userID) == length }, time.Second, time.Millisecond)
}

func waitQueueTestReceive(t *testing.T, results <-chan waitQueueTestAdmission) waitQueueTestAdmission {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued admission")
		return waitQueueTestAdmission{}
	}
}

func waitQueueTestContext(parent context.Context, policy *prechargePolicyFixture, wallet *prechargeWalletFixture, waiter *balancePrechargeWaiter) context.Context {
	ctx := newRequestBalancePrecharge(parent, policy, wallet, nil)
	requestPrecharge(ctx).waiter = waiter
	return ctx
}

// Capture changes the durable wallet first, then publishes the same settlement
// notification as the real billing worker. Integer amounts keep the in-memory
// fixture exact while checking conservation across a large number of requests.
func waitQueueTestCapture(t *testing.T, ctx context.Context, wallet *prechargeWalletFixture, key *APIKey, cost float64) {
	t.Helper()
	id := BalancePrechargeBillingID(ctx, key.UserID, key.ID)
	require.NotEmpty(t, id)
	wallet.mu.Lock()
	amount, exists := wallet.holds[id]
	if exists {
		wallet.balance += amount - cost
		wallet.frozen -= amount
		delete(wallet.holds, id)
	}
	wallet.mu.Unlock()
	require.True(t, exists, "each admitted request must own exactly one unsettled hold")
	MarkBalancePrechargeSettled(ctx)
	BalancePrechargeUsageFinished(ctx, nil)
	FinishBalancePrecharge(ctx)
}

func TestBalancePrechargeQueueOneHundredRequestsSettleInAffordableWaves(t *testing.T) {
	_, policy, wallet, key := prechargeFixture(400)
	policy.policy.Amount = 100
	policy.policy.Threshold = 1000
	w := waitQueueTestWaiter()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := make(chan struct{})
	results := make(chan waitQueueTestAdmission, 100)
	var workers sync.WaitGroup
	for range 100 {
		ctx := waitQueueTestContext(parent, policy, wallet, w)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- waitQueueTestAdmission{ctx, ensureRequestBalancePrecharge(ctx, key.User, key)}
		}()
	}
	close(start)
	firstWave := make([]context.Context, 0, 4)
	for range 4 {
		result := waitQueueTestReceive(t, results)
		require.NoError(t, result.err)
		firstWave = append(firstWave, result.ctx)
	}
	waitQueueTestWaitLength(t, w, key.UserID, 96)
	wallet.mu.Lock()
	require.Equal(t, float64(0), wallet.balance)
	require.Equal(t, float64(400), wallet.frozen)
	require.Len(t, wallet.holds, 4)
	wallet.mu.Unlock()
	select {
	case result := <-results:
		t.Fatalf("a fifth request completed before any frozen balance settled: %v", result.err)
	default:
	}
	for _, ctx := range firstWave {
		waitQueueTestCapture(t, ctx, wallet, key, 1)
	}
	for range 96 {
		result := waitQueueTestReceive(t, results)
		require.NoError(t, result.err, "temporarily frozen funds must not reject a waiting request")
		waitQueueTestCapture(t, result.ctx, wallet, key, 1)
	}
	workers.Wait()
	require.Zero(t, waitQueueTestLength(w, key.UserID))
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.Equal(t, float64(300), wallet.balance, "100 completed requests charged exactly one unit each")
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeQueueOnlyHeadProbesForOneHundredWaiters(t *testing.T) {
	w := waitQueueTestWaiter()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	probes := make(chan string, 1000)
	repo := &waitQueueTestRepository{reserve: func(_ context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
		probes <- cmd.ID
		return nil, ErrBalancePrechargeWaiting
	}}
	results := make(chan waitQueueTestAdmission, 100)
	for i := range 100 {
		cmd := &BalancePrechargeCommand{ID: fmt.Sprintf("request-%d", i), UserID: 1}
		go func() {
			_, err := w.reserve(parent, repo, cmd)
			results <- waitQueueTestAdmission{err: err}
		}()
	}
	waitQueueTestWaitLength(t, w, 1, 100)
	var head string
	select {
	case head = <-probes:
	case <-time.After(time.Second):
		t.Fatal("queue head did not probe")
	}
	for range 3 {
		w.notify(1)
		select {
		case id := <-probes:
			require.Equal(t, head, id, "followers must not independently poll the database")
		case <-time.After(time.Second):
			t.Fatal("head did not retry after a settlement notification")
		}
	}
	select {
	case id := <-probes:
		t.Fatalf("unexpected concurrent probe from request %q", id)
	default:
	}
	cancel()
	for range 100 {
		require.ErrorIs(t, waitQueueTestReceive(t, results).err, context.Canceled)
	}
	require.Zero(t, waitQueueTestLength(w, 1))
}

func TestBalancePrechargeQueuePreservesAdmissionOrder(t *testing.T) {
	w := waitQueueTestWaiter()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var unlocked atomic.Bool
	var orderMu sync.Mutex
	var order []string
	repo := &waitQueueTestRepository{reserve: func(_ context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
		if !unlocked.Load() {
			return nil, ErrBalancePrechargeWaiting
		}
		orderMu.Lock()
		order = append(order, cmd.ID)
		orderMu.Unlock()
		return &BalancePrechargeResult{}, nil
	}}
	results := make(chan waitQueueTestAdmission, 8)
	var expected []string
	for i := range 8 {
		cmd := &BalancePrechargeCommand{ID: fmt.Sprintf("request-%d", i), UserID: 1}
		expected = append(expected, cmd.ID)
		go func() {
			_, err := w.reserve(parent, repo, cmd)
			results <- waitQueueTestAdmission{err: err}
		}()
		waitQueueTestWaitLength(t, w, 1, i+1)
	}
	unlocked.Store(true)
	w.notify(1)
	for range 8 {
		require.NoError(t, waitQueueTestReceive(t, results).err)
	}
	orderMu.Lock()
	require.Equal(t, expected, order)
	orderMu.Unlock()
	require.Zero(t, waitQueueTestLength(w, 1))
}

func TestBalancePrechargeQueueCanceledHeadAndFollowerLeaveNoHolds(t *testing.T) {
	_, policy, wallet, key := prechargeFixture(100)
	policy.policy.Amount = 100
	policy.policy.Threshold = 1000
	w := waitQueueTestWaiter()
	owner := waitQueueTestContext(context.Background(), policy, wallet, w)
	require.NoError(t, ensureRequestBalancePrecharge(owner, key.User, key))
	results := make([]chan waitQueueTestAdmission, 3)
	cancels := make([]context.CancelFunc, 3)
	for i := range 3 {
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancels[i] = cancel
		ctx := waitQueueTestContext(parent, policy, wallet, w)
		results[i] = make(chan waitQueueTestAdmission, 1)
		go func() { results[i] <- waitQueueTestAdmission{ctx, ensureRequestBalancePrecharge(ctx, key.User, key)} }()
		waitQueueTestWaitLength(t, w, key.UserID, i+1)
	}
	cancels[1]()
	require.ErrorIs(t, waitQueueTestReceive(t, results[1]).err, context.Canceled)
	waitQueueTestWaitLength(t, w, key.UserID, 2)
	cancels[0]()
	require.ErrorIs(t, waitQueueTestReceive(t, results[0]).err, context.Canceled)
	waitQueueTestWaitLength(t, w, key.UserID, 1)
	FinishBalancePrecharge(owner)
	last := waitQueueTestReceive(t, results[2])
	require.NoError(t, last.err)
	FinishBalancePrecharge(last.ctx)
	require.Zero(t, waitQueueTestLength(w, key.UserID))
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.Equal(t, float64(100), wallet.balance)
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeQueueExternalReleaseIsObservedByPolling(t *testing.T) {
	_, policy, wallet, key := prechargeFixture(100)
	policy.policy.Amount = 100
	policy.policy.Threshold = 1000
	w := waitQueueTestWaiter()
	w.pollInterval = 5 * time.Millisecond
	owner := waitQueueTestContext(context.Background(), policy, wallet, w)
	require.NoError(t, ensureRequestBalancePrecharge(owner, key.User, key))
	ctx := waitQueueTestContext(context.Background(), policy, wallet, w)
	pending := make(chan struct{})
	var observed sync.Once
	requestPrecharge(ctx).repo = &waitQueueTestRepository{
		reserve: func(parent context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
			result, err := wallet.ReserveBalancePrecharge(parent, cmd)
			if errors.Is(err, ErrBalancePrechargeWaiting) {
				observed.Do(func() { close(pending) })
			}
			return result, err
		},
		release: wallet.ReleaseBalancePrecharge,
	}
	results := make(chan waitQueueTestAdmission, 1)
	go func() { results <- waitQueueTestAdmission{ctx, ensureRequestBalancePrecharge(ctx, key.User, key)} }()
	select {
	case <-pending:
	case <-time.After(time.Second):
		t.Fatal("waiting request did not first observe the frozen wallet")
	}
	// Simulate a different server process committing a refund without a local
	// notification. The next polling transaction must discover the free funds.
	released, err := wallet.ReleaseBalancePrecharge(context.Background(), BalancePrechargeBillingID(owner, key.UserID, key.ID), key.UserID)
	require.NoError(t, err)
	require.True(t, released)
	result := waitQueueTestReceive(t, results)
	require.NoError(t, result.err)
	MarkBalancePrechargeSettled(owner)
	FinishBalancePrecharge(owner)
	FinishBalancePrecharge(result.ctx)
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.Equal(t, float64(100), wallet.balance)
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeQueueSettlementCanRevealTrueInsufficiency(t *testing.T) {
	_, policy, wallet, key := prechargeFixture(100)
	policy.policy.Amount = 100
	policy.policy.Threshold = 1000
	w := waitQueueTestWaiter()
	owner := waitQueueTestContext(context.Background(), policy, wallet, w)
	require.NoError(t, ensureRequestBalancePrecharge(owner, key.User, key))
	ctx := waitQueueTestContext(context.Background(), policy, wallet, w)
	results := make(chan waitQueueTestAdmission, 1)
	go func() { results <- waitQueueTestAdmission{ctx, ensureRequestBalancePrecharge(ctx, key.User, key)} }()
	waitQueueTestWaitLength(t, w, key.UserID, 1)
	waitQueueTestCapture(t, owner, wallet, key, 1)
	require.ErrorIs(t, waitQueueTestReceive(t, results).err, ErrInsufficientBalance)
	FinishBalancePrecharge(ctx)
	require.Zero(t, waitQueueTestLength(w, key.UserID))
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.Equal(t, float64(99), wallet.balance)
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeQueueTimeoutAndTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		name          string
		want          error
		parentTimeout time.Duration
		heartbeatErr  error
	}{
		{name: "admission timeout", want: ErrBalancePrechargeWaitTimeout},
		{name: "parent deadline", want: context.DeadlineExceeded, parentTimeout: 15 * time.Millisecond},
		{name: "transport failed", heartbeatErr: errors.New("stream closed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := waitQueueTestWaiter()
			w.timeout = 40 * time.Millisecond
			w.heartbeatInterval = 3 * time.Millisecond
			parent := context.Background()
			if tc.parentTimeout != 0 {
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, tc.parentTimeout)
				defer cancel()
			}
			beats := 0
			parent = WithBalancePrechargeWaitHeartbeat(parent, func() error {
				beats++
				return tc.heartbeatErr
			})
			repo := &waitQueueTestRepository{reserve: func(context.Context, *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
				return nil, ErrBalancePrechargeWaiting
			}}
			result, err := w.reserve(parent, repo, &BalancePrechargeCommand{ID: "waiting", UserID: 1})
			require.Nil(t, result)
			if tc.heartbeatErr != nil {
				require.ErrorIs(t, err, tc.heartbeatErr)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			if tc.heartbeatErr != nil {
				require.Equal(t, 1, beats, "a failed heartbeat must stop admission immediately")
			}
			require.Zero(t, waitQueueTestLength(w, 1))
		})
	}
}

func TestBalancePrechargeQueueFollowerHeartbeatCanCancelWithoutProbing(t *testing.T) {
	w := waitQueueTestWaiter()
	w.heartbeatInterval = 3 * time.Millisecond
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var probes atomic.Int64
	repo := &waitQueueTestRepository{reserve: func(context.Context, *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
		probes.Add(1)
		return nil, ErrBalancePrechargeWaiting
	}}
	head := make(chan waitQueueTestAdmission, 1)
	go func() {
		_, err := w.reserve(parent, repo, &BalancePrechargeCommand{ID: "head", UserID: 1})
		head <- waitQueueTestAdmission{err: err}
	}()
	waitQueueTestWaitLength(t, w, 1, 1)
	transportError := errors.New("follower disconnected")
	follower := WithBalancePrechargeWaitHeartbeat(parent, func() error { return transportError })
	_, err := w.reserve(follower, repo, &BalancePrechargeCommand{ID: "follower", UserID: 1})
	require.ErrorIs(t, err, transportError)
	require.Equal(t, int64(1), probes.Load())
	require.Equal(t, 1, waitQueueTestLength(w, 1))
	cancel()
	require.ErrorIs(t, waitQueueTestReceive(t, head).err, context.Canceled)
	require.Zero(t, waitQueueTestLength(w, 1))
}

func TestBalancePrechargeQueueDifferentUsersProceedIndependently(t *testing.T) {
	w := waitQueueTestWaiter()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &waitQueueTestRepository{reserve: func(_ context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
		if cmd.UserID == 1 {
			return nil, ErrBalancePrechargeWaiting
		}
		return &BalancePrechargeResult{Reserved: true, Amount: 100, NewBalance: 900}, nil
	}}
	first := make(chan waitQueueTestAdmission, 1)
	go func() {
		_, err := w.reserve(parent, repo, &BalancePrechargeCommand{ID: "first-user", UserID: 1})
		first <- waitQueueTestAdmission{err: err}
	}()
	waitQueueTestWaitLength(t, w, 1, 1)
	result, err := w.reserve(parent, repo, &BalancePrechargeCommand{ID: "second-user", UserID: 2})
	require.NoError(t, err)
	require.True(t, result.Reserved)
	require.Equal(t, 1, waitQueueTestLength(w, 1))
	require.Zero(t, waitQueueTestLength(w, 2))
	cancel()
	require.ErrorIs(t, waitQueueTestReceive(t, first).err, context.Canceled)
	require.Zero(t, waitQueueTestLength(w, 1))
}

func TestBalancePrechargeQueueCancellationAfterCommitRefundsExactlyOnce(t *testing.T) {
	_, policy, wallet, key := prechargeFixture(100)
	policy.policy.Amount = 100
	policy.policy.Threshold = 1000
	w := waitQueueTestWaiter()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := waitQueueTestContext(parent, policy, wallet, w)
	reserveCalls, releaseCalls := 0, 0
	var releaseContextError error
	requestPrecharge(ctx).repo = &waitQueueTestRepository{
		reserve: func(probe context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
			reserveCalls++
			result, err := wallet.ReserveBalancePrecharge(probe, cmd)
			// The database commit succeeded just as the client disconnected.
			// Admission must take ownership of the result before returning.
			cancel()
			return result, err
		},
		release: func(cleanup context.Context, id string, userID int64) (bool, error) {
			releaseCalls++
			releaseContextError = cleanup.Err()
			return wallet.ReleaseBalancePrecharge(cleanup, id, userID)
		},
	}
	err := ensureRequestBalancePrecharge(ctx, key.User, key)
	require.ErrorIs(t, err, context.Canceled, "the handler must not receive a successful admission after disconnect")
	require.Equal(t, 1, reserveCalls)
	require.Equal(t, 1, releaseCalls)
	require.NoError(t, releaseContextError, "refund must use a live context independent of the disconnected request")
	require.False(t, HasBalancePrecharge(ctx))
	require.True(t, requestPrecharge(ctx).settled)
	require.False(t, requestPrecharge(ctx).upstreamAttempted)
	require.Zero(t, waitQueueTestLength(w, key.UserID))
	FinishBalancePrecharge(ctx)
	FinishBalancePrecharge(ctx)
	require.Equal(t, 1, releaseCalls, "deferred cleanup must not refund an already released hold again")
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.Equal(t, float64(100), wallet.balance)
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeQueueDatabaseFailureDoesNotRetry(t *testing.T) {
	for _, afterWait := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_wait_%t", afterWait), func(t *testing.T) {
			_, policy, wallet, key := prechargeFixture(100)
			policy.policy.Amount = 100
			policy.policy.Threshold = 1000
			w := waitQueueTestWaiter()
			w.pollInterval = time.Millisecond
			ctx := waitQueueTestContext(context.Background(), policy, wallet, w)
			databaseError := errors.New("database connection unavailable")
			reserveCalls, releaseCalls := 0, 0
			requestPrecharge(ctx).repo = &waitQueueTestRepository{
				reserve: func(context.Context, *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
					reserveCalls++
					if afterWait && reserveCalls == 1 {
						return nil, ErrBalancePrechargeWaiting
					}
					return nil, databaseError
				},
				release: func(context.Context, string, int64) (bool, error) {
					releaseCalls++
					return false, nil
				},
			}
			err := ensureRequestBalancePrecharge(ctx, key.User, key)
			require.ErrorIs(t, err, ErrBillingServiceUnavailable)
			require.ErrorIs(t, err, databaseError)
			wantCalls := 1
			if afterWait {
				wantCalls++
			}
			require.Equal(t, wantCalls, reserveCalls, "database failures must terminate admission without retrying")
			require.Zero(t, waitQueueTestLength(w, key.UserID))
			require.False(t, requestPrecharge(ctx).checked)
			require.False(t, HasBalancePrecharge(ctx))
			FinishBalancePrecharge(ctx)
			require.Zero(t, releaseCalls, "a failed transaction does not grant ownership of any hold")
			wallet.mu.Lock()
			defer wallet.mu.Unlock()
			require.Equal(t, float64(100), wallet.balance)
			require.Zero(t, wallet.frozen)
			require.Empty(t, wallet.holds)
		})
	}
}
