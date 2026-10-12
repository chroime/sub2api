//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func auditTestBillingService() *BillingService {
	return &BillingService{fallbackPrices: map[string]*ModelPricing{
		"claude-sonnet-4": {
			InputPricePerToken: 2e-6, OutputPricePerToken: 8e-6,
			CacheReadPricePerToken: .2e-6, CacheCreationPricePerToken: 3e-6,
			CacheCreation5mPrice: 3e-6, CacheCreation1hPrice: 4e-6,
			SupportsCacheBreakdown: true,
		},
	}}
}

func auditAdjustedLog() UsageLog {
	return UsageLog{
		ID: 1, Model: "claude-sonnet-4", InputTokens: 200, OutputTokens: 40,
		UpstreamInputTokens: 100, UpstreamOutputTokens: 20,
		RateMultiplier: 1.3,
		InputCost:      8, OutputCost: 2, TotalCost: 10, ActualCost: 13,
	}
}

func TestUsageCostAudit_UnifiedMultiplierUsesOriginalCacheBuckets(t *testing.T) {
	bs := auditTestBillingService()
	svc := NewUsageCostAuditService(bs, nil, nil, nil)
	row := auditAdjustedLog()
	row.CacheReadTokens = 800
	row.CacheCreationTokens = 200
	row.CacheCreation5mTokens = 40
	row.CacheCreation1hTokens = 160
	row.UpstreamCacheReadTokens = 400
	row.UpstreamCacheCreationTokens = 100
	row.UpstreamCacheCreation5mTokens = 20
	row.UpstreamCacheCreation1hTokens = 80
	result := svc.EstimateBatch(context.Background(), []UsageLog{row})[row.ID]

	require.Equal(t, "calculated", result.Status)
	// Original buckets: 100*2e-6 + 20*8e-6 + 400*.2e-6 + 20*3e-6 + 80*4e-6.
	require.InDelta(t, .00082, *result.TotalCost, 1e-12)
}

func TestUsageCostAudit_RawCacheBucketsAndTTLUseIndependentPrices(t *testing.T) {
	bs := auditTestBillingService()
	svc := NewUsageCostAuditService(bs, nil, nil, nil)
	row := auditAdjustedLog()
	row.CacheReadTokens = 400
	row.CacheCreationTokens = 100
	row.CacheCreation5mTokens = 20
	row.CacheCreation1hTokens = 80
	row.UpstreamCacheReadTokens = 400
	row.UpstreamCacheCreationTokens = 100
	row.UpstreamCacheCreation5mTokens = 20
	row.UpstreamCacheCreation1hTokens = 80
	before := row
	result := svc.EstimateBatch(context.Background(), []UsageLog{row})[row.ID]

	require.Equal(t, "calculated", result.Status)
	require.Equal(t, "current_pricing_calculation", result.Reason)
	require.NotNil(t, result.Amount)
	require.NotNil(t, result.TotalCost)
	require.NotNil(t, result.BilledAmount)
	require.NotNil(t, result.Difference)
	// 100*2e-6 + 20*8e-6 + 400*.2e-6 + 20*3e-6 + 80*4e-6.
	require.InDelta(t, .00082, *result.TotalCost, 1e-12)
	require.InDelta(t, .001066, *result.Amount, 1e-12)
	require.InDelta(t, 13, *result.BilledAmount, 1e-12)
	require.InDelta(t, 12.99918, *result.Difference, 1e-12)
	require.Equal(t, before, row)
}

func TestUsageCostAudit_DoesNotUseBilledCacheWhenRawCacheBucketsAreMissing(t *testing.T) {
	row := auditAdjustedLog()
	row.CacheReadTokens = 400
	row.CacheCreationTokens = 100

	result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]

	require.Equal(t, "unavailable", result.Status)
	require.Equal(t, "missing_upstream_cache_usage", result.Reason)
	require.Nil(t, result.Amount)
}

func TestUsageCostAudit_RecordedOnlyWhenOriginalBillIsUnadjusted(t *testing.T) {
	row := auditAdjustedLog()
	row.InputTokens = row.UpstreamInputTokens
	row.OutputTokens = row.UpstreamOutputTokens
	row.InputCost, row.OutputCost, row.TotalCost, row.ActualCost = .03, .04, .07, .091
	// An original bill needs no current pricing dependency or group lookup.
	svc := NewUsageCostAuditService(nil, nil, nil, nil)
	result := svc.EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "recorded", result.Status)
	require.Equal(t, "original_usage_unadjusted", result.Reason)
	require.Equal(t, .091, *result.Amount)
	require.Equal(t, .07, *result.TotalCost)
}

