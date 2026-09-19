package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type prechargePolicyFixture struct {
	policy BalancePrechargeSettings
	err    error
}

func (p *prechargePolicyFixture) GetEffectiveBalancePrechargeSettings(context.Context, int64) (BalancePrechargeSettings, error) {
	return p.policy, p.err
}

type prechargeWalletFixture struct {
	mu              sync.Mutex
	balance, frozen float64
	holds           map[string]float64
}

func (r *prechargeWalletFixture) ReserveBalancePrecharge(_ context.Context, c *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if amount, ok := r.holds[c.ID]; ok {
		return &BalancePrechargeResult{Reserved: true, Amount: amount, NewBalance: r.balance}, nil
	}
	required := c.MinimumBalance
	if r.balance < c.Threshold && c.Amount > required {
		required = c.Amount
	}
	if r.balance <= 0 || r.balance < required {
		if r.frozen > 0 && r.balance+r.frozen >= required && r.balance+r.frozen > 0 {
			return nil, ErrBalancePrechargeWaiting
		}
		return nil, ErrInsufficientBalance
	}
	if r.balance >= c.Threshold {
		return &BalancePrechargeResult{NewBalance: r.balance}, nil
	}
	r.balance -= c.Amount
	r.frozen += c.Amount
	r.holds[c.ID] = c.Amount
	return &BalancePrechargeResult{Reserved: true, Amount: c.Amount, NewBalance: r.balance}, nil
}
func (r *prechargeWalletFixture) ReleaseBalancePrecharge(_ context.Context, id string, _ int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	amount, ok := r.holds[id]
	if !ok {
		return false, nil
	}
	r.balance += amount
	r.frozen -= amount
	delete(r.holds, id)
	return true, nil
}
func prechargeFixture(balance float64) (context.Context, *prechargePolicyFixture, *prechargeWalletFixture, *APIKey) {
	p := &prechargePolicyFixture{policy: BalancePrechargeSettings{Enabled: true, Threshold: 1, Amount: .1}}
	r := &prechargeWalletFixture{balance: balance, holds: make(map[string]float64)}
	ctx := newRequestBalancePrecharge(context.Background(), p, r, nil)
	key := &APIKey{ID: 2, UserID: 1, GroupID: ptrPrechargeGroup(3), Group: &Group{ID: 3}, User: &User{ID: 1}}
	return ctx, p, r, key
}
func ptrPrechargeGroup(v int64) *int64 { return &v }

func TestRequestBalancePrechargeRefundsOnlyAfterQueuedUsageCompletes(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.15)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	done := retainBalancePrechargeTask(ctx)
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .05, wallet.balance, 1e-8)
	require.InDelta(t, .1, wallet.frozen, 1e-8)
	BalancePrechargeUsageFinished(ctx, nil) // A successfully processed zero-cost result.
	done()
	require.InDelta(t, .15, wallet.balance, 1e-8)
	require.InDelta(t, 0, wallet.frozen, 1e-8)
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .15, wallet.balance, 1e-8)
}

func TestRequestBalancePrechargeSnapshotsPolicyAcrossRetries(t *testing.T) {
	ctx, policy, wallet, key := prechargeFixture(.3)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	policy.policy.Amount = .2
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.InDelta(t, .1, wallet.frozen, 1e-8)
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .3, wallet.balance, 1e-8)
}

func TestRequestBalancePrechargeRefundsEndedUnbilledUpstream(t *testing.T) {
	for _, networkError := range []bool{false, true} {
		ctx, _, wallet, key := prechargeFixture(.2)
		require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
		StartBalancePrechargeUpstream(ctx)
		if networkError {
			ObserveBalancePrechargeUpstream(ctx, 0, errors.New("connection reset"))
		} else {
			ObserveBalancePrechargeUpstream(ctx, 200, nil)
		}
		FinishBalancePrecharge(ctx)
		require.Zero(t, wallet.frozen)
		require.InDelta(t, .2, wallet.balance, 1e-8)
		require.Empty(t, wallet.holds)
	}
}

func TestRequestBalancePrechargeRefundsRejectedUpstream(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, 400, nil)
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.InDelta(t, 0, wallet.frozen, 1e-8)
}

func TestRequestBalancePrechargePropagatesSettingsAndBalanceFailures(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.05)
	require.ErrorIs(t, ensureRequestBalancePrecharge(ctx, key.User, key), ErrInsufficientBalance)
	require.InDelta(t, .05, wallet.balance, 1e-8)
	ctx, policy, _, key := prechargeFixture(.2)
	policy.err = errors.New("settings unavailable")
	require.Error(t, ensureRequestBalancePrecharge(ctx, key.User, key))
}

func TestRequestBalancePrechargeCopiedToWorkerPreservesOwnership(t *testing.T) {
	ctx, _, _, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	worker := CopyBalancePrechargeContext(ctx, context.Background())
	require.NotEmpty(t, BalancePrechargeBillingID(worker, 1, 2))
	require.Equal(t, BalancePrechargeBillingID(ctx, 1, 2), BalancePrechargeBillingID(worker, 1, 2))
	require.Empty(t, BalancePrechargeBillingID(worker, 99, 2))
	require.Empty(t, BalancePrechargeBillingID(worker, 1, 99))
}

func TestRequestBalancePrechargeRetainsFailedBillingForReconciliation(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	done := retainBalancePrechargeTask(ctx)
	BalancePrechargeUsageFinished(ctx, errors.New("database unavailable"))
	FinishBalancePrecharge(ctx)
	done()
	require.InDelta(t, .1, wallet.frozen, 1e-8)
}

func TestRequestBalancePrechargeWorkerPanicRetainsHold(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	task := WrapBalancePrechargeTask(ctx, func(context.Context) { panic("before billing") })
	FinishBalancePrecharge(ctx)
	require.Panics(t, func() { task(context.Background()) })
	require.InDelta(t, .1, wallet.frozen, 1e-8)
	require.InDelta(t, .1, wallet.balance, 1e-8)
}

func TestRequestBalancePrechargeWorkerSurvivesClientCancellation(t *testing.T) {
	parent, _, wallet, key := prechargeFixture(.2)
	ctx, cancel := context.WithCancel(parent)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	task := WrapBalancePrechargeTask(ctx, func(worker context.Context) {
		require.NoError(t, worker.Err())
		require.Equal(t, BalancePrechargeBillingID(ctx, 1, 2), BalancePrechargeBillingID(worker, 1, 2))
		BalancePrechargeUsageFinished(worker, nil)
	})
	cancel()
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .1, wallet.frozen, 1e-8)
	task(context.Background())
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.Zero(t, wallet.frozen)
}

func TestRequestBalancePrechargeRefundsZeroCostRetryAfterUnbilledAttempt(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, 0, errors.New("upstream response lost"))
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, 400, nil)
	BalancePrechargeUsageFinished(ctx, nil)
	FinishBalancePrecharge(ctx)
	require.Zero(t, wallet.frozen, "ended unbilled attempts must not permanently reserve the user's money")
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.Empty(t, wallet.holds)
}

func TestRequestBalancePrechargeUpstreamServerFailureWithoutBillRefundsHold(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, 500, nil)
	FinishBalancePrecharge(ctx)
	require.Zero(t, wallet.frozen, "an upstream error by itself must not become a billing failure")
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.Empty(t, wallet.holds)
}
