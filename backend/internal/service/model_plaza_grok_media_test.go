//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireMediaTiers asserts tier labels and per-unit prices in order: label, price, label, price, ...
func requireMediaTiers(t *testing.T, intervals []PricingInterval, want ...any) {
	t.Helper()
	require.Len(t, intervals, len(want)/2)
	for i := range intervals {
		require.Equal(t, want[2*i], intervals[i].TierLabel)
		require.NotNil(t, intervals[i].PerRequestPrice, intervals[i].TierLabel)
		require.InDelta(t, want[2*i+1], *intervals[i].PerRequestPrice, 1e-12, intervals[i].TierLabel)
	}
}

func grokMediaPlazaGroups(t *testing.T, channels []Channel, groups []Group, models []PlazaAccountModel) map[int64]map[string]PlazaModel {
	t.Helper()
	platforms := make(map[int64]string, len(groups))
	byGroup := make(map[int64][]PlazaAccountModel, len(groups))
	for _, g := range groups {
		platforms[g.ID] = g.Platform
		byGroup[g.ID] = models
	}
	svc := newPlazaServiceWithBilling(channels, groups, platforms, newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{}))
	out, err := svc.ListGroupsWithAccountModels(context.Background(), staticPlazaAccountModels(byGroup))
	require.NoError(t, err)
	result := make(map[int64]map[string]PlazaModel, len(out))
	for _, g := range out {
		result[g.ID] = plazaModelsByName(g.Models)
	}
	return result
}

// Regression: Grok Imagine image/video models were billed at the built-in xAI
// prices, but the plaza showed them with no price at all.
func TestListGroups_GrokImagineMediaShowsBuiltInPrices(t *testing.T) {
	groups := []Group{
		{ID: 10, Name: "grok", Platform: PlatformGrok, RateMultiplier: 1},
		{ID: 20, Name: "grok-custom", Platform: PlatformGrok, RateMultiplier: 1,
			ImagePrice1K: testPtrFloat64(0.03), VideoPrice720P: testPtrFloat64(0.2)},
	}
	models := []PlazaAccountModel{
		{Name: "grok-imagine-image-2.0", Platform: PlatformGrok},
		{Name: "grok-imagine-image", Platform: PlatformGrok},
		{Name: "grok-imagine-video-1.5", Platform: PlatformGrok},
		{Name: "grok-imagine-video", Platform: PlatformGrok},
		{Name: "grok-voice-tts", Platform: PlatformGrok},
	}
	byGroup := grokMediaPlazaGroups(t, nil, groups, models)
	require.Len(t, byGroup, 2)

	std := byGroup[10]
	image := std["grok-imagine-image-2.0"]
	require.NotNil(t, image.Pricing)
	require.Equal(t, BillingModeImage, image.Pricing.BillingMode)
	require.Equal(t, PlazaPricingSourceOfficial, image.PricingSource)
	requireMediaTiers(t, image.Pricing.Intervals, "1K", 0.06, "2K", 0.08)
	require.NotNil(t, image.OfficialPricing)
	require.Equal(t, BillingModeImage, image.OfficialPricing.BillingMode)
	requireMediaTiers(t, image.OfficialPricing.Intervals, "1K", 0.06, "2K", 0.08)

	flat := std["grok-imagine-image"]
	require.Empty(t, flat.Pricing.Intervals, "same price for every size collapses to one")
	require.InDelta(t, 0.02, *flat.Pricing.PerRequestPrice, 1e-12)
	require.InDelta(t, 0.02, *flat.OfficialPricing.PerRequestPrice, 1e-12)

	video15 := std["grok-imagine-video-1.5"]
	require.Equal(t, BillingModeVideo, video15.Pricing.BillingMode)
	requireMediaTiers(t, video15.Pricing.Intervals, "480P", 0.08, "720P", 0.14, "1080P", 0.25)
	require.Equal(t, BillingModeVideo, video15.OfficialPricing.BillingMode)

	video := std["grok-imagine-video"]
	requireMediaTiers(t, video.Pricing.Intervals, "480P", 0.05, "720P", 0.07)

	voice := std["grok-voice-tts"]
	require.Nil(t, voice.Pricing, "models without any price stay unpriced")
	require.Nil(t, voice.OfficialPricing)
	require.Empty(t, voice.PricingSource)

	custom := byGroup[20]
	customImage := custom["grok-imagine-image-2.0"]
	require.Equal(t, PlazaPricingSourceGroup, customImage.PricingSource)
	requireMediaTiers(t, customImage.Pricing.Intervals, "1K", 0.03, "2K", 0.08)
	requireMediaTiers(t, customImage.OfficialPricing.Intervals, "1K", 0.06, "2K", 0.08)

	customVideo := custom["grok-imagine-video-1.5"]
	require.Equal(t, PlazaPricingSourceGroup, customVideo.PricingSource)
	requireMediaTiers(t, customVideo.Pricing.Intervals, "480P", 0.08, "720P", 0.2, "1080P", 0.25)
}

func TestListGroups_GrokImagineChannelPriceWinsOverBuiltInPrice(t *testing.T) {
	groups := []Group{{ID: 10, Name: "grok", Platform: PlatformGrok, RateMultiplier: 1}}
	channels := []Channel{{
		ID: 1, Name: "grok-media", Status: StatusActive, GroupIDs: []int64{10},
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformGrok, Models: []string{"grok-imagine-image-2.0"},
			BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.5),
		}},
	}}
	byGroup := grokMediaPlazaGroups(t, channels, groups, nil)

	image := byGroup[10]["grok-imagine-image-2.0"]
	require.Equal(t, PlazaPricingSourceChannel, image.PricingSource)
	require.InDelta(t, 0.5, *image.Pricing.PerRequestPrice, 1e-12)
	require.Empty(t, image.Pricing.Intervals)
	requireMediaTiers(t, image.OfficialPricing.Intervals, "1K", 0.06, "2K", 0.08)
}