func TestUsageCostAudit_MissingRawTokensAreNotTreatedAsZero(t *testing.T) {
	for _, change := range []func(*UsageLog){
		func(row *UsageLog) { row.UpstreamInputTokens, row.UpstreamOutputTokens = 0, 0 },
		func(row *UsageLog) { row.UpstreamInputTokens = 0 },
		func(row *UsageLog) { row.UpstreamOutputTokens = 0 },
		func(row *UsageLog) {
			row.UpstreamInputTokens, row.UpstreamOutputTokens = 0, 0
			row.InputTokens, row.OutputTokens, row.CacheReadTokens = 0, 0, 100
		},
	} {
		row := auditAdjustedLog()
		change(&row)
		result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
			EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
		require.Equal(t, "unavailable", result.Status)
		require.Equal(t, "missing_upstream_usage", result.Reason)
		require.Nil(t, result.Amount)
		require.Nil(t, result.TotalCost)
	}
}

func TestUsageCostAudit_UnsupportedAndMalformedRecords(t *testing.T) {
	tests := []struct {
		name   string
		change func(*UsageLog)
		reason string
	}{
		{"negative input", func(r *UsageLog) { r.UpstreamInputTokens = -1 }, "invalid_usage"},
		{"unsafe integer", func(r *UsageLog) { r.CacheReadTokens = int(1 << 53) }, "invalid_usage"},
		{"unsafe sum", func(r *UsageLog) { r.CacheReadTokens = int(1<<53) - 1 }, "invalid_usage"},
		{"inconsistent TTL", func(r *UsageLog) { r.CacheCreationTokens, r.CacheCreation1hTokens = 10, 20 }, "invalid_usage"},
		{"NaN rate", func(r *UsageLog) { r.RateMultiplier = math.NaN() }, "invalid_rate_multiplier"},
		{"negative rate", func(r *UsageLog) { r.RateMultiplier = -1 }, "invalid_rate_multiplier"},
		{"per-request", func(r *UsageLog) { r.BillingMode = testPtrString("per_request") }, "unsupported_billing_mode"},
		{"image", func(r *UsageLog) { r.ImageInputTokens = 20 }, "unsupported_billing_mode"},
		{"video", func(r *UsageLog) { r.VideoCount = 1 }, "unsupported_billing_mode"},
		{"audio", func(r *UsageLog) { r.InboundEndpoint = testPtrString("/v1/audio/speech") }, "unsupported_billing_mode"},
		{"search", func(r *UsageLog) { r.Model = "gpt-5-search-api" }, "unsupported_billing_mode"},
		{"search surcharge", func(r *UsageLog) { r.TotalCost = 10.01 }, "incomplete_cost_breakdown"},
		{"cyber blocked", func(r *UsageLog) { r.RequestType = RequestTypeCyberBlocked }, "unsupported_billing_mode"},
		{"unknown model", func(r *UsageLog) { r.Model = "no-known-price" }, "pricing_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := auditAdjustedLog()
			tt.change(&row)
			result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
				EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
			require.Equal(t, "unavailable", result.Status)
			require.Equal(t, tt.reason, result.Reason)
			require.Nil(t, result.Amount)
		})
	}
}

func TestUsageCostAudit_FreeRateRemainsZero(t *testing.T) {
	for _, unchanged := range []bool{false, true} {
		row := auditAdjustedLog()
		row.RateMultiplier, row.ActualCost = 0, 0
		if unchanged {
			row.InputTokens, row.OutputTokens = row.UpstreamInputTokens, row.UpstreamOutputTokens
		}
		result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
			EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
		wantStatus := "calculated"
		if unchanged {
			wantStatus = "recorded"
		}
		require.Equal(t, wantStatus, result.Status)
		require.NotNil(t, result.Amount)
		require.Zero(t, *result.Amount)
		require.Positive(t, *result.TotalCost)
	}
}

func TestUsageCostAudit_CacheFoldingCannotBeInferredFromMatchingDisplayedTokens(t *testing.T) {
	row := auditAdjustedLog()
	row.InputTokens, row.OutputTokens = row.UpstreamInputTokens, row.UpstreamOutputTokens
	row.CacheReadTokens, row.UpstreamCacheReadTokens = 100, 100
	// A tiny output-only adjustment can round back to the original output
	// count, yet still fold cache reads into the customer input charge.
	result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "calculated", result.Status)
	require.InDelta(t, .000494, *result.Amount, 1e-12)
}

