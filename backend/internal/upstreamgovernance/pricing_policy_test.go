package upstreamgovernance

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestCalculatePricingTargetKeepsBaselineMarginAndRoundsUp(t *testing.T) {
	policy := PricingPolicy{
		Enabled:                  true,
		Mode:                     PricingModeKeepMargin,
		BaselineCost:             0.20,
		BaselineSale:             0.30,
		MinMargin:                0.20,
		SafetyBuffer:             0.05,
		DecreaseStabilitySeconds: 60,
	}

	decision, err := CalculatePricingTarget(policy, 0.22101)
	if err != nil {
		t.Fatalf("CalculatePricingTarget() error = %v", err)
	}
	if got, want := decision.TargetSale, 0.3316; got != want {
		t.Fatalf("target sale = %.8f, want %.8f", got, want)
	}
	if got, want := decision.Ratio, 1.5; got != want {
		t.Fatalf("ratio = %.8f, want %.8f", got, want)
	}
	if decision.Protected {
		t.Fatal("ordinary increase must not be protected")
	}
}

func TestCalculatePricingTargetTargetMarginMode(t *testing.T) {
	policy := PricingPolicy{Enabled: true, Mode: PricingModeTargetMargin, BaselineCost: 1, BaselineSale: 2, MinMargin: 0.3, SafetyBuffer: 0.1}
	decision, err := CalculatePricingTarget(policy, 2)
	if err != nil {
		t.Fatalf("CalculatePricingTarget() error = %v", err)
	}
	// 2 / (1 - .3 - .1) = 3.333..., always round upward.
	if got, want := decision.TargetSale, 3.3334; got != want {
		t.Fatalf("target sale = %.8f, want %.8f", got, want)
	}
}

func TestAggregateComparableCostUsesHighestEligibleSource(t *testing.T) {
	now := time.Now()
	cost, source, err := AggregateComparableCost([]CostObservation{
		{SourceID: "cheap", Cost: 0.2, Comparable: true, Eligible: true, ObservedAt: now},
		{SourceID: "expensive", Cost: 0.4, Comparable: true, Eligible: true, ObservedAt: now},
		{SourceID: "offline", Cost: 0.9, Comparable: true, Eligible: false, ObservedAt: now},
	})
	if err != nil {
		t.Fatalf("AggregateComparableCost() error = %v", err)
	}
	if cost != 0.4 || source != "expensive" {
		t.Fatalf("aggregate = (%v, %q), want (0.4, expensive)", cost, source)
	}
}

func TestAggregateComparableCostProtectsUnknownEligibleSource(t *testing.T) {
	_, _, err := AggregateComparableCost([]CostObservation{
		{SourceID: "known", Cost: 0.2, Comparable: true, Eligible: true},
		{SourceID: "unknown", Eligible: true, Comparable: false, Unknown: true},
	})
	if !errors.Is(err, ErrPricingUnknownCost) {
		t.Fatalf("error = %v, want ErrPricingUnknownCost", err)
	}
}

func TestCalculatePricingTargetRejectsZeroBaselineAndInvalidValues(t *testing.T) {
	cases := []PricingPolicy{
		{Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: 0, BaselineSale: 1},
		{Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: math.NaN(), BaselineSale: 1},
		{Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: 1, BaselineSale: 1, MinMargin: 0.8, SafetyBuffer: 0.2},
	}
	for i, policy := range cases {
		if _, err := CalculatePricingTarget(policy, 1); !errors.Is(err, ErrPricingInvalidPolicy) {
			t.Errorf("case %d error = %v, want ErrPricingInvalidPolicy", i, err)
		}
	}
}

func TestPricingApplicationDecisionHonorsManualOwnershipAndDecreaseStability(t *testing.T) {
	base := PricingPolicy{Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: 1, BaselineSale: 1.5, DecreaseStabilitySeconds: 60}
	if apply, reason := ShouldApplyPricing(base, 1, 1.2, time.Time{}, time.Now()); !apply || reason != "increase" {
		t.Fatalf("increase decision = (%v, %q)", apply, reason)
	}
	changed := time.Now()
	if apply, reason := ShouldApplyPricing(base, 1.2, 1, changed, changed.Add(59*time.Second)); apply || reason != "decrease_stability" {
		t.Fatalf("early decrease decision = (%v, %q)", apply, reason)
	}
	if apply, reason := ShouldApplyPricing(base, 1.2, 1, changed, changed.Add(60*time.Second)); !apply || reason != "decrease" {
		t.Fatalf("stable decrease decision = (%v, %q)", apply, reason)
	}
	base.ManualOwner = true
	if apply, reason := ShouldApplyPricing(base, 1, 2, time.Time{}, time.Now()); apply || reason != "manual_owner" {
		t.Fatalf("manual decision = (%v, %q)", apply, reason)
	}
}

func TestCalculatePricingTargetFlagsLargeIncreaseForProtection(t *testing.T) {
	policy := PricingPolicy{Enabled: true, Mode: PricingModeKeepMargin, BaselineCost: 1, BaselineSale: 1.2, MaxIncreasePercent: 10}
	decision, err := CalculatePricingTarget(policy, 2)
	if err != nil {
		t.Fatalf("CalculatePricingTarget() error = %v", err)
	}
	if !decision.Protected || decision.Reason != "increase_review" {
		t.Fatalf("decision = %#v, want protected increase_review", decision)
	}
	if decision.TargetSale <= 0 {
		t.Fatal("protected decision must still expose the computed target")
	}
}
