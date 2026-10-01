package upstreamgovernance

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func pricingAdminFixture() (PricingPolicyDraft, pricingAdminState) {
	return PricingPolicyDraft{Enabled: true, Mode: PricingModeTargetMargin, MinMargin: .25, SafetyBuffer: .1, DecreaseStabilitySeconds: 60, MaxIncreasePercent: 20}, pricingAdminState{
		State:    PricingState{CurrentSale: .3, Policy: PricingPolicy{LocalGroupID: 12, Mode: PricingModeKeepMargin, BaselineCost: .22, BaselineSale: .3, Version: 7}, Observations: []CostObservation{{SourceID: "site:1:group:r:openai", LocalGroupID: 12, SiteID: 1, BindingID: 3, Cost: .22, Unit: "multiplier", Currency: "relative", Comparable: true, Eligible: true, ObservedAt: time.Unix(1, 0)}}},
		Bindings: []PricingBindingImpact{{SiteID: 1, SiteName: "Fixture", BindingID: 3, RemoteGroupID: "r", AccountID: 4}},
	}
}

func TestPricingAdminPreviewUsesDraftAndTrustedFacts(t *testing.T) {
	draft, state := pricingAdminFixture()
	before := state.State.Policy
	preview, policy, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	require.Equal(t, .3385, *preview.TargetSale)
	require.Equal(t, .22, *preview.CurrentCost)
	require.InDelta(t, .3500738552, *preview.ProjectedMargin, 1e-9)
	require.False(t, preview.Blocked)
	require.Equal(t, before, state.State.Policy, "preview must not mutate stored state")
	require.Equal(t, before.BaselineCost, policy.BaselineCost)
	draft.MinMargin = .4
	changed, _, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	require.Equal(t, .44, *changed.TargetSale)
	require.NotEqual(t, preview.Fingerprint, changed.Fingerprint)
}

func TestPricingAdminFingerprintIgnoresObservationRefreshButNotSemanticChanges(t *testing.T) {
	draft, state := pricingAdminFixture()
	preview, _, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	state.State.Observations[0].ObservedAt = time.Unix(1000, 0)
	state.State.Policy.Version++
	state.State.Policy.CostFactRevision++
	refreshed, _, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	require.Equal(t, preview.Fingerprint, refreshed.Fingerprint)
	for name, change := range map[string]func(*pricingAdminState){
		"sale":      func(s *pricingAdminState) { s.State.CurrentSale = .4 },
		"cost":      func(s *pricingAdminState) { s.State.Observations[0].Cost = .25 },
		"policy":    func(s *pricingAdminState) { s.State.Policy.MinMargin = .1 },
		"baseline":  func(s *pricingAdminState) { s.State.Policy.BaselineSale = .4 },
		"ownership": func(s *pricingAdminState) { s.State.Policy.ManualOwner = true },
		"binding":   func(s *pricingAdminState) { s.Bindings[0].AccountID = 9 },
	} {
		t.Run(name, func(t *testing.T) {
			_, next := pricingAdminFixture()
			change(&next)
			v, _, err := preparePricingAdmin(1, 12, draft, next)
			require.NoError(t, err)
			require.NotEqual(t, preview.Fingerprint, v.Fingerprint)
		})
	}
}

func TestPricingAdminPreviewBlocksUnknownCostAndPreservesOwnership(t *testing.T) {
	draft, state := pricingAdminFixture()
	state.State.Observations[0].Unknown = true
	preview, _, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	require.True(t, preview.Blocked)
	require.Equal(t, "unknown_cost", preview.Reason)
	require.Nil(t, preview.CurrentCost)
	require.Nil(t, preview.TargetSale)
	require.Nil(t, preview.ProjectedMargin)
	draft.Enabled = false
	preview, _, err = preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	require.False(t, preview.Blocked, "unknown cost must not prevent disabling automation")
	require.Equal(t, "policy_disabled", preview.Reason)
	draft, state = pricingAdminFixture()
	state.State.Policy.ManualOwner = true
	preview, policy, err := preparePricingAdmin(1, 12, draft, state)
	require.NoError(t, err)
	require.True(t, policy.ManualOwner)
	require.True(t, preview.Protected)
	require.Equal(t, "manual_owner", preview.Reason)
}

func TestPricingAdminDraftRejectsNonFiniteAndOutOfRangeInputs(t *testing.T) {
	for name, change := range map[string]func(*PricingPolicyDraft){
		"nan":                func(d *PricingPolicyDraft) { d.MinMargin = math.NaN() },
		"infinity":           func(d *PricingPolicyDraft) { d.SafetyBuffer = math.Inf(1) },
		"increase_nan":       func(d *PricingPolicyDraft) { d.MaxIncreasePercent = math.NaN() },
		"negative":           func(d *PricingPolicyDraft) { d.MinMargin = -.1 },
		"total_one":          func(d *PricingPolicyDraft) { d.MinMargin = .9 },
		"stability_negative": func(d *PricingPolicyDraft) { d.DecreaseStabilitySeconds = -1 },
		"stability_overflow": func(d *PricingPolicyDraft) { d.DecreaseStabilitySeconds = math.MaxInt64 },
		"mode":               func(d *PricingPolicyDraft) { d.Mode = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			draft, state := pricingAdminFixture()
			change(&draft)
			_, _, err := preparePricingAdmin(1, 12, draft, state)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestPricingAdminRejectsUnboundGroup(t *testing.T) {
	draft, state := pricingAdminFixture()
	_, _, err := preparePricingAdmin(2, 12, draft, state)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestPricingAdminEmptySourcesAreAnArrayInPolicyResponse(t *testing.T) {
	view := pricingViewFromState(12, "Empty fixture", PricingState{Policy: PricingPolicy{LocalGroupID: 12}, CurrentSale: .3})
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"sources":[]`)
	require.Nil(t, view.CurrentCost)
	require.Nil(t, view.TargetSale)
	require.Equal(t, "disabled", view.Status)
}
