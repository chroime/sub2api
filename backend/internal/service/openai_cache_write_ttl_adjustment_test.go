//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAICacheWriteTTLRetainsGroupTokenAdjustmentAndRawAccountCost(t *testing.T) {
	for _, test := range []struct {
		name                                   string
		unified, input                         float64
		minimum                                int
		wantInput, wantWrite, want30m, want12h int
	}{
		{name: "neutral", wantInput: 100, wantWrite: 300, want30m: 100, want12h: 200},
		{name: "unified", unified: 2, wantInput: 200, wantWrite: 600, want30m: 200, want12h: 400},
		{name: "split", input: 2, wantInput: 800},
		{name: "below threshold", unified: 2, minimum: 101, wantInput: 100, wantWrite: 300, want30m: 100, want12h: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			var pricing ChannelModelPricing
			require.NoError(t, json.Unmarshal([]byte(`{"platform":"openai","models":["gpt-6-luna"],"billing_mode":"token","input_price":0.000001,"output_price":0.000002,"cache_write_price":0.00000125,"cache_write_12h_price":0.0000025,"cache_read_price":0.0000001}`), &pricing))
			const groupID int64 = 777
			channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: []ChannelModelPricing{pricing}, ApplyPricingToAccountStats: true}
			channel.AccountStatsPricingRules = []AccountStatsPricingRule{{GroupIDs: []int64{groupID}, Pricing: []ChannelModelPricing{pricing}}}
			svc.channelService = &ChannelService{}
			svc.channelService.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{groupID: PlatformOpenAI}))
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			group := &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1, BillingUnifiedTokenMultiplier: test.unified, BillingInputTokenMultiplier: test.input, BillingTokenAdjustmentMinInputTokens: test.minimum}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "ttl-adjustment-" + test.name, Model: "gpt-6-luna", Usage: OpenAIUsage{InputTokens: 400, OutputTokens: 10, CacheCreationInputTokens: 300, CacheCreation30mInputTokens: 100, CacheCreation12hInputTokens: 200}},
				APIKey: &APIKey{ID: 1, GroupID: i64p(groupID), Group: group}, User: &User{ID: 1}, Account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-6-luna", BillingModelSource: BillingModelSourceRequested},
			})
			require.NoError(t, err)
			log := usageRepo.lastLog
			require.NotNil(t, log)
			require.Equal(t, test.wantInput, log.InputTokens)
			require.Equal(t, test.wantWrite, log.CacheCreationTokens)
			require.Equal(t, test.want30m, log.CacheCreation30mTokens)
			require.Equal(t, test.want12h, log.CacheCreation12hTokens)
			require.Equal(t, 300, log.UpstreamCacheCreationTokens)
			require.Equal(t, 100, log.UpstreamInputTokens)
			wantCacheCost := float64(test.want30m)*1.25e-6 + float64(test.want12h)*2.5e-6
			require.InDelta(t, wantCacheCost, log.CacheCreationCost, 1e-12)
			require.NotNil(t, log.AccountStatsCost)
			require.InDelta(t, 100*1e-6+10*2e-6+100*1.25e-6+200*2.5e-6, *log.AccountStatsCost, 1e-12)
		})
	}
}