func TestUsageCostAudit_InconsistentOriginalBillIsNotClaimedAsRecorded(t *testing.T) {
	row := auditAdjustedLog()
	row.InputTokens, row.OutputTokens = row.UpstreamInputTokens, row.UpstreamOutputTokens
	row.ActualCost = 99
	result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "calculated", result.Status)
	require.InDelta(t, .000468, *result.Amount, 1e-12)
}

func TestUsageCostAudit_UnknownFamilyAliasDoesNotInventAUsablePrice(t *testing.T) {
	row := auditAdjustedLog()
	row.Model = "unpublished-sonnet-4-future"
	result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "unavailable", result.Status)
	require.Equal(t, "pricing_unavailable", result.Reason)
}

func TestUsageCostAudit_UnknownOpenAIModelDoesNotAcquireFamilyFallback(t *testing.T) {
	row := auditAdjustedLog()
	row.Model = "gpt-5-future-unpublished"
	result := NewUsageCostAuditService(newTestBillingService(), nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "unavailable", result.Status)
	require.Equal(t, "pricing_unavailable", result.Reason)
	require.Nil(t, result.Amount)
}

func TestUsageCostAudit_ExplicitCodexAutoReviewPricingRemainsUsable(t *testing.T) {
	bs := auditTestBillingService()
	bs.pricingService = newStubPricingServiceFromJSON(t, `{
		"codex-auto-review": {"input_cost_per_token": 0.0000002, "output_cost_per_token": 0.0000012, "cache_read_input_token_cost": 0.00000002}
	}`)
	row := auditAdjustedLog()
	row.Model, row.CacheReadTokens, row.UpstreamCacheReadTokens = "codex-auto-review", 100, 100
	result := NewUsageCostAuditService(bs, nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "calculated", result.Status)
	require.Equal(t, "codex-auto-review", result.Model)
	// (100*.2e-6 + 20*1.2e-6 + 100*.02e-6)*1.3.
	require.InDelta(t, .0000598, *result.Amount, 1e-12)
}

type auditGroupRepository struct {
	GroupRepository
	groups map[int64]*Group
	calls  map[int64]int
	err    error
}

func (r *auditGroupRepository) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	r.calls[id]++
	return r.groups[id], r.err
}

func TestUsageCostAudit_GroupPricingTierEffortAndBatchCaching(t *testing.T) {
	groupID := int64(7)
	repo := &auditGroupRepository{
		calls: map[int64]int{},
		groups: map[int64]*Group{groupID: {
			ID: groupID, RateMultiplier: 99,
			ModelPricing: []ChannelModelPricing{{
				Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
				InputPrice: testPtrFloat64(5e-6), OutputPrice: testPtrFloat64(10e-6),
				FastMultiplier: testPtrFloat64(3), ReasoningEffortMultipliers: map[string]float64{"high": 2},
			}},
		}},
	}
	first := auditAdjustedLog()
	first.GroupID, first.ServiceTier, first.ReasoningEffort = &groupID, testPtrString("priority"), testPtrString("high")
	second := first
	second.ID = 2
	svc := NewUsageCostAuditService(auditTestBillingService(), nil, repo, nil)
	results := svc.EstimateBatch(context.Background(), []UsageLog{first, second})
	for _, id := range []int64{1, 2} {
		require.Equal(t, "calculated", results[id].Status)
		// (100*5e-6 + 20*10e-6)*3*2*1.3; ignore today's group rate of 99.
		require.InDelta(t, .00546, *results[id].Amount, 1e-12)
	}
	require.Equal(t, 1, repo.calls[groupID])
}

func TestUsageCostAudit_GroupLookupFailureIsCachedAndDoesNotUsePublicPrice(t *testing.T) {
	groupID := int64(8)
	repo := &auditGroupRepository{calls: map[int64]int{}, err: errors.New("unavailable")}
	first := auditAdjustedLog()
	first.GroupID = &groupID
	second := first
	second.ID = 2
	results := NewUsageCostAuditService(auditTestBillingService(), nil, repo, nil).
		EstimateBatch(context.Background(), []UsageLog{first, second})
	for _, id := range []int64{1, 2} {
		require.Equal(t, "unavailable", results[id].Status)
		require.Equal(t, "group_unavailable", results[id].Reason)
		require.Nil(t, results[id].Amount)
	}
	require.Equal(t, 1, repo.calls[groupID])
}

