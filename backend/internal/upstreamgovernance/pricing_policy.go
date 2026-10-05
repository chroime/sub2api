package upstreamgovernance

import (
	"errors"
	"math"
	"sort"
	"time"
)

// PricingMode controls how a local group's sale multiplier is derived from a
// trusted upstream cost fact. Existing groups are not enabled implicitly.
type PricingMode string

const (
	PricingModeKeepMargin   PricingMode = "keep_margin"
	PricingModeTargetMargin PricingMode = "target_margin"
)

var (
	ErrPricingInvalidPolicy  = errors.New("invalid upstream pricing policy")
	ErrPricingUnknownCost    = errors.New("upstream pricing cost is unknown or incomparable")
	ErrPricingPolicyDisabled = errors.New("upstream pricing policy is disabled")
)

// PricingPolicy is the durable, local-group-owned pricing policy. BaselineCost
// and BaselineSale are frozen when the policy is enabled; subsequent
// calculations never use a rounded sale as the next baseline.
type PricingPolicy struct {
	ID                       int64       `json:"id,omitempty"`
	LocalGroupID             int64       `json:"local_group_id"`
	Enabled                  bool        `json:"enabled"`
	Mode                     PricingMode `json:"mode"`
	BaselineCost             float64     `json:"baseline_cost"`
	BaselineSale             float64     `json:"baseline_sale"`
	Ratio                    float64     `json:"ratio"`
	MinMargin                float64     `json:"min_margin"`
	SafetyBuffer             float64     `json:"safety_buffer"`
	DecreaseStabilitySeconds int64       `json:"decrease_stability_seconds"`
	MaxIncreasePercent       float64     `json:"max_increase_percent"`
	Version                  int64       `json:"version"`
	ManualOwner              bool        `json:"manual_owner"`
	ManualVersion            int64       `json:"manual_version,omitempty"`
	LastAutomaticSale        float64     `json:"last_automatic_sale,omitempty"`
	LastAutomaticCost        float64     `json:"last_automatic_cost,omitempty"`
	ActiveCost               float64     `json:"active_cost,omitempty"`
	ActiveCostSource         string      `json:"active_cost_source,omitempty"`
	Protected                bool        `json:"protected,omitempty"`
	ProtectionReason         string      `json:"protection_reason,omitempty"`
	CostFactRevision         int64       `json:"cost_fact_revision,omitempty"`
	DecreaseObservedAt       time.Time   `json:"decrease_observed_at,omitempty"`
}

