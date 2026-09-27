package upstreamgovernance

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfiguredIntervalsAcceptPositiveWholeMinutes(t *testing.T) {
	for _, minutes := range []int{1, 2, 1441, 10081, 43200, maxIntervalMinutes} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			svc, _, _, _ := setupEngine(t)
			site, err := svc.CreateSite(t.Context(), Site{Name: "Interval fixture", Platform: "sub2api", BaseURL: "https://fixture.example", IntervalMinutes: minutes})
			require.NoError(t, err)
			require.Equal(t, minutes, site.IntervalMinutes)
			site, err = svc.UpdateSite(t.Context(), site.ID, *site)
			require.NoError(t, err)
			require.Equal(t, minutes, site.IntervalMinutes)

			probeSvc, probeStore, connector, _ := importedEngine(t)
			binding, err := probeSvc.ConfigureMonitor(t.Context(), 1, probeStore.bindings[0].ID, true, "gpt-fixture", minutes)
			require.NoError(t, err)
			require.Equal(t, minutes, binding.ProbeIntervalMinutes)
			require.Zero(t, connector.probeCalls, "changing the interval must not issue a paid probe")

			balanceSvc, balanceStore, _, notifier, _ := balanceEngine(t)
			policy := balanceStore.site.BalanceMonitor
			policy.CooldownMinutes = minutes
			balanceSite, err := balanceSvc.ConfigureBalanceMonitor(t.Context(), 1, 1, policy)
			require.NoError(t, err)
			require.Equal(t, minutes, balanceSite.BalanceMonitor.CooldownMinutes)
			require.Empty(t, notifier.sent, "saving a notification interval must not send mail")

			rechargeSvc, _, _, _, _ := rechargeEngine(t)
			recharge := defaultRechargePolicy("sub2api")
			recharge.CooldownMinutes = minutes
			plan, err := rechargeSvc.ConfigureRechargePlan(t.Context(), 1, 0, recharge)
			require.NoError(t, err)
			require.Equal(t, minutes, plan.Policy.CooldownMinutes)
			require.Nil(t, plan.Evaluation)
		})
	}
}

func TestConfiguredIntervalsRejectNegativeAndUnrepresentableMinutes(t *testing.T) {
	tooLarge := int64(maxIntervalMinutes) + 1
	for _, minutes := range []int{-1, int(tooLarge)} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			svc, _, _, _ := setupEngine(t)
			_, err := svc.CreateSite(t.Context(), Site{Name: "Interval fixture", Platform: "sub2api", BaseURL: "https://fixture.example", IntervalMinutes: minutes})
			require.ErrorIs(t, err, ErrInvalid)
			_, err = svc.ConfigureMonitor(t.Context(), 1, 1, true, "gpt-fixture", minutes)
			require.ErrorIs(t, err, ErrInvalid)
			balance := defaultBalanceMonitor("sub2api")
			balance.CooldownMinutes = minutes
			_, err = svc.ConfigureBalanceMonitor(t.Context(), 1, 1, balance)
			require.ErrorIs(t, err, ErrInvalid)
			recharge := defaultRechargePolicy("sub2api")
			recharge.CooldownMinutes = minutes
			_, err = normalizeRechargePolicy(recharge, "sub2api")
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	// Existing callers may omit collection/probe intervals. Their established
	// defaults stay compatible, while explicit notification policies require >0.
	svc, _, _, _ := setupEngine(t)
	site, err := svc.CreateSite(t.Context(), Site{Name: "Default fixture", Platform: "sub2api", BaseURL: "https://fixture.example"})
	require.NoError(t, err)
	require.Equal(t, 15, site.IntervalMinutes)
	policy := defaultRechargePolicy("sub2api")
	policy.CooldownMinutes = 0
	_, err = normalizeRechargePolicy(policy, "sub2api")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestDisabledProbeSavesConfiguredIntervalWithoutChangingProbeState(t *testing.T) {
	for _, minutes := range []int{1, 43200} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			svc, store, connector, _ := importedEngine(t)
			store.bindings[0].ProbeModel = "saved-model"
			originalDue := store.bindings[0].NextProbeAt
			_, err := svc.ConfigureMonitor(t.Context(), 1, store.bindings[0].ID, false, "ignored-model", minutes)
			require.NoError(t, err)
			bindings, err := svc.Bindings(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, minutes, bindings[0].ProbeIntervalMinutes)
			require.False(t, bindings[0].ProbeEnabled)
			require.Equal(t, "saved-model", bindings[0].ProbeModel)
			require.Equal(t, originalDue, bindings[0].NextProbeAt)
			require.Zero(t, connector.probeCalls)
		})
	}
}