func TestUsageCostAudit_HydratedGroupSkipsRepositoryAndMismatchedGroupDoesNot(t *testing.T) {
	for _, matchingID := range []bool{false, true} {
		groupID := int64(4)
		repo := &auditGroupRepository{calls: map[int64]int{}, err: errors.New("must not be used for a hydrated group")}
		row := auditAdjustedLog()
		row.GroupID = &groupID
		row.Group = &Group{ID: groupID, ModelPricing: []ChannelModelPricing{{
			Models: []string{row.Model}, BillingMode: BillingModeToken,
			InputPrice: testPtrFloat64(10e-6), OutputPrice: testPtrFloat64(20e-6),
		}}}
		if !matchingID {
			row.Group.ID++
		}
		result := NewUsageCostAuditService(auditTestBillingService(), nil, repo, nil).
			EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
		if matchingID {
			require.Equal(t, "calculated", result.Status)
			require.InDelta(t, .00182, *result.Amount, 1e-12)
			require.Zero(t, repo.calls[groupID])
		} else {
			require.Equal(t, "unavailable", result.Status)
			require.Equal(t, "group_unavailable", result.Reason)
			require.Equal(t, 1, repo.calls[groupID])
		}
	}
}

func TestUsageCostAudit_RawLongContextUsesGroupPolicy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		bs := auditTestBillingService()
		bs.fallbackPrices["claude-sonnet-4"].LongContextInputThreshold = 100
		bs.fallbackPrices["claude-sonnet-4"].LongContextInputMultiplier = 2
		bs.fallbackPrices["claude-sonnet-4"].LongContextOutputMultiplier = 1.5
		groupID := int64(9)
		repo := &auditGroupRepository{calls: map[int64]int{}, groups: map[int64]*Group{
			groupID: {ID: groupID, LongContextPricingEnabled: enabled},
		}}
		row := auditAdjustedLog()
		row.GroupID, row.CacheReadTokens, row.UpstreamCacheReadTokens = &groupID, 100, 100
		result := NewUsageCostAuditService(bs, nil, repo, nil).
			EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
		require.Equal(t, "calculated", result.Status)
		want := .000494 // (100*2e-6 + 20*8e-6 + 100*.2e-6)*1.3.
		if enabled {
			want = .000884 // (100*4e-6 + 20*12e-6 + 100*.4e-6)*1.3.
		}
		require.InDelta(t, want, *result.Amount, 1e-12)
	}
}

func TestUsageCostAudit_OpenAIAccountCanEnableLongContextWithoutGroupGate(t *testing.T) {
	tests := []struct {
		name                   string
		platform               string
		accountGate, groupGate bool
		want                   float64
	}{
		{"account only", PlatformOpenAI, true, false, .000884},
		{"both disabled", PlatformOpenAI, false, false, .000494},
		{"account false does not veto group", PlatformOpenAI, false, true, .000884},
		{"non OpenAI ignores account key", PlatformAnthropic, true, false, .000494},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := auditTestBillingService()
			bs.fallbackPrices["claude-sonnet-4"].LongContextInputThreshold = 100
			bs.fallbackPrices["claude-sonnet-4"].LongContextInputMultiplier = 2
			bs.fallbackPrices["claude-sonnet-4"].LongContextOutputMultiplier = 1.5
			groupID := int64(9)
			repo := &auditGroupRepository{calls: map[int64]int{}, groups: map[int64]*Group{
				groupID: {ID: groupID, LongContextPricingEnabled: tt.groupGate},
			}}
			row := auditAdjustedLog()
			row.GroupID, row.CacheReadTokens, row.UpstreamCacheReadTokens = &groupID, 100, 100
			row.Account = &Account{Platform: tt.platform, Extra: map[string]any{openAILongContextBillingEnabledKey: tt.accountGate}}
			result := NewUsageCostAuditService(bs, nil, repo, nil).
				EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
			require.Equal(t, "calculated", result.Status)
			require.InDelta(t, tt.want, *result.Amount, 1e-12)
		})
	}
}

func TestUsageCostAudit_AccountLongContextSelectsSameIntervalForPriceValidationAndCost(t *testing.T) {
	groupID := int64(1)
	cs := newTestChannelServiceWithCache(t, &channelCache{
		channelByGroupID: map[int64]*Channel{groupID: {ID: 11, Status: StatusActive}},
		pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{{groupID: groupID, model: "custom-model"}: {
			BillingMode: BillingModeToken, Intervals: []PricingInterval{{
				MinTokens: 100, InputPrice: testPtrFloat64(4e-6), OutputPrice: testPtrFloat64(12e-6), CacheReadPrice: testPtrFloat64(.4e-6),
			}},
		}},
	})
	repo := &auditGroupRepository{calls: map[int64]int{}, groups: map[int64]*Group{groupID: {ID: groupID}}}
	row := auditAdjustedLog()
	row.Model, row.GroupID, row.CacheReadTokens, row.UpstreamCacheReadTokens = "custom-model", &groupID, 100, 100
	row.Account = &Account{Platform: PlatformOpenAI, Extra: map[string]any{openAILongContextBillingEnabledKey: true}}
	result := NewUsageCostAuditService(auditTestBillingService(), nil, repo, cs).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "calculated", result.Status, result.Reason)
	require.InDelta(t, .000884, *result.Amount, 1e-12)
}

