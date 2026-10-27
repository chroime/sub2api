package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func grokPricingFloat(value float64) *float64 { return &value }

func TestGrokChannelPricingVariantInheritsBaseAndPrefersExactVariant(t *testing.T) {
	base := ChannelModelPricing{
		ID:          10,
		Platform:    PlatformGrok,
		Models:      []string{"grok-4.7"},
		InputPrice:  grokPricingFloat(2e-6),
		OutputPrice: grokPricingFloat(6e-6),
	}
	exact := ChannelModelPricing{
		ID:          11,
		Platform:    PlatformGrok,
		Models:      []string{"grok-4.7-build-fast"},
		InputPrice:  grokPricingFloat(3e-6),
		OutputPrice: grokPricingFloat(7e-6),
	}
	cache := populateChannelCache([]Channel{{
		ID:           1,
		Status:       StatusActive,
		GroupIDs:     []int64{42},
		ModelPricing: []ChannelModelPricing{base, exact},
	}}, map[int64]string{42: PlatformGrok})
	svc := &ChannelService{}
	svc.cache.Store(cache)

	got := svc.GetChannelModelPricing(context.Background(), 42, "grok-4.7-build-fast")
	require.NotNil(t, got)
	require.Equal(t, int64(11), got.ID)

	withoutExact := populateChannelCache([]Channel{{
		ID:           1,
		Status:       StatusActive,
		GroupIDs:     []int64{42},
		ModelPricing: []ChannelModelPricing{base},
	}}, map[int64]string{42: PlatformGrok})
	svc.cache.Store(withoutExact)
	got = svc.GetChannelModelPricing(context.Background(), 42, "grok-4.7-build-fast")
	require.NotNil(t, got)
	require.Equal(t, int64(10), got.ID)
}

func TestGrokGroupPricingVariantInheritsBaseAndPrefersExactVariant(t *testing.T) {
	base := ChannelModelPricing{Models: []string{"grok-4.7"}, InputPrice: grokPricingFloat(2e-6)}
	exact := ChannelModelPricing{Models: []string{"grok-4.7-build-fast"}, InputPrice: grokPricingFloat(3e-6)}

	group := &Group{ModelPricing: []ChannelModelPricing{base, exact}}
	got := matchGroupModelPricing(group, "grok-4.7-build-fast")
	require.NotNil(t, got)
	require.InDelta(t, 3e-6, *got.InputPrice, 1e-12)

	group.ModelPricing = []ChannelModelPricing{base}
	got = matchGroupModelPricing(group, "grok-4.7-build-fast")
	require.NotNil(t, got)
	require.InDelta(t, 2e-6, *got.InputPrice, 1e-12)
}

func TestGrok47BuildFastUsesGrok47GatewayCapabilities(t *testing.T) {
	for _, model := range []string{"grok-4.7-build-fast", "grok-4.7-build-fast-latest"} {
		require.True(t, grokSupportsReasoningEffort(model), model)
		require.True(t, GrokSupportsXHighReasoningEffort(model), model)
		require.True(t, grokChatResponsesRuntimeEligible(model, "cache-id"), model)
	}
}

func TestGrok47BuildFastCustomPricingPrecedesFastFallback(t *testing.T) {
	groupID := int64(42)
	models := []string{"grok-4.7-build-fast", "grok-4.7-build-fast-latest"}
	base := ChannelModelPricing{
		Models: []string{"grok-4.7"}, BillingMode: BillingModeToken,
		InputPrice: grokPricingFloat(2e-6), OutputPrice: grokPricingFloat(6e-6), CacheReadPrice: grokPricingFloat(0.5e-6),
	}
	exact := ChannelModelPricing{
		Models: models, BillingMode: BillingModeToken,
		InputPrice: grokPricingFloat(3e-6), OutputPrice: grokPricingFloat(7e-6), CacheReadPrice: grokPricingFloat(0.25e-6),
	}
	channel := ChannelModelPricing{
		Platform: PlatformGrok, Models: models, BillingMode: BillingModeToken,
		InputPrice: grokPricingFloat(5e-6), OutputPrice: grokPricingFloat(9e-6), CacheReadPrice: grokPricingFloat(0.8e-6),
	}
	for _, tc := range []struct {
		name                     string
		groupPricing             []ChannelModelPricing
		channelPricing           []ChannelModelPricing
		input, output, cacheRead float64
	}{
		{"exact group", []ChannelModelPricing{base, exact}, []ChannelModelPricing{channel}, 3e-6, 7e-6, 0.25e-6},
		{"inherited group", []ChannelModelPricing{base}, []ChannelModelPricing{channel}, 2e-6, 6e-6, 0.5e-6},
		{"channel", nil, []ChannelModelPricing{channel}, 5e-6, 9e-6, 0.8e-6},
		{"fallback", nil, nil, 4e-6, 12e-6, 1e-6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := &BillingService{fallbackPrices: make(map[string]*ModelPricing)}
			billing.initFallbackPricing()
			channels := &ChannelService{}
			channels.cache.Store(populateChannelCache([]Channel{{
				ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: tc.channelPricing,
			}}, map[int64]string{groupID: PlatformGrok}))
			resolver := NewModelPricingResolver(channels, billing)
			for _, model := range []string{
				models[0], models[1], "xai/" + models[0], "x-ai/" + models[1],
			} {
				cost, err := billing.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Model: model, GroupID: &groupID,
					Group: &Group{ID: groupID, ModelPricing: tc.groupPricing}, Resolver: resolver,
					Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 200, CacheReadTokens: 500}, RateMultiplier: 2,
				})
				require.NoError(t, err, model)
				require.InDelta(t, 1000*tc.input+200*tc.output+500*tc.cacheRead, cost.TotalCost, 1e-12, model)
				require.InDelta(t, cost.TotalCost*2, cost.ActualCost, 1e-12, model)
			}
		})
	}
}
