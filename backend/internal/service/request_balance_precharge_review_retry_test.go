//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This fixture owns all asynchronously accessed state. The wallet uses its own
// lock so checking retained money never races a persistence retry.
type retryPrechargeReviewRepository struct {
	BalancePrechargeReconciliationRepository
	wallet          *prechargeWalletFixture
	mu              sync.Mutex
	failures        int
	attempts        int
	reviews         []BalancePrechargeReviewEvidence
	blockAfterFirst bool
}

func (r *retryPrechargeReviewRepository) ReserveBalancePrecharge(ctx context.Context, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
	return r.wallet.ReserveBalancePrecharge(ctx, cmd)
}

func (r *retryPrechargeReviewRepository) ReleaseBalancePrecharge(ctx context.Context, id string, userID int64) (bool, error) {
	return r.wallet.ReleaseBalancePrecharge(ctx, id, userID)
}

func (r *retryPrechargeReviewRepository) MarkBalancePrechargeForReview(ctx context.Context, evidence *BalancePrechargeReviewEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.attempts++
	if r.blockAfterFirst && r.attempts > 1 {
		r.mu.Unlock()
		<-ctx.Done()
		return ctx.Err()
	}
	defer r.mu.Unlock()
	if r.attempts <= r.failures {
		return errors.New("transient review database outage")
	}
	r.reviews = append(r.reviews, *evidence)
	return nil
}

func (r *retryPrechargeReviewRepository) snapshot() (int, []BalancePrechargeReviewEvidence) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts, append([]BalancePrechargeReviewEvidence(nil), r.reviews...)
}

func retryPrechargeReviewFixture(t *testing.T) (context.Context, *retryPrechargeReviewRepository) {
	t.Helper()
	wallet := &prechargeWalletFixture{balance: .2, holds: make(map[string]float64)}
	r := &retryPrechargeReviewRepository{wallet: wallet, failures: 1}
	policy := &prechargePolicyFixture{policy: BalancePrechargeSettings{Enabled: true, Threshold: 1, Amount: .1}}
	ctx := newRequestBalancePrecharge(context.Background(), policy, r, nil)
	key := &APIKey{ID: 2, UserID: 1, GroupID: ptrPrechargeGroup(3), User: &User{ID: 1}}
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	SetBalancePrechargeFailureEvidence(ctx, BalancePrechargeFailureEvidence{Reason: "upstream_usage_missing", RequestID: "upstream-123", AccountID: 7, Model: "test-model"})
	BalancePrechargeUsageFinished(ctx, errors.New("billing transaction failed"))
	return ctx, r
}

func TestBalancePrechargeReviewRetry_PersistsAfterOneFinish(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "connected", true: "client canceled"}[canceled], func(t *testing.T) {
			parent, repo := retryPrechargeReviewFixture(t)
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			if canceled {
				cancel()
			}
			// Production calls Finish only once; recovery must happen by itself.
			FinishBalancePrecharge(ctx)
			require.Eventually(t, func() bool {
				_, reviews := repo.snapshot()
				return len(reviews) == 1
			}, 2*time.Second, 10*time.Millisecond)
			attempts, reviews := repo.snapshot()
			require.Equal(t, 2, attempts)
			require.Equal(t, BalancePrechargeReviewEvidence{
				PrechargeID: BalancePrechargeBillingID(ctx, 1, 2), UserID: 1,
				Reason: "billing_failed", RequestID: "upstream-123", AccountID: 7, Model: "test-model",
			}, reviews[0])
			repo.wallet.mu.Lock()
			balance, frozen := repo.wallet.balance, repo.wallet.frozen
			repo.wallet.mu.Unlock()
			require.InDelta(t, .1, balance, 1e-8)
			require.InDelta(t, .1, frozen, 1e-8, "persisting evidence must never refund a failed billing transaction")
		})
	}
}

func testPrechargeReviewRetryer() *balancePrechargeReviewRetryer {
	return &balancePrechargeReviewRetryer{
		slots: make(chan struct{}, 1), delays: []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond},
		timeout: time.Second, attemptTimeout: 100 * time.Millisecond,
	}
}

func requireReviewRetryFinished(t *testing.T, retryer *balancePrechargeReviewRetryer) {
	t.Helper()
	require.Eventually(t, func() bool { return len(retryer.slots) == 0 }, 2*time.Second, time.Millisecond)
}

