package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDomesticOfficialCatalogCacheWriteQuotes(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	pricing := &PricingService{}
	pricing.pricingData, err = pricing.parsePricingData(body)
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, pricing)
	plaza := &ModelPlazaService{pricingService: pricing, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	for _, model := range []string{
		"deepseek-flash", "deepseek-v4-pro", "glm-5.3", "glm-5.2", "glm-5", "glm-5.1", "glm-5v-turbo",
		"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.6-pro-ultraspeed",
		"kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k2.6",
		"minimax-m3", "minimax-m3.1-flash", "step-3.7-flash", "qwen-mt-lite",
	} {
		t.Run(model, func(t *testing.T) {
			catalog := pricing.GetModelPricing(model)
			require.NotNil(t, catalog)
			require.True(t, catalog.CacheCreationInputTokenCostExplicit, "official no-charge writes must be explicit")
			quote := plaza.lookupOfficialPricing(context.Background(), model, PlatformOpenAI, map[string]*PlazaOfficialPricing{})
			require.NotNil(t, quote)
			require.Equal(t, "CNY", quote.Currency)
			require.NotNil(t, quote.CacheWritePrice, "public quote must distinguish zero from unavailable")
			require.Zero(t, *quote.CacheWritePrice)
			schedule, err := billing.ResolveContextPricingSchedule(context.Background(), plaza.resolver, ContextPricingScheduleInput{Model: model})
			require.NoError(t, err)
			for _, tier := range schedule.Tiers {
				require.NotNil(t, tier.CacheWrite, "flat and tiered site quotes must retain a catalog's explicit zero")
				require.Zero(t, *tier.CacheWrite)
			}
			for _, tier := range quote.Intervals {
				require.NotNil(t, tier.CacheWritePrice, "free writes must survive context-tier probing")
				require.Zero(t, *tier.CacheWritePrice)
			}
		})
	}
	for model, expected := range map[string][2]float64{
		"kimi-k3.1-pro": {13e-6, 26e-6}, "kimi-k3": {20e-6, 40e-6}, "qwen3.8-max": {15e-6, 0},
	} {
		quote := plaza.lookupOfficialPricing(context.Background(), model, PlatformOpenAI, map[string]*PlazaOfficialPricing{})
		require.NotNil(t, quote, model)
		require.NotNil(t, quote.CacheWritePrice, model)
		require.InDelta(t, expected[0], *quote.CacheWritePrice, 1e-15, model)
		if expected[1] == 0 {
			require.Nil(t, quote.CacheWrite1hPrice, "do not invent a duration absent from the official card")
		} else {
			require.NotNil(t, quote.CacheWrite1hPrice, model)
			require.InDelta(t, expected[1], *quote.CacheWrite1hPrice, 1e-15, model)
		}
	}
	for _, tc := range []struct {
		input int
		price float64
	}{{31999, 4e-6}, {32000, 6e-6}, {32001, 6e-6}} {
		cost, err := billing.CalculateCost("glm-5", UsageTokens{InputTokens: tc.input}, 1)
		require.NoError(t, err)
		require.InDelta(t, float64(tc.input)*tc.price, cost.InputCost, 1e-12, "official >=32K boundary")
	}
	for _, model := range []string{"qwen3-max", "qwen3.8-max-prime", "mimo-v2.5", "minimax-m2.7", "deepseek-v4-pro-0813"} {
		require.NotNil(t, pricing.GetModelPricing(model), "preserve historical billing data")
		quote := plaza.lookupOfficialPricing(context.Background(), model, PlatformOpenAI, map[string]*PlazaOfficialPricing{})
		require.Nil(t, quote, "an unverified or retired first-party quote is not a current official reference: %s", model)
	}
}

func TestDomesticOfficialDeepSeekV41ReferenceQuote(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	pricing := &PricingService{}
	pricing.pricingData, err = pricing.parsePricingData(body)
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, pricing)
	plaza := &ModelPlazaService{pricingService: pricing, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	before := *pricing.GetModelPricing("deepseek-v4.1-flash")
	quote := plaza.lookupOfficialPricing(context.Background(), "deepseek-v4.1-flash", PlatformOpenAI, map[string]*PlazaOfficialPricing{})
	require.NotNil(t, quote, "a public version name must retain its explicitly verified official reference")
	require.Equal(t, "CNY", quote.Currency)
	require.InDelta(t, 2e-6, *quote.InputPrice, 1e-15)
	require.InDelta(t, 8e-6, *quote.OutputPrice, 1e-15)
	require.InDelta(t, .04e-6, *quote.CacheReadPrice, 1e-15)
	require.NotNil(t, quote.CacheWritePrice)
	require.Zero(t, *quote.CacheWritePrice)
	require.Equal(t, before, *pricing.GetModelPricing("deepseek-v4.1-flash"), "official display must not mutate the site's billing catalog")
}

func TestDomesticOfficialPriceAliasRequiresVerifiedCanonicalCard(t *testing.T) {
	for _, status := range []string{"verified", "unconfirmed", "not_listed", "missing"} {
		t.Run(status, func(t *testing.T) {
			pricing := &PricingService{}
			var err error
			pricing.pricingData, err = pricing.parsePricingData([]byte(`{
 "deepseek-flash":{"currency":"CNY","input_cost_per_token":0.000002,"output_cost_per_token":0.000008,"cache_read_input_token_cost":0.00000004,"cache_creation_input_token_cost":0,"deepseek_peak_price":true,"official_api_verification_status":"verified"},
 "deepseek-v4.1-flash":{"currency":"CNY","input_cost_per_token":0.000007,"output_cost_per_token":0.000009,"deepseek_peak_price":true,"official_api_verification_status":"verified_price_alias","official_pricing_model":"deepseek-flash"}
}`))
			require.NoError(t, err)
			if status == "missing" {
				delete(pricing.pricingData, "deepseek-flash")
			} else {
				pricing.pricingData["deepseek-flash"].OfficialAPIVerificationStatus = status
			}
			billing := NewBillingService(&config.Config{}, pricing)
			plaza := &ModelPlazaService{pricingService: pricing, billingService: billing}
			quote := plaza.lookupOfficialPricing(context.Background(), "deepseek-v4.1-flash", PlatformOpenAI, map[string]*PlazaOfficialPricing{})
			if status != "verified" {
				require.Nil(t, quote, "do not resurrect absent or unverified canonical quotes")
				return
			}
			require.NotNil(t, quote)
			require.InDelta(t, 2e-6, *quote.InputPrice, 1e-15, "official view resolves the canonical card, not the alias's billing values")
			require.InDelta(t, 7e-6, pricing.GetModelPricing("deepseek-v4.1-flash").InputCostPerToken, 1e-15)
		})
	}
}