// CostObservation is a cost fact for one source that can route to a local
// group. Unknown or incomparable eligible sources deliberately block automatic
// pricing instead of being treated as zero cost.
type CostObservation struct {
	SourceID     string    `json:"source_id"`
	LocalGroupID int64     `json:"local_group_id,omitempty"`
	SiteID       int64     `json:"site_id,omitempty"`
	BindingID    int64     `json:"binding_id,omitempty"`
	Cost         float64   `json:"cost"`
	Unit         string    `json:"unit,omitempty"`
	Currency     string    `json:"currency,omitempty"`
	Comparable   bool      `json:"comparable"`
	Eligible     bool      `json:"eligible"`
	Unknown      bool      `json:"unknown,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
}

// PricingDecision is an auditable pure-calculation result. Protected means a
// source should not continue scheduling under the old cost until an operator
// reviews the change; TargetSale is still returned for an explicit preview.
type PricingDecision struct {
	Cost        float64 `json:"cost"`
	SourceID    string  `json:"source_id,omitempty"`
	TargetSale  float64 `json:"target_sale"`
	Ratio       float64 `json:"ratio"`
	MarginFloor float64 `json:"margin_floor"`
	IncreasePct float64 `json:"increase_percent"`
	Protected   bool    `json:"protected"`
	Applied     bool    `json:"applied"`
	Reason      string  `json:"reason"`
}

func (p PricingPolicy) validate() error {
	if !p.Enabled {
		return ErrPricingPolicyDisabled
	}
	if p.Mode != PricingModeKeepMargin && p.Mode != PricingModeTargetMargin {
		return ErrPricingInvalidPolicy
	}
	if !finitePositive(p.BaselineCost) || !finitePositive(p.BaselineSale) {
		return ErrPricingInvalidPolicy
	}
	if !finiteRatio(p.MinMargin) || !finiteRatio(p.SafetyBuffer) || p.MinMargin+p.SafetyBuffer >= 1 {
		return ErrPricingInvalidPolicy
	}
	if p.DecreaseStabilitySeconds < 0 || math.IsNaN(p.MaxIncreasePercent) || math.IsInf(p.MaxIncreasePercent, 0) || p.MaxIncreasePercent < 0 {
		return ErrPricingInvalidPolicy
	}
	return nil
}

func finitePositive(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 }
func finiteRatio(v float64) bool    { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v < 1 }

// CalculatePricingTarget computes a sale multiplier from one trusted cost.
// Values are always rounded upward to four decimal places so rounding can
// never move the effective margin below the configured floor.
func CalculatePricingTarget(policy PricingPolicy, cost float64) (PricingDecision, error) {
	if err := policy.validate(); err != nil {
		return PricingDecision{}, err
	}
	if !finitePositive(cost) {
		return PricingDecision{}, ErrPricingUnknownCost
	}
	ratio := math.Round(policy.BaselineSale/policy.BaselineCost*1e8) / 1e8
	if !finitePositive(ratio) {
		return PricingDecision{}, ErrPricingInvalidPolicy
	}
	floor := cost / (1 - policy.MinMargin - policy.SafetyBuffer)
	target := floor
	if policy.Mode == PricingModeKeepMargin && cost*ratio > target {
		target = cost * ratio
	}
	target = ceil4(target)
	decision := PricingDecision{Cost: cost, TargetSale: target, Ratio: ratio, MarginFloor: floor, Reason: "price_ready"}
	previousCost := policy.LastAutomaticCost
	if previousCost <= 0 {
		previousCost = policy.BaselineCost
	}
	if previousCost > 0 && cost > previousCost {
		decision.IncreasePct = (cost/previousCost - 1) * 100
		if policy.MaxIncreasePercent > 0 && decision.IncreasePct > policy.MaxIncreasePercent+1e-9 {
			decision.Protected = true
			decision.Reason = "increase_review"
		}
	}
	return decision, nil
}

func ceil4(v float64) float64 {
	return math.Ceil((v-1e-12)*10000) / 10000
}

// AggregateComparableCost chooses the highest eligible, comparable cost. The
// highest source is deliberately conservative when a local group has a backup
// route. An eligible unknown source blocks the result.
func AggregateComparableCost(observations []CostObservation) (float64, string, error) {
	eligible := make([]CostObservation, 0, len(observations))
	for _, observation := range observations {
		if !observation.Eligible {
			continue
		}
		if observation.Unknown || !observation.Comparable || !finitePositive(observation.Cost) {
			return 0, "", ErrPricingUnknownCost
		}
		eligible = append(eligible, observation)
	}
	if len(eligible) == 0 {
		return 0, "", ErrPricingUnknownCost
	}
	sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].Cost > eligible[j].Cost })
	return eligible[0].Cost, eligible[0].SourceID, nil
}

// ShouldApplyPricing implements the asymmetric change policy: increases apply
// immediately, while decreases need a stable observation window. The caller
// must persist the cost fact even when this returns false.
func ShouldApplyPricing(policy PricingPolicy, previousCost, nextCost float64, changedAt, now time.Time) (bool, string) {
	if policy.ManualOwner {
		return false, "manual_owner"
	}
	if !finitePositive(previousCost) || !finitePositive(nextCost) {
		return false, "unknown_cost"
	}
	if math.Abs(previousCost-nextCost) <= 1e-12 {
		return false, "unchanged"
	}
	if nextCost > previousCost {
		return true, "increase"
	}
	if policy.DecreaseStabilitySeconds <= 0 || changedAt.IsZero() || !now.Before(changedAt.Add(time.Duration(policy.DecreaseStabilitySeconds)*time.Second)) {
		return true, "decrease"
	}
	return false, "decrease_stability"
}