func TestUsageCostAudit_ShadowAccountDoesNotGuessParentLongContextGate(t *testing.T) {
	row := auditAdjustedLog()
	parentID := int64(99)
	row.Account = &Account{Platform: PlatformOpenAI, ParentAccountID: &parentID}
	result := NewUsageCostAuditService(auditTestBillingService(), nil, nil, nil).
		EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
	require.Equal(t, "unavailable", result.Status)
	require.Nil(t, result.Amount)
}

func TestUsageCostAudit_ChannelSourceAndRecordedMapping(t *testing.T) {
	tests := []struct {
		name, source, model, chain, wantModel, wantStatus string
	}{
		{"requested", BillingModelSourceRequested, "claude-sonnet-4", "public→mapped→claude-sonnet-4", "public", "calculated"},
		{"channel mapped", BillingModelSourceChannelMapped, "claude-sonnet-4", "public→mapped→claude-sonnet-4", "mapped", "calculated"},
		{"ambiguous two step mapping", BillingModelSourceChannelMapped, "claude-sonnet-4", "public→mapped", "", "unavailable"},
		{"ambiguous upstream", BillingModelSourceUpstream, "public", "public→claude-sonnet-4", "", "unavailable"},
		{"unknown response adoption", BillingModelSourceResponse, "public", "public→claude-sonnet-4", "", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groupID, channelID := int64(1), int64(11)
			cs := newTestChannelServiceWithCache(t, &channelCache{
				channelByGroupID: map[int64]*Channel{groupID: {ID: channelID, Status: StatusActive, BillingModelSource: tt.source}},
			})
			repo := &auditGroupRepository{calls: map[int64]int{}, groups: map[int64]*Group{groupID: {
				ID: groupID, ModelPricing: []ChannelModelPricing{{
					Models: []string{"public", "mapped"}, BillingMode: BillingModeToken,
					InputPrice: testPtrFloat64(10e-6), OutputPrice: testPtrFloat64(20e-6),
				}},
			}}}
			row := auditAdjustedLog()
			row.GroupID, row.ChannelID = &groupID, &channelID
			row.Model, row.RequestedModel = tt.model, "public"
			row.ModelMappingChain = testPtrString(tt.chain)
			row.UpstreamModel, row.UpstreamResponseModel = testPtrString("claude-sonnet-4"), testPtrString("claude-sonnet-4")
			result := NewUsageCostAuditService(auditTestBillingService(), nil, repo, cs).
				EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
			require.Equal(t, tt.wantStatus, result.Status, result.Reason)
			if tt.wantStatus == "calculated" {
				require.Equal(t, tt.wantModel, result.Model)
				require.InDelta(t, .00182, *result.Amount, 1e-12)
			} else {
				require.Equal(t, "billing_model_unavailable", result.Reason)
				require.Nil(t, result.Amount)
			}
		})
	}
}

func TestUsageCostAudit_CustomModelNeedsExplicitPricesForUsedBuckets(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		groupID := int64(3)
		card := ChannelModelPricing{Models: []string{"custom-model"}, BillingMode: BillingModeToken}
		if explicit {
			card.InputPrice, card.OutputPrice = testPtrFloat64(0), testPtrFloat64(0)
		}
		repo := &auditGroupRepository{calls: map[int64]int{}, groups: map[int64]*Group{
			groupID: {ID: groupID, ModelPricing: []ChannelModelPricing{card}},
		}}
		row := auditAdjustedLog()
		row.Model, row.GroupID = "custom-model", &groupID
		result := NewUsageCostAuditService(auditTestBillingService(), nil, repo, nil).
			EstimateBatch(context.Background(), []UsageLog{row})[row.ID]
		if explicit {
			require.Equal(t, "calculated", result.Status)
			require.Zero(t, *result.Amount)
		} else {
			require.Equal(t, "unavailable", result.Status)
			require.Equal(t, "pricing_unavailable", result.Reason)
			require.Nil(t, result.Amount)
		}
	}
}
