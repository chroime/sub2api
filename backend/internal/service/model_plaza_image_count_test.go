//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListGroups_GPTImageCountBillingOverridesTokenDisplay(t *testing.T) {
	models := []string{"gpt-image-2", "gpt-image-2.5", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst", "gpt-image-2.6"}
	for _, fromChannel := range []bool{false, true} {
		t.Run(map[bool]string{false: "catalog", true: "channel"}[fromChannel], func(t *testing.T) {
			group := Group{ID: 8, Name: "GPT Image", Platform: PlatformOpenAI,
				RateMultiplier: 0.03, ImageCountBilling: true, ImageRateIndependent: true, ImageRateMultiplier: 1,
				ImagePrice1K: testPtrFloat64(0.03), ImagePrice2K: testPtrFloat64(0.03), ImagePrice4K: testPtrFloat64(0.03)}
			catalog := make(map[string]*LiteLLMModelPricing)
			accountModels := make([]PlazaAccountModel, 0, len(models))
			for _, name := range models {
				catalog[name] = &LiteLLMModelPricing{InputCostPerToken: 5e-6, CacheReadInputTokenCost: 1.25e-6}
				accountModels = append(accountModels, PlazaAccountModel{Name: name, Platform: PlatformOpenAI})
			}
			var channels []Channel
			if fromChannel {
				channels = []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{8}, ModelPricing: []ChannelModelPricing{{
					Platform: PlatformOpenAI, Models: models, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(5e-6),
				}}}}
			}
			svc := newPlazaServiceWithBilling(channels, []Group{group}, map[int64]string{8: PlatformOpenAI}, newStubPricingServiceFromMap(catalog))
			out, err := svc.ListGroupsWithAccountModels(context.Background(), staticPlazaAccountModels(map[int64][]PlazaAccountModel{8: accountModels}))
			require.NoError(t, err)
			require.Len(t, out, 1)
			for _, displayed := range [][]PlazaModel{out[0].Models, out[0].CatalogModels} {
				require.Len(t, displayed, len(models))
				for _, model := range displayed {
					require.NotNil(t, model.Pricing, model.Name)
					require.Equal(t, BillingModeImage, model.Pricing.BillingMode, model.Name)
					require.Equal(t, PlazaPricingSourceGroup, model.PricingSource)
					require.NotNil(t, model.Pricing.PerRequestPrice)
					require.InDelta(t, 0.03, *model.Pricing.PerRequestPrice, 1e-12)
					require.Empty(t, model.Pricing.Intervals, "uniform size prices collapse into one per-image price")
					require.Nil(t, model.Pricing.InputPrice)
					require.Equal(t, "not_applicable", model.PricingRules.ContextStatus)
					require.NotNil(t, model.OfficialPricing)
					require.InDelta(t, 5e-6, *model.OfficialPricing.InputPrice, 1e-12, "official token quotes retain their original units")
				}
			}
		})
	}
}

func TestPlazaGPTImageCountPricingRespectsScopeAndSizes(t *testing.T) {
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	for _, tc := range []struct {
		name    string
		model   string
		enabled bool
	}{
		{"switch_off", "gpt-image-2.6", false},
		{"text_model", "gpt-6.2-sol", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ImageCountBilling: tc.enabled, ImagePrice1K: testPtrFloat64(0.03)}
			model := PlazaModel{Name: tc.model, Platform: PlatformOpenAI, Pricing: &ChannelModelPricing{BillingMode: BillingModeToken}}
			svc.fillDisplayPricing(context.Background(), &model, group)
			require.NotEqual(t, BillingModeImage, model.Pricing.BillingMode)
		})
	}
	group := &Group{ImageCountBilling: true, ImagePrice1K: testPtrFloat64(0), ImagePrice2K: testPtrFloat64(0.03), ImagePrice4K: testPtrFloat64(0.06)}
	model := PlazaModel{Name: "gpt-image-2.6", Platform: PlatformOpenAI}
	svc.fillDisplayPricing(context.Background(), &model, group)
	require.NotNil(t, model.Pricing)
	require.Equal(t, BillingModeImage, model.Pricing.BillingMode)
	requireMediaTiers(t, model.Pricing.Intervals, "1K", float64(0), "2K", 0.03, "4K", 0.06)

	group.ModelPricing = []ChannelModelPricing{{Models: []string{"gpt-image-*"}, BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.5)}}
	model = PlazaModel{Name: "gpt-image-2.6", Platform: PlatformOpenAI}
	svc.fillDisplayPricing(context.Background(), &model, group)
	require.Equal(t, PlazaPricingSourceGroup, model.PricingSource)
	require.InDelta(t, 0.5, *model.Pricing.PerRequestPrice, 1e-12, "explicit group model cards retain priority")
}
