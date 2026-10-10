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

func TestTokenUsageAdjustmentCacheNeutralLogicalInput(t *testing.T) {
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 3, MinInputTokens: 100}
	raw := TokenUsageBuckets{
		InputTokens:      200,
		CacheReadTokens:  600,
		CacheWriteTokens: 200,
		OutputTokens:     50,
	}
	got := adjustment.ApplyToUsage(raw)

	if got.Raw != raw {
		t.Fatalf("raw usage changed: %+v, want %+v", got.Raw, raw)
	}
	if got.Customer.InputTokens != 2000 || got.Customer.OutputTokens != 150 {
		t.Fatalf("customer usage = %+v, want input=2000 output=150", got.Customer)
	}
	if got.Customer.CacheReadTokens != 0 || got.Customer.CacheWriteTokens != 0 {
		t.Fatalf("customer cache buckets were retained: %+v", got.Customer)
	}
	if !got.Applied || got.InputTokens != 2000 || got.OutputTokens != 150 {
		t.Fatalf("adjustment result = %+v, want applied customer aliases", got)
	}
}

func TestTokenUsageAdjustmentCacheSplitDoesNotChangeCustomerCharge(t *testing.T) {
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 3, MinInputTokens: 100}
	cacheMiss := adjustment.ApplyForRequestUsage(1000, 0, 0, 50)
	cacheHit := adjustment.ApplyForRequestUsage(200, 600, 200, 50)

	if cacheMiss.Customer != cacheHit.Customer {
		t.Fatalf("cache split changed customer usage: miss=%+v hit=%+v", cacheMiss.Customer, cacheHit.Customer)
	}
	if cacheMiss.Customer.InputTokens != 2000 || cacheMiss.Customer.OutputTokens != 150 {
		t.Fatalf("customer usage = %+v, want input=2000 output=150", cacheMiss.Customer)
	}
	if cacheHit.Raw.CacheReadTokens != 600 || cacheHit.Raw.CacheWriteTokens != 200 {
		t.Fatalf("raw cache buckets changed: %+v", cacheHit.Raw)
	}
}

func TestTokenUsageAdjustmentThresholdUsesNonCacheInput(t *testing.T) {
	adjustment := TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 3, MinInputTokens: 100}
	raw := TokenUsageBuckets{InputTokens: 99, CacheReadTokens: 1000, CacheWriteTokens: 100, OutputTokens: 40}
	got := adjustment.ApplyToUsage(raw)

	if got.Applied {
		t.Fatal("short non-cache request was adjusted because cache buckets exceeded threshold")
	}
	if got.Raw != got.Customer {
		t.Fatalf("short request changed customer buckets: raw=%+v customer=%+v", got.Raw, got.Customer)
	}
	if got.InputTokens != 99 || got.OutputTokens != 40 {
		t.Fatalf("short request aliases = input=%d output=%d, want 99/40", got.InputTokens, got.OutputTokens)
	}
}

func TestTokenUsageAdjustmentDefaultMultipliersPreserveCacheBuckets(t *testing.T) {
	adjustment := TokenUsageAdjustment{}
	raw := TokenUsageBuckets{InputTokens: 200, CacheReadTokens: 600, CacheWriteTokens: 200, OutputTokens: 50}
	got := adjustment.ApplyToUsage(raw)

	if got.Applied {
		t.Fatal("default multipliers unexpectedly enabled adjustment")
	}
	if got.Raw != raw || got.Customer != raw {
		t.Fatalf("default adjustment changed usage: raw=%+v customer=%+v", got.Raw, got.Customer)
	}
}

func TestTokenUsageAdjustmentClampsAllRawBuckets(t *testing.T) {
	got := (TokenUsageAdjustment{InputMultiplier: 2, OutputMultiplier: 2}).ApplyForRequestUsage(-1, -2, -3, -4)
	want := TokenUsageBuckets{}
	if got.Raw != want || got.Customer != want {
		t.Fatalf("negative buckets were not clamped: raw=%+v customer=%+v", got.Raw, got.Customer)
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
