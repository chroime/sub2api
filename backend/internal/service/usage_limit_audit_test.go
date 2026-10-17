package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func usageAuditFixture(t *testing.T) (*PricingService, *UsageLog) {
	t.Helper()
	pricing := &PricingService{}
	data, err := pricing.parsePricingData([]byte(`{
		"known-model": {"input_cost_per_token": 0.000001, "max_input_tokens": 250, "max_output_tokens": 100, "max_tokens": 100},
		"gpt-6-astra": {"input_cost_per_token": 0.000001, "max_input_tokens": 300, "max_output_tokens": 120},
		"claude-opus-4-6": {"input_cost_per_token": 0.000001, "max_input_tokens": 400, "max_output_tokens": 130}
	}`))
	require.NoError(t, err)
	pricing.pricingData = data
	account := &Account{}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{
			"known-model": {ID: "known-model", ContextWindow: 200, MaxContextWindow: 300, MaxOutputTokens: 100},
		},
	})
	return pricing, &UsageLog{
		Model: "known-model", Account: account,
		UpstreamInputTokens: 100, UpstreamOutputTokens: 80,
		CacheReadTokens: 60, CacheCreationTokens: 40,
		CacheCreation5mTokens: 15, CacheCreation1hTokens: 25,
	}
}

func TestAuditUsageLimitsUsesPersistedUpstreamCacheNotBilledCache(t *testing.T) {
	log := &UsageLog{
		Model:               "gpt-5.5",
		UpstreamInputTokens: 100, UpstreamOutputTokens: 20,
		CacheReadTokens: 1800, CacheCreationTokens: 200,
		UpstreamCacheReadTokens: 900, UpstreamCacheCreationTokens: 100,
	}
	before := *log
	audit := AuditUsageLimits(log)
	require.Equal(t, before, *log)
	require.EqualValues(t, 1100, *audit.TotalInputTokens)
	require.EqualValues(t, 1120, *audit.TotalContextTokens)
}

func TestAuditUsageLimitsUsesRawBucketsWithoutChangingUsage(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	log.InputTokens, log.OutputTokens = 900000, 800000
	log.ImageInputTokens, log.ImageOutputTokens = 20, 10 // Already included in raw totals.
	log.ReasoningEffort = usageAuditString("high")       // Reasoning is already in raw output.
	before, err := json.Marshal(log)
	require.NoError(t, err)

	audit := pricing.AuditUsageLimits(log)

	require.Equal(t, "valid", audit.Status)
	require.Equal(t, "known-model", audit.Model)
	require.EqualValues(t, 250, *audit.InputLimit)
	require.EqualValues(t, 100, *audit.OutputLimit)
	require.EqualValues(t, 300, *audit.ContextLimit)
	require.EqualValues(t, 200, *audit.TotalInputTokens)
	require.EqualValues(t, 80, *audit.TotalOutputTokens)
	require.EqualValues(t, 280, *audit.TotalContextTokens)
	require.Empty(t, audit.Issues)
	after, err := json.Marshal(log)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

func TestAuditUsageLimitsChecksEachBudgetIncludingCache(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  int
		output int
		read   int
		write  int
		issues []string
	}{
		{name: "exact boundaries", input: 100, read: 100, write: 50, output: 50},
		{name: "cache reads consume input", input: 100, read: 151, issues: []string{"input_limit_exceeded"}},
		{name: "cache writes consume input", input: 100, write: 151, issues: []string{"input_limit_exceeded"}},
		{name: "output ceiling", input: 100, output: 101, issues: []string{"output_limit_exceeded"}},
		{name: "combined context", input: 150, read: 100, output: 51, issues: []string{"context_limit_exceeded"}},
		{name: "all ceilings", input: 251, output: 101, issues: []string{"input_limit_exceeded", "output_limit_exceeded", "context_limit_exceeded"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing, log := usageAuditFixture(t)
			log.UpstreamInputTokens, log.UpstreamOutputTokens = tc.input, tc.output
			log.CacheReadTokens, log.CacheCreationTokens = tc.read, tc.write
			log.CacheCreation5mTokens, log.CacheCreation1hTokens = 0, 0
			audit := pricing.AuditUsageLimits(log)
			status := "valid"
			if len(tc.issues) > 0 {
				status = "invalid"
			}
			require.Equal(t, status, audit.Status)
			require.ElementsMatch(t, tc.issues, audit.Issues)
		})
	}
}

