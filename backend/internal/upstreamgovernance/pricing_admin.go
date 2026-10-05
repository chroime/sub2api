package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"time"
)

// PricingPolicyDraft is deliberately an allowlist: cost facts, baselines,
// ownership and coordinator state can never be provided by the client.
type PricingPolicyDraft struct {
	Enabled                  bool        `json:"enabled"`
	Mode                     PricingMode `json:"mode"`
	MinMargin                float64     `json:"min_margin"`
	SafetyBuffer             float64     `json:"safety_buffer"`
	DecreaseStabilitySeconds int64       `json:"decrease_stability_seconds"`
	MaxIncreasePercent       float64     `json:"max_increase_percent"`
}

type PricingBindingImpact struct {
	SiteID        int64  `json:"site_id"`
	SiteName      string `json:"site_name"`
	BindingID     int64  `json:"binding_id"`
	RemoteGroupID string `json:"remote_group_id"`
	AccountID     int64  `json:"account_id"`
}

type PricingPolicyPreview struct {
	Fingerprint     string                 `json:"fingerprint"`
	LocalGroupID    int64                  `json:"local_group_id"`
	CurrentSale     float64                `json:"current_sale"`
	CurrentCost     *float64               `json:"current_cost"`
	TargetSale      *float64               `json:"target_sale"`
	ProjectedMargin *float64               `json:"projected_margin"`
	Reason          string                 `json:"reason"`
	Protected       bool                   `json:"protected"`
	Blocked         bool                   `json:"blocked"`
	Sources         []CostObservation      `json:"sources"`
	Bindings        []PricingBindingImpact `json:"bindings"`
	Policy          PricingPolicyDraft     `json:"policy"`
}

type pricingAdminState struct {
	State    PricingState
	Bindings []PricingBindingImpact
}

func pricingDraftFromPolicy(p PricingPolicy) PricingPolicyDraft {
	return PricingPolicyDraft{p.Enabled, p.Mode, p.MinMargin, p.SafetyBuffer, p.DecreaseStabilitySeconds, p.MaxIncreasePercent}
}

func (d PricingPolicyDraft) validate() error {
	if d.Mode != PricingModeKeepMargin && d.Mode != PricingModeTargetMargin || !finiteRatio(d.MinMargin) || !finiteRatio(d.SafetyBuffer) || d.MinMargin+d.SafetyBuffer >= 1 || d.DecreaseStabilitySeconds < 0 || d.DecreaseStabilitySeconds > math.MaxInt64/int64(time.Second) || math.IsNaN(d.MaxIncreasePercent) || math.IsInf(d.MaxIncreasePercent, 0) || d.MaxIncreasePercent < 0 || d.MaxIncreasePercent >= 1e12 {
		return ErrInvalid
	}
	return nil
}