func TestAddMinutesPreservesElapsedTimeAcrossDSTAndLargeIntervals(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	for _, start := range []time.Time{
		time.Date(2026, 3, 7, 12, 15, 4, 123456789, location),
		time.Date(2026, 10, 31, 12, 15, 4, 123456789, location),
	} {
		for _, minutes := range []int64{1, 1440, 43200, maxIntervalMinutes, 2 * int64(maxIntervalMinutes)} {
			got := addMinutes(start, minutes)
			require.Equal(t, start.Unix()+minutes*60, got.Unix())
			require.Equal(t, start.Nanosecond(), got.Nanosecond())
			require.Same(t, location, got.Location())
		}
	}
}

func TestWorkerHonorsConfiguredCollectionAndProbeIntervals(t *testing.T) {
	for _, minutes := range []int{1, 2, 1441, 10081, 43200, maxIntervalMinutes} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			svc, store, connector, _ := importedEngine(t)
			start := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
			now := start
			svc.now = func() time.Time { return now }
			store.site.IntervalMinutes = minutes
			store.site.NextSyncAt = now
			_, err := svc.ConfigureMonitor(t.Context(), 1, store.bindings[0].ID, true, "gpt-fixture", minutes)
			require.NoError(t, err)
			before := connector.discoveryCalls
			require.NoError(t, svc.runDue(t.Context()))
			deadline := time.Unix(start.Unix()+int64(minutes)*60, 0)
			require.True(t, store.site.NextSyncAt.Equal(deadline))
			require.True(t, store.bindings[0].NextProbeAt.Equal(deadline))
			_, err = json.Marshal(store.site)
			require.NoError(t, err, "the next-run time must remain JSON serializable")
			_, err = json.Marshal(store.bindings[0])
			require.NoError(t, err)
			require.Equal(t, before+1, connector.discoveryCalls)
			require.Equal(t, 1, connector.probeCalls)
			now = deadline.Add(-time.Second)
			require.NoError(t, svc.runDue(t.Context()))
			require.Equal(t, before+1, connector.discoveryCalls)
			require.Equal(t, 1, connector.probeCalls)
			now = deadline
			require.NoError(t, svc.runDue(t.Context()))
			require.Equal(t, before+2, connector.discoveryCalls)
			require.Equal(t, 2, connector.probeCalls)
		})
	}
}

func TestBalanceReminderUsesConfiguredIntervalWithoutDurationOverflow(t *testing.T) {
	for _, minutes := range []int{1, 2, 10081, 43200, maxIntervalMinutes} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			svc, store, _, notifier, now := balanceEngine(t)
			store.site.BalanceMonitor.CooldownMinutes = minutes
			start := *now
			_, err := svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			require.Len(t, notifier.sent["admin@example.com"], 1)
			deadline := time.Unix(start.Unix()+int64(minutes)*60, 0)
			delivery := store.site.balanceState.Recipients[balanceRecipientHash("admin@example.com")]
			require.True(t, delivery.NextAttemptAt.Equal(deadline))
			*now = deadline.Add(-time.Second)
			_, err = svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			require.Len(t, notifier.sent["admin@example.com"], 1)
			if minutes <= 43200 {
				*now = deadline
				_, err = svc.Sync(t.Context(), 1)
				require.NoError(t, err)
				require.Len(t, notifier.sent["admin@example.com"], 2)
			}
		})
	}
}

func TestLargeCollectionIntervalsKeepBalanceAndCatalogFreshUntilDeadline(t *testing.T) {
	svc, store, connector, _, now := balanceEngine(t)
	store.site.IntervalMinutes = maxIntervalMinutes
	store.site.BalanceMonitor.Enabled = false
	connector.catalog.GroupsComplete = true
	snapshot, err := svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	deadline := time.Unix(snapshot.CreatedAt.Unix()+2*int64(maxIntervalMinutes)*60, 0)
	require.Greater(t, deadline.Year(), 9999, "the doubled deadline is internal comparison state only")
	*now = deadline.Add(-time.Second)
	require.False(t, svc.observeBalance(store.site, snapshot).stale)
	require.True(t, reconciliationSnapshotFresh(store.site, *snapshot, *now))
	*now = deadline.Add(time.Second)
	require.True(t, svc.observeBalance(store.site, snapshot).stale)
	require.False(t, reconciliationSnapshotFresh(store.site, *snapshot, *now))
}

func TestReconciliationMissingConfirmationsFollowConfiguredInterval(t *testing.T) {
	for _, minutes := range []int{1, 2, 43200, maxIntervalMinutes} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			svc, store, connector, local, now := reconciliationFixture(t)
			store.site.IntervalMinutes = minutes
			store.config.Policy.Enabled = true
			_, err := svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			connector.catalog.Groups = nil
			_, err = svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, store.states[2].MissingCount)
			deadline := time.Unix(now.Unix()+int64(minutes)*60, int64(now.Nanosecond()))
			*now = deadline.Add(-time.Second)
			_, err = svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, store.states[2].MissingCount)
			require.True(t, local.account.Schedulable)
			*now = deadline
			_, err = svc.Sync(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, 2, store.states[2].MissingCount)
			require.False(t, local.account.Schedulable)
		})
	}
}