func TestAuditUsageLimitsCacheWriteBucketsAreNotAddedTwice(t *testing.T) {
	for _, tc := range []struct {
		name      string
		aggregate int
		five      int
		hour      int
		wantInput int64
		mismatch  bool
	}{
		{name: "aggregate only", aggregate: 40, wantInput: 200},
		{name: "details only", five: 15, hour: 25, wantInput: 200},
		{name: "matching details", aggregate: 40, five: 15, hour: 25, wantInput: 200},
		{name: "details larger", aggregate: 40, five: 25, hour: 25, wantInput: 200, mismatch: true},
		{name: "details smaller", aggregate: 40, five: 15, hour: 15, wantInput: 200, mismatch: true},
		{name: "no cache writes", wantInput: 160},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing, log := usageAuditFixture(t)
			log.CacheCreationTokens, log.CacheCreation5mTokens, log.CacheCreation1hTokens = tc.aggregate, tc.five, tc.hour
			audit := pricing.AuditUsageLimits(log)
			require.Equal(t, tc.wantInput, *audit.TotalInputTokens)
			if tc.mismatch {
				require.Equal(t, "invalid", audit.Status)
				require.Contains(t, audit.Issues, "cache_creation_total_mismatch")
			} else {
				require.Equal(t, "valid", audit.Status)
				require.Empty(t, audit.Issues)
			}
			require.Equal(t, tc.aggregate, log.CacheCreationTokens)
			require.Equal(t, tc.five, log.CacheCreation5mTokens)
			require.Equal(t, tc.hour, log.CacheCreation1hTokens)
		})
	}
}

func TestAuditUsageLimitsRejectsNegativeRawCounts(t *testing.T) {
	for _, tc := range []struct {
		code string
		set  func(*UsageLog)
	}{
		{"negative_upstream_input_tokens", func(l *UsageLog) { l.UpstreamInputTokens = -1 }},
		{"negative_upstream_output_tokens", func(l *UsageLog) { l.UpstreamOutputTokens = -1 }},
		{"negative_cache_read_tokens", func(l *UsageLog) { l.CacheReadTokens = -1 }},
		{"negative_cache_creation_tokens", func(l *UsageLog) { l.CacheCreationTokens = -1 }},
		{"negative_cache_creation_5m_tokens", func(l *UsageLog) { l.CacheCreation5mTokens = -1 }},
		{"negative_cache_creation_1h_tokens", func(l *UsageLog) { l.CacheCreation1hTokens = -1 }},
	} {
		t.Run(tc.code, func(t *testing.T) {
			pricing, log := usageAuditFixture(t)
			tc.set(log)
			audit := pricing.AuditUsageLimits(log)
			require.Equal(t, "invalid", audit.Status)
			require.Contains(t, audit.Issues, tc.code)
		})
	}
}

func TestAuditUsageLimitsDetectsIntegerOverflow(t *testing.T) {
	if int64(maxIntValue()) < math.MaxInt64 {
		t.Skip("int64 overflow inputs require a 64-bit int")
	}
	for _, tc := range []struct {
		code string
		log  UsageLog
	}{
		{"cache_creation_sum_overflow", UsageLog{CacheCreation5mTokens: maxIntValue(), CacheCreation1hTokens: 1}},
		{"input_sum_overflow", UsageLog{UpstreamInputTokens: maxIntValue(), CacheReadTokens: 1}},
		{"context_sum_overflow", UsageLog{UpstreamInputTokens: maxIntValue(), UpstreamOutputTokens: 1}},
	} {
		t.Run(tc.code, func(t *testing.T) {
			before := tc.log
			audit := AuditUsageLimits(&tc.log)
			require.Equal(t, "invalid", audit.Status)
			require.Contains(t, audit.Issues, tc.code)
			require.Nil(t, audit.TotalContextTokens)
			require.Equal(t, before, tc.log)
		})
	}
}

func TestAuditUsageLimitsPartialOrMissingLimitsRemainUnknown(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	log.Account = nil
	audit := pricing.AuditUsageLimits(log)
	require.Equal(t, "unknown", audit.Status)
	require.Equal(t, []string{"missing_context_limit"}, audit.Issues)
	require.Nil(t, audit.ContextLimit, "max_tokens is an output field, never a context limit")

	log.UpstreamOutputTokens = 101
	require.Equal(t, "invalid", pricing.AuditUsageLimits(log).Status, "known violations take precedence over missing limits")

	audit = (*PricingService)(nil).AuditUsageLimits(log)
	require.Equal(t, "unknown", audit.Status)
	require.ElementsMatch(t, []string{"missing_input_limit", "missing_output_limit", "missing_context_limit"}, audit.Issues)
	require.Nil(t, audit.InputLimit)
	require.Nil(t, audit.OutputLimit)

	audit = AuditUsageLimits(nil)
	require.Equal(t, "unknown", audit.Status)
	require.Equal(t, []string{"missing_usage"}, audit.Issues)
}

