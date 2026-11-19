//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheCreationGroupAdjustmentPreservesRawCost(t *testing.T) {
	for _, gateway := range []string{"anthropic", "openai"} {
		for _, snapshot := range []struct {
			name                  string
			aggregate, five, hour int
		}{
			{"complete", 100, 20, 80},
			{"TTL only", 0, 20, 80},
			{"oversized TTL detail", 100, 50, 200},
		} {
			for _, mode := range []struct {
				name                 string
				unified, input       float64
				minimum              int
				wantInput, wantWrite int
				wantCost             float64
			}{
				{"neutral", 0, 0, 0, 100, 100, 0.482},
				{"unified", 2, 0, 0, 200, 200, 0.964},
				{"split", 0, 2, 0, 440, 0, 0.46},
				{"below threshold", 2, 0, 101, 100, 100, 0.482},
			} {
				t.Run(gateway+"/"+snapshot.name+"/"+mode.name, func(t *testing.T) {
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					billingRepo := &openAIRecordUsageBillingRepoStub{}
					billing := newTestBillingServiceWithPrices(map[string]*ModelPricing{
						"claude-sonnet-4": {
							InputPricePerToken: 0.001, OutputPricePerToken: 0.002,
							CacheReadPricePerToken: 0.0001, CacheCreationPricePerToken: 0.002,
							SupportsCacheBreakdown: true, CacheCreation5mPrice: 0.002, CacheCreation1hPrice: 0.004,
						},
					})
					const groupID int64 = 10
					group := &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1,
						BillingUnifiedTokenMultiplier: mode.unified, BillingInputTokenMultiplier: mode.input,
						BillingTokenAdjustmentMinInputTokens: mode.minimum}
					account := &Account{ID: 701, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
					key := &APIKey{ID: 501, GroupID: i64p(groupID), Group: group}
					user := &User{ID: 601}
					channel := &Channel{ID: 1, Status: StatusActive, AccountStatsPricingRules: []AccountStatsPricingRule{{
						AccountIDs: []int64{account.ID}, Pricing: []ChannelModelPricing{{
							Platform: PlatformAnthropic, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
							InputPrice: float64Ptr(0.01), OutputPrice: float64Ptr(0.02), CacheReadPrice: float64Ptr(0.001),
							CacheWritePrice: float64Ptr(0.02), CacheWrite1hPrice: float64Ptr(0.04),
						}},
					}}}
					channels := newTestChannelServiceForStats(t, channel, groupID, PlatformAnthropic)
					if gateway == "anthropic" {
						svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
						svc.billingService, svc.channelService = billing, channels
						svc.resolver = NewModelPricingResolver(channels, billing)
						require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
							Result: &ForwardResult{RequestID: t.Name(), Model: "claude-sonnet-4", Usage: ClaudeUsage{
								InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 20,
								CacheCreationInputTokens: snapshot.aggregate, CacheCreation5mTokens: snapshot.five, CacheCreation1hTokens: snapshot.hour,
							}}, APIKey: key, User: user, Account: account,
						}))
					} else {
						svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
						svc.billingService, svc.channelService = billing, channels
						svc.resolver = NewModelPricingResolver(channels, billing)
						require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
							Result: &OpenAIForwardResult{RequestID: t.Name(), Model: "claude-sonnet-4", Usage: OpenAIUsage{
								InputTokens: 220, OutputTokens: 10, CacheReadInputTokens: 20,
								CacheCreationInputTokens: snapshot.aggregate, CacheCreation5mTokens: snapshot.five, CacheCreation1hTokens: snapshot.hour,
							}}, APIKey: key, User: user, Account: account,
						}))
					}
					log := usageRepo.lastLog
					require.NotNil(t, log)
					require.Equal(t, mode.wantInput, log.InputTokens)
					require.InDelta(t, mode.wantCost, log.TotalCost, 1e-12)
					require.Equal(t, 1, billingRepo.calls)
					require.InDelta(t, mode.wantCost, billingRepo.lastCmd.BalanceCost, 1e-12)
					require.NotNil(t, log.AccountStatsCost)
					require.InDelta(t, 4.82, *log.AccountStatsCost, 1e-12)
					require.Equal(t, 100, log.UpstreamInputTokens)
					require.Equal(t, snapshot.aggregate, log.UpstreamCacheCreationTokens)
					require.Equal(t, snapshot.five, log.UpstreamCacheCreation5mTokens)
					require.Equal(t, snapshot.hour, log.UpstreamCacheCreation1hTokens)
					if mode.name == "unified" {
						require.Equal(t, 200, log.CacheCreationTokens)
						require.Equal(t, 40, log.CacheCreation5mTokens)
						require.Equal(t, 160, log.CacheCreation1hTokens)
					} else if mode.name == "split" {
						require.Zero(t, log.CacheCreationTokens)
						require.Zero(t, log.CacheCreation5mTokens)
						require.Zero(t, log.CacheCreation1hTokens)
					} else {
						require.Equal(t, snapshot.five, log.CacheCreation5mTokens)
						require.Equal(t, snapshot.hour, log.CacheCreation1hTokens)
					}
				})
			}
		}
	}
}
