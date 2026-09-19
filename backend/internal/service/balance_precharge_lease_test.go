//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type leasePrechargeFixture struct {
	*prechargeWalletFixture
	renewals atomic.Int32
	ended    atomic.Int32
}

func (f *leasePrechargeFixture) RenewBalancePrechargeLease(context.Context, string, string) (bool, error) {
	f.renewals.Add(1)
	return true, nil
}
func (f *leasePrechargeFixture) EndBalancePrechargeLease(context.Context, string, string) error {
	f.ended.Add(1)
	return nil
}
func (f *leasePrechargeFixture) RecoverBalancePrecharges(context.Context, int) (int, error) {
	return 0, nil
}

func TestBalancePrechargeLease_HeldThroughDetachedBillingAndStoppedAtFinish(t *testing.T) {
	f := &leasePrechargeFixture{prechargeWalletFixture: &prechargeWalletFixture{balance: .2, holds: make(map[string]float64)}}
	policy := &prechargePolicyFixture{policy: BalancePrechargeSettings{Enabled: true, Threshold: 1, Amount: .1}}
	ctx := newRequestBalancePrecharge(context.Background(), policy, f, nil)
	p := requestPrecharge(ctx)
	p.leaseInterval = 5 * time.Millisecond
	require.NoError(t, ensureRequestBalancePrecharge(ctx, &User{ID: 1}, &APIKey{ID: 1}))
	require.NotEmpty(t, p.command.LeaseOwner)
	done := retainBalancePrechargeTask(ctx)
	FinishBalancePrecharge(ctx)
	require.Eventually(t, func() bool { return f.renewals.Load() > 0 }, time.Second, 5*time.Millisecond)
	require.Zero(t, f.ended.Load())
	done()
	require.EqualValues(t, 1, f.ended.Load())
	require.Eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.leaseCancel == nil }, time.Second, 5*time.Millisecond)
}