func TestAuditUsageLimitsDoesNotSubstituteBilledCountsForMissingRawUsage(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	log.UpstreamInputTokens, log.UpstreamOutputTokens = 0, 0
	log.InputTokens, log.OutputTokens = 100, 80
	audit := pricing.AuditUsageLimits(log)
	require.Equal(t, "unknown", audit.Status)
	require.ElementsMatch(t, []string{"upstream_input_unavailable", "upstream_output_unavailable"}, audit.Issues)
	require.Nil(t, audit.TotalInputTokens)
	require.Nil(t, audit.TotalOutputTokens)
	require.Nil(t, audit.TotalContextTokens)

	log.InputTokens, log.OutputTokens = 0, 0
	require.Equal(t, "valid", pricing.AuditUsageLimits(log).Status, "zero is valid when no billed count contradicts it")
}

func TestAuditUsageLimitsModelIdentityAndMetadataPrecedence(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	log.RequestedModel = "gpt-6-astra"
	log.UpstreamModel = usageAuditString("claude-opus-4-6")
	log.UpstreamResponseModel = usageAuditString("known-model")
	log.Account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{
			"known-model": {ID: "known-model", ContextWindow: 300, MaxOutputTokens: 90},
		},
	})
	audit := pricing.AuditUsageLimits(log)
	require.Equal(t, "known-model", audit.Model)
	require.EqualValues(t, 90, *audit.OutputLimit, "account metadata overrides generic catalog output limits")

	log.UpstreamResponseModel = usageAuditString("unlisted-model")
	audit = pricing.AuditUsageLimits(log)
	require.Equal(t, "unlisted-model", audit.Model)
	require.Nil(t, audit.InputLimit, "do not audit an unknown actual model with requested-model limits")
	require.Nil(t, audit.ContextLimit)

	log.UpstreamResponseModel = nil
	require.Equal(t, "claude-opus-4-6", pricing.AuditUsageLimits(log).Model)
	log.UpstreamModel = nil
	require.Equal(t, "gpt-6-astra", pricing.AuditUsageLimits(log).Model)
	log.RequestedModel = ""
	require.Equal(t, "known-model", pricing.AuditUsageLimits(log).Model)
}

func TestAuditUsageLimitsKnownCacheStillExceedsLimitsWhenRawUsageIsMissing(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	log.UpstreamInputTokens, log.UpstreamOutputTokens = 0, 0
	log.InputTokens, log.OutputTokens = 100, 80
	log.CacheReadTokens = 301
	log.CacheCreationTokens, log.CacheCreation5mTokens, log.CacheCreation1hTokens = 0, 0, 0
	audit := pricing.AuditUsageLimits(log)
	require.Equal(t, "invalid", audit.Status)
	require.Contains(t, audit.Issues, "input_limit_exceeded", "known cache usage alone already exceeds the input ceiling")
	require.Contains(t, audit.Issues, "context_limit_exceeded", "unknown non-cache/output cannot make the known cache count fit")
	require.Nil(t, audit.TotalInputTokens)
	require.Nil(t, audit.TotalContextTokens)
}

func TestAuditUsageLimitsNilPricingReceiverUsesAccountSnapshot(t *testing.T) {
	_, log := usageAuditFixture(t)
	log.UpstreamOutputTokens = 101
	audit := (*PricingService)(nil).AuditUsageLimits(log)
	require.Equal(t, "invalid", audit.Status)
	require.EqualValues(t, 100, *audit.OutputLimit)
	require.EqualValues(t, 300, *audit.ContextLimit)
	require.Nil(t, audit.InputLimit)
	require.Contains(t, audit.Issues, "output_limit_exceeded")
	require.Contains(t, audit.Issues, "missing_input_limit")
}

func TestAuditUsageLimitsDoesNotSerializeUnsafeIntegerTotals(t *testing.T) {
	const maxSafe = int64(1<<53 - 1)
	if int64(maxIntValue()) <= maxSafe {
		t.Skip("unsafe JavaScript integer inputs require a 64-bit int")
	}
	for _, tc := range []struct {
		name   string
		log    UsageLog
		issues []string
	}{
		{"raw input", UsageLog{UpstreamInputTokens: int(maxSafe + 1)}, []string{"token_count_unsafe_integer", "input_sum_unsafe_integer", "context_sum_unsafe_integer"}},
		{"raw output", UsageLog{UpstreamOutputTokens: int(maxSafe + 1)}, []string{"token_count_unsafe_integer", "context_sum_unsafe_integer"}},
		{"cache detail sum", UsageLog{CacheCreation5mTokens: int(maxSafe), CacheCreation1hTokens: 1}, []string{"cache_creation_sum_unsafe_integer", "input_sum_unsafe_integer", "context_sum_unsafe_integer"}},
		{"input sum", UsageLog{UpstreamInputTokens: int(maxSafe), CacheReadTokens: 1}, []string{"input_sum_unsafe_integer", "context_sum_unsafe_integer"}},
		{"context sum", UsageLog{UpstreamInputTokens: int(maxSafe), UpstreamOutputTokens: 1}, []string{"context_sum_unsafe_integer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.log
			audit := AuditUsageLimits(&tc.log)
			require.Equal(t, "invalid", audit.Status)
			for _, code := range tc.issues {
				require.Contains(t, audit.Issues, code)
			}
			require.Nil(t, audit.TotalContextTokens)
			for _, value := range []*int64{audit.TotalInputTokens, audit.TotalOutputTokens} {
				if value != nil {
					require.LessOrEqual(t, *value, maxSafe)
				}
			}
			require.Equal(t, before, tc.log)
		})
	}
	log := &UsageLog{UpstreamInputTokens: int(maxSafe)}
	audit := AuditUsageLimits(log)
	require.Equal(t, maxSafe, *audit.TotalInputTokens)
	require.Equal(t, maxSafe, *audit.TotalContextTokens)
}

