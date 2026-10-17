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