func TestBalancePrechargeReviewRetry_WaitsForFinalUsageWorker(t *testing.T) {
	ctx, repo := retryPrechargeReviewFixture(t)
	retryer := testPrechargeReviewRetryer()
	requestPrecharge(ctx).reviewRetryer = retryer
	done := retainBalancePrechargeTask(ctx)
	FinishBalancePrecharge(ctx)
	attempts, _ := repo.snapshot()
	require.Zero(t, attempts, "in-flight usage must not become reviewable")
	done()
	requireReviewRetryFinished(t, retryer)
	attempts, reviews := repo.snapshot()
	require.Equal(t, 2, attempts)
	require.Len(t, reviews, 1)
}

func TestBalancePrechargeReviewRetry_DuplicateFinishUsesOneJobAndOriginalEvidence(t *testing.T) {
	ctx, repo := retryPrechargeReviewFixture(t)
	repo.failures = 2
	retryer := testPrechargeReviewRetryer()
	retryer.delays = []time.Duration{20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond}
	requestPrecharge(ctx).reviewRetryer = retryer
	FinishBalancePrecharge(ctx)
	SetBalancePrechargeFailureEvidence(ctx, BalancePrechargeFailureEvidence{Reason: "later unrelated evidence"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); FinishBalancePrecharge(ctx) }()
	}
	wg.Wait()
	requireReviewRetryFinished(t, retryer)
	attempts, reviews := repo.snapshot()
	require.Equal(t, 3, attempts, "duplicate Finish must not create another synchronous attempt or retry job")
	require.Len(t, reviews, 1)
	require.Equal(t, "billing_failed", reviews[0].Reason)
	require.Equal(t, "upstream-123", reviews[0].RequestID)
}

func TestBalancePrechargeReviewRetry_SettlementCancelsBackoff(t *testing.T) {
	ctx, repo := retryPrechargeReviewFixture(t)
	retryer := testPrechargeReviewRetryer()
	retryer.delays = []time.Duration{time.Hour}
	requestPrecharge(ctx).reviewRetryer = retryer
	FinishBalancePrecharge(ctx)
	// A terminal ledger transition occurs before the lifecycle is notified.
	_, err := repo.ReleaseBalancePrecharge(context.Background(), BalancePrechargeBillingID(ctx, 1, 2), 1)
	require.NoError(t, err)
	MarkBalancePrechargeSettled(ctx)
	require.Eventually(t, func() bool { return len(retryer.slots) == 0 }, 200*time.Millisecond, time.Millisecond)
	attempts, reviews := repo.snapshot()
	require.Equal(t, 1, attempts)
	require.Empty(t, reviews)
}

func TestBalancePrechargeReviewRetry_ExhaustionRetainsHoldAndDoesNotRespawn(t *testing.T) {
	ctx, repo := retryPrechargeReviewFixture(t)
	repo.failures = 100
	retryer := testPrechargeReviewRetryer()
	requestPrecharge(ctx).reviewRetryer = retryer
	FinishBalancePrecharge(ctx)
	requireReviewRetryFinished(t, retryer)
	for i := 0; i < 5; i++ {
		FinishBalancePrecharge(ctx)
	}
	attempts, reviews := repo.snapshot()
	require.Equal(t, 4, attempts, "one synchronous attempt plus only three background attempts")
	require.Empty(t, reviews)
	require.Zero(t, len(retryer.slots))
	repo.wallet.mu.Lock()
	defer repo.wallet.mu.Unlock()
	require.InDelta(t, .1, repo.wallet.frozen, 1e-8)
	require.InDelta(t, .1, repo.wallet.balance, 1e-8)
}

func TestBalancePrechargeReviewRetry_TotalDeadlineBoundsBlockedDatabase(t *testing.T) {
	ctx, repo := retryPrechargeReviewFixture(t)
	repo.blockAfterFirst = true
	retryer := testPrechargeReviewRetryer()
	retryer.timeout, retryer.attemptTimeout = 40*time.Millisecond, time.Hour
	requestPrecharge(ctx).reviewRetryer = retryer
	FinishBalancePrecharge(ctx)
	require.Eventually(t, func() bool { return len(retryer.slots) == 0 }, 300*time.Millisecond, time.Millisecond)
	attempts, reviews := repo.snapshot()
	require.Equal(t, 2, attempts)
	require.Empty(t, reviews)
}

func TestBalancePrechargeReviewRetry_CapacityExhaustionDoesNotSpawn(t *testing.T) {
	ctx, repo := retryPrechargeReviewFixture(t)
	retryer := testPrechargeReviewRetryer()
	retryer.slots <- struct{}{} // Another request already owns the sole retry slot.
	requestPrecharge(ctx).reviewRetryer = retryer
	FinishBalancePrecharge(ctx)
	<-retryer.slots
	FinishBalancePrecharge(ctx)
	attempts, reviews := repo.snapshot()
	require.Equal(t, 1, attempts)
	require.Empty(t, reviews)
	require.Zero(t, len(retryer.slots))
}
