package upstreamgovernance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthNotifier struct{ *balanceTestNotifier }

func (n healthNotifier) Readiness(context.Context, []string) BalanceDeliveryReadiness {
	return BalanceDeliveryReadiness{Ready: false, RecipientCount: 1, Reason: "smtp_not_configured"}
}

func TestBalanceHealthUsesSuccessfulSnapshotAndNeverSends(t *testing.T) {
	svc, store, connector, notifier, now := balanceEngine(t)
	store.site.BalanceMonitor.Enabled = false
	_, err := svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	store.site.BalanceMonitor.Enabled = true
	svc.SetBalanceNotifier(healthNotifier{notifier})
	observed := store.snap.CreatedAt
	*now = now.Add(5 * time.Minute)
	connector.err = ErrReauth
	_, err = svc.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	before := store.saveCalls
	health, err := svc.BalanceHealth(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, &observed, health.ObservedAt)
	require.Equal(t, now, health.LastAttemptAt)
	require.Equal(t, "unknown", health.State)
	require.Equal(t, "reauth_required", health.Reason)
	require.False(t, health.DeliveryReady)
	require.Equal(t, "smtp_not_configured", health.DeliveryReason)
	require.Equal(t, 1, health.RecipientCount)
	require.Empty(t, notifier.sent)
	require.Equal(t, before, store.saveCalls, "health GET must not persist state")
}

func TestBalanceHealthFreshnessAndVersionBoundaries(t *testing.T) {
	svc, store, _, _, now := balanceEngine(t)
	store.site.BalanceMonitor.Enabled = false
	_, err := svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	store.site.BalanceMonitor.Enabled = true
	store.site.IntervalMinutes = 5
	*now = now.Add(10 * time.Minute)
	health, err := svc.BalanceHealth(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, health.Stale)
	require.Equal(t, "balance_stale", health.Reason)
	require.Equal(t, "unknown", health.State)
	*now = store.snap.CreatedAt
	store.site.Version++
	health, err = svc.BalanceHealth(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, health.Stale)
	require.Equal(t, "snapshot_outdated", health.Reason)
}
