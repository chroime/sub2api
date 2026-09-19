package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestBalancePrechargeWaitsForRelease(t *testing.T) {
	first, policy, wallet, key := prechargeFixture(.1)
	require.NoError(t, ensureRequestBalancePrecharge(first, key.User, key))
	parent, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	second := newRequestBalancePrecharge(parent, policy, wallet, nil)
	result := make(chan error, 1)
	go func() { result <- ensureRequestBalancePrecharge(second, key.User, key) }()
	select {
	case err := <-result:
		t.Fatalf("frozen funds must cause waiting, not immediate completion: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	FinishBalancePrecharge(first)
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-parent.Done():
		t.Fatal("waiting request did not resume after release")
	}
	require.True(t, HasBalancePrecharge(second))
	FinishBalancePrecharge(second)
	wallet.mu.Lock()
	defer wallet.mu.Unlock()
	require.InDelta(t, .1, wallet.balance, 1e-8)
	require.Zero(t, wallet.frozen)
	require.Empty(t, wallet.holds)
}