func TestAuditUsageLimitsDoesNotSerializeUnsafeIntegerLimits(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	unsafe := int64(1 << 53)
	pricing.pricingData[log.Model].MaxInputTokens = &unsafe
	pricing.pricingData[log.Model].MaxOutputTokens = &unsafe
	log.Account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{
			log.Model: {ID: log.Model, ContextWindow: unsafe, MaxOutputTokens: unsafe},
		},
	})
	audit := pricing.AuditUsageLimits(log)
	require.Equal(t, "unknown", audit.Status)
	require.Nil(t, audit.InputLimit)
	require.Nil(t, audit.OutputLimit)
	require.Nil(t, audit.ContextLimit)
	require.ElementsMatch(t, []string{"input_limit_unsafe_integer", "output_limit_unsafe_integer", "context_limit_unsafe_integer"}, audit.Issues)
	require.Equal(t, unsafe, *pricing.pricingData[log.Model].MaxInputTokens)
}

func TestAuditUsageLimitsOnlyUsesExactOrCanonicalModelNames(t *testing.T) {
	for _, tc := range []struct {
		model string
		input int64
	}{
		{"gpt-6-astra", 300},
		{"models/gpt-6", 300},
		{"GPT-6-ASTRA", 300},
		{"gpt-999-future", 0},
		{"fake-claude-opus-4-6", 0},
		{"claude-opus-4-6-unlisted", 0},
		{"gpt-6-astra-20990101", 0},
	} {
		t.Run(tc.model, func(t *testing.T) {
			pricing, log := usageAuditFixture(t)
			log.Model, log.Account = tc.model, nil
			audit := pricing.AuditUsageLimits(log)
			if tc.input == 0 {
				require.Nil(t, audit.InputLimit)
				require.Nil(t, audit.OutputLimit)
			} else {
				require.Equal(t, tc.input, *audit.InputLimit)
			}
		})
	}
}

func TestAuditUsageLimitsExactAliasEntryWins(t *testing.T) {
	pricing, log := usageAuditFixture(t)
	data, err := pricing.parsePricingData([]byte(`{"gpt-6":{"input_cost_per_token":0.000001,"max_input_tokens":350}}`))
	require.NoError(t, err)
	pricing.pricingData["gpt-6"] = data["gpt-6"]
	log.Model, log.Account = "gpt-6", nil
	require.EqualValues(t, 350, *pricing.AuditUsageLimits(log).InputLimit)
}

func TestPricingTokenLimitsMalformedMetadataDoesNotDiscardPrices(t *testing.T) {
	pricing := &PricingService{}
	data, err := pricing.parsePricingData([]byte(`{
		"valid": {"input_cost_per_token": 0.000001, "max_input_tokens": 1000, "max_output_tokens": 100},
		"missing": {"input_cost_per_token": 0.000002, "max_tokens": 100},
		"invalid": {"input_cost_per_token": 0.000003, "max_input_tokens": "unlimited", "max_output_tokens": 1.5},
		"nonpositive": {"input_cost_per_token": 0.000004, "max_input_tokens": -1, "max_output_tokens": 0}
	}`))
	require.NoError(t, err)
	pricing.pricingData = data
	require.Len(t, data, 4)
	require.Equal(t, 0.000003, data["invalid"].InputCostPerToken)
	for _, model := range []string{"missing", "invalid", "nonpositive"} {
		audit := pricing.AuditUsageLimits(&UsageLog{Model: model})
		require.Nil(t, audit.InputLimit, model)
		require.Nil(t, audit.OutputLimit, model)
		require.Nil(t, audit.ContextLimit, model)
	}
}

func usageAuditString(value string) *string { return &value }
