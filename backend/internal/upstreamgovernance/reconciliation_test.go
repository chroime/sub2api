package upstreamgovernance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconciliationPlansRatesNamesAndLargeIncrease(t *testing.T) {
	now := time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC)
	site := Site{ID: 1, Version: 1, BaseURL: "https://up.example", IntervalMinutes: 5}
	binding := Binding{ID: 2, SiteID: 1, AccountID: 3, RemoteGroupID: "7", Platform: "openai", Marker: "owned"}
	account := ManagedLocalAccount{ID: 3, Identity: "identity", Name: "https://up.example--1", Rate: 1, Status: "active", Schedulable: true}
	state := newReconciliationState(binding, account)
	config := DefaultAutomationConfig()
	config.Policy.Enabled = true
	rate := 1.1
	snap := Snapshot{ID: 5, SiteID: 1, SiteVersion: 1, CreatedAt: now, Catalog: Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "7", Name: "renamed", ResolvedRateMultiplier: &rate}}}}
	row := planReconciliationRow(site, config, snap, binding, state, account, now)
	require.Equal(t, "update", row.Action)
	require.Equal(t, "ready", row.State)
	require.Equal(t, "https://up.example--1.1", *row.Patch.Name)
	require.InDelta(t, 1.1, *row.Patch.Rate, 0.00001)
	rate = 1.5
	row = planReconciliationRow(site, config, snap, binding, state, account, now)
	require.Equal(t, "review", row.State)
	require.Equal(t, "rate_increase_review", row.Reason)
	rate = 0
	row = planReconciliationRow(site, config, snap, binding, state, account, now)
	require.Equal(t, "ready", row.State)
	require.Zero(t, *row.Patch.Rate)
	rate = 1000000
	row = planReconciliationRow(site, config, snap, binding, state, account, now)
	require.Equal(t, "unavailable", row.State)
	require.Equal(t, "invalid_upstream_rate", row.Reason)
}

func TestReconciliationMissingRequiresDistinctCompleteSpacedObservations(t *testing.T) {
	now := time.Now().UTC()
	state := ReconciliationState{}
	snap := Snapshot{ID: 1, CreatedAt: now, Catalog: Catalog{GroupsComplete: true}}
	state = advanceReconciliationObservation(state, snap, false, 5)
	require.Equal(t, 1, state.MissingCount)
	state = advanceReconciliationObservation(state, snap, false, 5)
	require.Equal(t, 1, state.MissingCount)
	snap.ID++
	snap.CreatedAt = now.Add(time.Minute)
	state = advanceReconciliationObservation(state, snap, false, 5)
	require.Equal(t, 1, state.MissingCount)
	snap.ID++
	snap.CreatedAt = now.Add(5 * time.Minute)
	snap.Catalog.GroupsComplete = false
	state = advanceReconciliationObservation(state, snap, false, 5)
	require.Equal(t, 1, state.MissingCount)
	snap.ID++
	snap.Catalog.GroupsComplete = true
	state = advanceReconciliationObservation(state, snap, false, 5)
	require.Equal(t, 2, state.MissingCount)
	snap.ID++
	state = advanceReconciliationObservation(state, snap, true, 5)
	require.Zero(t, state.MissingCount)
}

func TestReconciliationKeepsCustomNamesAndRejectsOwnedEdits(t *testing.T) {
	site := Site{ID: 1, Version: 1, BaseURL: "https://up.example"}
	binding := Binding{ID: 2, SiteID: 1, AccountID: 3, RemoteGroupID: "7", Marker: "owned"}
	account := ManagedLocalAccount{ID: 3, Identity: "id", Name: "custom", Rate: 1, Status: "active", Schedulable: true}
	state := newReconciliationState(binding, account)
	config := DefaultAutomationConfig()
	config.Policy.Enabled = true
	rate := 1.1
	snap := Snapshot{ID: 1, SiteVersion: 1, CreatedAt: time.Now(), Catalog: Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "7", Name: "remote", ResolvedRateMultiplier: &rate}}}}
	row := planReconciliationRow(site, config, snap, binding, state, account, time.Now())
	require.Nil(t, row.Patch.Name)
	account.RateOwner = binding.Marker
	account.Rate = 0.7
	row = planReconciliationRow(site, config, snap, binding, state, account, time.Now())
	require.Equal(t, "conflict", row.State)
	require.Equal(t, "managed_rate_changed", row.Reason)
	account.Identity = "different-key"
	row = planReconciliationRow(site, config, snap, binding, state, account, time.Now())
	require.Equal(t, "conflict", row.State)
	require.Equal(t, "account_identity_changed", row.Reason)
}

func TestReconciliationNamePolicyWorksIndependentlyOfRatePolicy(t *testing.T) {
	now := time.Now()
	site := Site{Version: 1, BaseURL: "https://up.example", IntervalMinutes: 5}
	binding := Binding{ID: 2, AccountID: 3, RemoteGroupID: "7", Marker: "owned"}
	a := ManagedLocalAccount{ID: 3, Identity: "id", Name: "https://up.example--1", Rate: 1.2, Status: "active", Schedulable: true, NativeRateSync: true}
	state := newReconciliationState(binding, a)
	cfg := DefaultAutomationConfig()
	cfg.Policy.SyncRate = false
	rate := 9.0
	snapshot := Snapshot{ID: 1, SiteVersion: 1, CreatedAt: now, Catalog: Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "7", ResolvedRateMultiplier: &rate}}}}
	row := planReconciliationRow(site, cfg, snapshot, binding, state, a, now)
	require.Equal(t, "ready", row.State)
	require.Equal(t, "https://up.example--1.2", *row.Patch.Name)
	require.Nil(t, row.Patch.Rate)
}
