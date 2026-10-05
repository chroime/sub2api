package service

import "testing"

func TestTokenUsageAdjustmentAppliesInputAndOutputMultipliers(t *testing.T) {
	adjusted := TokenUsageAdjustment{InputMultiplier: 1.25, OutputMultiplier: 1.5}.Apply(101, 99)
	if adjusted.InputTokens != 126 {
		t.Fatalf("input tokens = %d, want 126", adjusted.InputTokens)
	}
	if adjusted.OutputTokens != 149 {
		t.Fatalf("output tokens = %d, want 149", adjusted.OutputTokens)
	}
}

func TestTokenUsageAdjustmentDefaultsInvalidMultipliersToOne(t *testing.T) {
	adjusted := TokenUsageAdjustment{InputMultiplier: 0, OutputMultiplier: -1}.Apply(17, 23)
	if adjusted.InputTokens != 17 || adjusted.OutputTokens != 23 {
		t.Fatalf("invalid multipliers changed usage: %+v", adjusted)
	}
}

func TestTokenUsageAdjustmentClampsNegativeUsage(t *testing.T) {
	adjusted := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2}.Apply(-1, -2)
	if adjusted.InputTokens != 0 || adjusted.OutputTokens != 0 {
		t.Fatalf("negative usage was not clamped: %+v", adjusted)
	}
}

func TestNormalizeBillingTokenMultiplierValidatesRange(t *testing.T) {
	if got, err := normalizeBillingTokenMultiplier(0); err != nil || got != 1 {
		t.Fatalf("zero default = (%v, %v), want (1, nil)", got, err)
	}
	if _, err := normalizeBillingTokenMultiplier(0.09); err == nil {
		t.Fatal("expected a lower-bound validation error")
	}
	if _, err := normalizeBillingTokenMultiplier(10.01); err == nil {
		t.Fatal("expected an upper-bound validation error")
	}
}

func TestTokenUsageAdjustmentForGroupUsesConfiguredMultipliers(t *testing.T) {
	group := &Group{BillingInputTokenMultiplier: 1.2, BillingOutputTokenMultiplier: 1.8, BillingTokenAdjustmentMinInputTokens: 100}
	got := tokenUsageAdjustmentForGroup(group).Apply(100, 50)
	if got.InputTokens != 120 || got.OutputTokens != 90 {
		t.Fatalf("group adjustment = %+v, want input=120 output=90", got)
	}
	got = tokenUsageAdjustmentForGroup(group).ApplyForRequest(99, 50)
	if got.InputTokens != 99 || got.OutputTokens != 50 {
		t.Fatalf("group short-request adjustment = %+v, want input=99 output=50", got)
	}
}

func TestTokenUsageAdjustmentSkipsShortRequests(t *testing.T) {
	adjustment := TokenUsageAdjustment{InputMultiplier: 1.5, OutputMultiplier: 2, MinInputTokens: 100}
	got := adjustment.ApplyForRequest(99, 40)
	if got.InputTokens != 99 || got.OutputTokens != 40 {
		t.Fatalf("short request was adjusted: %+v", got)
	}

	got = adjustment.ApplyForRequest(100, 40)
	if got.InputTokens != 150 || got.OutputTokens != 80 {
		t.Fatalf("threshold request was not adjusted: %+v", got)
	}
}

func TestNormalizeBillingTokenAdjustmentMinInputTokens(t *testing.T) {
	if got, err := normalizeBillingTokenAdjustmentMinInputTokens(-1); err == nil || got != 0 {
		t.Fatalf("negative threshold = (%v, %v), want an error", got, err)
	}
	if got, err := normalizeBillingTokenAdjustmentMinInputTokens(0); err != nil || got != 0 {
		t.Fatalf("zero threshold = (%v, %v), want (0, nil)", got, err)
	}
	if _, err := normalizeBillingTokenAdjustmentMinInputTokens(1_000_001); err == nil {
		t.Fatal("expected an upper-bound validation error")
	}
}