func preparePricingAdmin(siteID, groupID int64, draft PricingPolicyDraft, input pricingAdminState) (PricingPolicyPreview, PricingPolicy, error) {
	if siteID <= 0 || groupID <= 0 {
		return PricingPolicyPreview{}, PricingPolicy{}, ErrInvalid
	}
	if err := draft.validate(); err != nil {
		return PricingPolicyPreview{}, PricingPolicy{}, err
	}
	bound := false
	for _, binding := range input.Bindings {
		bound = bound || binding.SiteID == siteID
	}
	if !bound || input.State.Policy.LocalGroupID != groupID {
		return PricingPolicyPreview{}, PricingPolicy{}, ErrNotFound
	}
	preview := PricingPolicyPreview{LocalGroupID: groupID, CurrentSale: input.State.CurrentSale, Sources: append([]CostObservation{}, input.State.Observations...), Bindings: append([]PricingBindingImpact{}, input.Bindings...), Policy: draft}
	// Canonical semantic input excludes collection timestamps and observation /
	// coordinator version counters. Identical second-level refreshes must not
	// invalidate a human's preview. Every editable policy value is included.
	sort.Slice(preview.Sources, func(i, j int) bool { return preview.Sources[i].SourceID < preview.Sources[j].SourceID })
	sort.Slice(preview.Bindings, func(i, j int) bool { return preview.Bindings[i].BindingID < preview.Bindings[j].BindingID })
	facts := append([]CostObservation{}, preview.Sources...)
	for i := range facts {
		facts[i].ObservedAt = time.Time{}
	}
	policy := input.State.Policy
	semanticPolicy := policy
	semanticPolicy.ID, semanticPolicy.Version, semanticPolicy.CostFactRevision = 0, 0, 0
	semanticPolicy.DecreaseObservedAt = time.Time{}
	// ManualVersion and LastAutomaticSale are not decision inputs; a changed
	// ownership flag, live sale or last automatic cost still invalidates.
	semanticPolicy.ManualVersion, semanticPolicy.LastAutomaticSale = 0, 0
	raw, err := json.Marshal(struct {
		SiteID   int64
		Sale     float64
		Policy   PricingPolicy
		Draft    PricingPolicyDraft
		Facts    []CostObservation
		Bindings []PricingBindingImpact
	}{siteID, input.State.CurrentSale, semanticPolicy, draft, facts, preview.Bindings})
	if err != nil {
		return PricingPolicyPreview{}, PricingPolicy{}, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	preview.Fingerprint = hex.EncodeToString(sum[:])
	policy.Enabled, policy.Mode = draft.Enabled, draft.Mode
	policy.MinMargin, policy.SafetyBuffer = draft.MinMargin, draft.SafetyBuffer
	policy.DecreaseStabilitySeconds, policy.MaxIncreasePercent = draft.DecreaseStabilitySeconds, draft.MaxIncreasePercent
	cost, _, costErr := AggregateComparableCost(preview.Sources)
	if costErr == nil {
		preview.CurrentCost = &cost
	}
	if !draft.Enabled {
		preview.Reason = "policy_disabled"
		return preview, policy, nil
	}
	if costErr != nil || !finitePositive(preview.CurrentSale) {
		preview.Blocked, preview.Protected, preview.Reason = true, true, "unknown_cost"
		return preview, policy, nil
	}
	// Only a never-initialized baseline is established, from trusted state.
	// Editing or re-enabling an existing policy never resets its baseline.
	if !finitePositive(policy.BaselineCost) || !finitePositive(policy.BaselineSale) {
		policy.BaselineCost, policy.BaselineSale = cost, preview.CurrentSale
		policy.Ratio = preview.CurrentSale / cost
	}
	decision, err := CalculatePricingTarget(policy, cost)
	if err != nil || !finitePositive(decision.TargetSale) || decision.TargetSale >= 1e12 {
		preview.Blocked, preview.Protected, preview.Reason = true, true, "invalid_policy"
		return preview, policy, nil
	}
	if policy.LastAutomaticCost <= 0 && policy.ActiveCost <= 0 {
		decision.Protected, decision.Reason = false, "price_ready"
	}
	preview.TargetSale = &decision.TargetSale
	margin := (decision.TargetSale - cost) / decision.TargetSale
	preview.ProjectedMargin = &margin
	preview.Protected, preview.Reason = decision.Protected, decision.Reason
	if policy.ManualOwner {
		preview.Protected, preview.Reason = true, "manual_owner"
	}
	return preview, policy, nil
}

type PricingAdminStore interface {
	PreviewPricingPolicy(context.Context, int64, int64, PricingPolicyDraft) (PricingPolicyPreview, error)
	SavePricingPolicy(context.Context, int64, int64, PricingPolicyDraft, string) (PricingPoliciesConfiguration, error)
	SavePricingNotifications(context.Context, int64, int64, PricingNotificationPolicy) (PricingPoliciesConfiguration, error)
}

func (s *Service) PreviewPricingPolicy(ctx context.Context, siteID, groupID int64, draft PricingPolicyDraft) (PricingPolicyPreview, error) {
	store, ok := s.store.(PricingAdminStore)
	if !ok {
		return PricingPolicyPreview{}, ErrUnsupported
	}
	return store.PreviewPricingPolicy(ctx, siteID, groupID, draft)
}

func (s *Service) ConfigurePricingPolicy(ctx context.Context, siteID, groupID int64, draft PricingPolicyDraft, fingerprint string) (PricingPoliciesConfiguration, error) {
	store, ok := s.store.(PricingAdminStore)
	if !ok {
		return PricingPoliciesConfiguration{}, ErrUnsupported
	}
	return store.SavePricingPolicy(ctx, siteID, groupID, draft, fingerprint)
}

func (s *Service) ConfigurePricingNotifications(ctx context.Context, siteID, version int64, notifications PricingNotificationPolicy) (PricingPoliciesConfiguration, error) {
	store, ok := s.store.(PricingAdminStore)
	if !ok {
		return PricingPoliciesConfiguration{}, ErrUnsupported
	}
	return store.SavePricingNotifications(ctx, siteID, version, notifications)
}
