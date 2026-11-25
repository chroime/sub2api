package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIsDomesticPricingModelName_HunyuanAliasesAreBounded(t *testing.T) {
	for _, model := range []string{"hy3", "hy3-preview", "hy4-preview", "hy4-preview-2026"} {
		require.True(t, isDomesticPricingModelName(model), model)
	}
	for _, model := range []string{"hyperbolic/claude-sonnet-4", "hybrid-llm", "hyperscale-model"} {
		require.False(t, isDomesticPricingModelName(model), model)
	}
}

func TestDomesticCatalog_CNYFallbackWinsRemoteUSDAndPreservesOperatorOverride(t *testing.T) {
	for _, override := range []string{"", `{"glm-5.3":{"input_cost_per_token":9e-6}}`} {
		t.Run(override, func(t *testing.T) {
			dir := t.TempDir()
			fallback := filepath.Join(dir, "fallback.json")
			require.NoError(t, os.WriteFile(fallback, []byte(`{
				"glm-5.3":{"currency":"CNY","input_cost_per_token":8e-6,"output_cost_per_token":28e-6,"cache_read_input_token_cost":2e-6},
				"gpt-test":{"input_cost_per_token":4e-6,"output_cost_per_token":8e-6}
			}`), 0600))
			svc := &PricingService{cfg: &config.Config{}}
			svc.cfg.Pricing.FallbackFile = fallback
			if override != "" {
				svc.cfg.Pricing.OverrideFile = filepath.Join(dir, "override.json")
				require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(override), 0600))
			}
			data, _, err := svc.buildPricingData([]byte(`{
				"glm-5.3":{"input_cost_per_token":1e-6,"output_cost_per_token":4e-6,"max_input_tokens":1000000,"input_cost_per_token_priority":99e-6},
				"gpt-test":{"input_cost_per_token":2e-6,"output_cost_per_token":3e-6}
			}`))
			require.NoError(t, err)
			wantInput := 8e-6
			if override != "" {
				wantInput = 9e-6
			}
			require.InDelta(t, wantInput, data["glm-5.3"].InputCostPerToken, 1e-15)
			require.InDelta(t, 28e-6, data["glm-5.3"].OutputCostPerToken, 1e-15)
			require.InDelta(t, 2e-6, data["glm-5.3"].CacheReadInputTokenCost, 1e-15)
			require.Zero(t, data["glm-5.3"].InputCostPerTokenPriority, "must not mix old remote USD priority prices into a CNY card")
			require.Equal(t, "CNY", data["glm-5.3"].Currency)
			require.Empty(t, data["glm-5.3"].OfficialCurrency)
			require.NotNil(t, data["glm-5.3"].MaxInputTokens)
			require.EqualValues(t, 1000000, *data["glm-5.3"].MaxInputTokens)
			require.InDelta(t, 2e-6, data["gpt-test"].InputCostPerToken, 1e-15)
		})
	}
}

const domesticThreeTierJSON = `{"qwen-tier-test":{
	"currency":"CNY","input_cost_per_token":0.2e-6,"output_cost_per_token":2e-6,
	"context_pricing_tiers":[
		{"min_tokens":0,"max_tokens":128000,"input_price":0.2e-6,"output_price":2e-6},
		{"min_tokens":128000,"max_tokens":256000,"input_price":0.8e-6,"output_price":8e-6},
		{"min_tokens":256000,"input_price":1.2e-6,"output_price":12e-6}
	]
}}`

func TestDomesticCatalog_AllContextTiersReachBillingAndSchedule(t *testing.T) {
	svc := &PricingService{}
	data, err := svc.parsePricingData([]byte(domesticThreeTierJSON))
	require.NoError(t, err)
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)
	resolver := NewModelPricingResolver(nil, billing)
	for _, tc := range []struct {
		tokens int
		input  float64
	}{
		{128000, 0.2e-6}, {128001, 0.8e-6}, {256000, 0.8e-6}, {256001, 1.2e-6},
	} {
		usage := UsageTokens{InputTokens: tc.tokens, OutputTokens: 1000}
		cost, err := billing.CalculateCost("qwen-tier-test", usage, 1)
		require.NoError(t, err)
		require.InDelta(t, float64(tc.tokens)*tc.input, cost.InputCost, 1e-12)
		unified, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "qwen-tier-test", Tokens: usage, RateMultiplier: 1, Resolver: resolver})
		require.NoError(t, err)
		require.InDelta(t, cost.TotalCost, unified.TotalCost, 1e-12)
	}
	schedule, err := billing.ResolveContextPricingSchedule(context.Background(), resolver, ContextPricingScheduleInput{Model: "qwen-tier-test"})
	require.NoError(t, err)
	require.Len(t, schedule.Tiers, 3)
	require.InDelta(t, 1.2e-6, *schedule.Tiers[2].Input, 1e-15)
	group := &Group{ID: 123, LongContextPricingEnabled: false}
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "qwen-tier-test", Group: group, Tokens: UsageTokens{InputTokens: 300000}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, 300000*0.2e-6, cost.TotalCost, 1e-12)
}

func TestDomesticCatalog_OperatorFlatPriceDoesNotInheritAbsoluteCatalogTiers(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"qwen-tier-test":{"input_cost_per_token":7e-6}}`)
	data, err := svc.parsePricingData([]byte(domesticThreeTierJSON))
	require.NoError(t, err)
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)
	cost, err := billing.CalculateCost("qwen-tier-test", UsageTokens{InputTokens: 300000}, 1)
	require.NoError(t, err)
	require.InDelta(t, 2.1, cost.InputCost, 1e-12)
}

func TestDomesticCatalog_GroupFlatPriceWinsCatalogTiers(t *testing.T) {
	svc := &PricingService{}
	data, err := svc.parsePricingData([]byte(domesticThreeTierJSON))
	require.NoError(t, err)
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)
	resolver := NewModelPricingResolver(nil, billing)
	input := 7e-6
	group := &Group{LongContextPricingEnabled: true, ModelPricing: []ChannelModelPricing{{
		Models: []string{"qwen-tier-test"}, InputPrice: &input,
	}}}
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "qwen-tier-test",
		Group: group, Tokens: UsageTokens{InputTokens: 300000}, RateMultiplier: 1, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, 2.1, cost.InputCost, 1e-12)
}

func TestDomesticCatalog_DeepSeekUSDRemoteAndFallbackUseExactCNY(t *testing.T) {
	for _, body := range []string{
		`{"deepseek-flash":{"input_cost_per_token":0.00001,"output_cost_per_token":0.00002}}`,
		`{"other-model":{"input_cost_per_token":1e-6}}`,
	} {
		svc := newPricingServiceWithOverride(t, `{"deepseek-flash":{"output_cost_per_token":0}}`)
		data, err := svc.parsePricingData([]byte(body))
		require.NoError(t, err)
		svc.pricingData = data
		billing := NewBillingService(&config.Config{}, svc)
		resolver := NewModelPricingResolver(nil, billing)
		pricing, err := billing.GetModelPricing("deepseek-flash")
		require.NoError(t, err)
		require.Equal(t, "CNY", pricing.Currency)
		require.InDelta(t, 1e-6, pricing.InputPricePerToken, 1e-15)
		if data["deepseek-flash"] != nil {
			require.Zero(t, pricing.OutputPricePerToken)
		} else {
			require.InDelta(t, 4e-6, pricing.OutputPricePerToken, 1e-15)
		}
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-flash",
			Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1, Resolver: resolver,
			PricingAt: time.Date(2026, 11, 23, 2, 0, 0, 0, time.UTC)})
		require.NoError(t, err)
		require.InDelta(t, 0.002, cost.InputCost, 1e-12)
	}
}

func TestDomesticCatalog_SnapshotContainsNativeCNYPrices(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	var raw map[string]map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	svc := &PricingService{}
	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	for model, input := range map[string]float64{
		"deepseek-v4-pro": 9e-6, "kimi-k3.1-pro": 13e-6, "kimi-k3": 20e-6, "glm-5.3": 8e-6,
		"minimax-m3": 2.1e-6, "mimo-v2.6-pro": 3e-6, "qwen3.8-max": 12e-6,
		"doubao-seed-2.1-pro": 6e-6, "ernie-5.1": 4e-6, "hy4-preview": 6e-6,
		"spark-x2.5": 1.6e-6, "step-3.7-flash": 1.35e-6, "baichuan-m3-plus": 5e-6,
	} {
		require.Equal(t, "CNY", raw[model]["currency"], model)
		require.NotNil(t, data[model], model)
		require.Empty(t, data[model].OfficialCurrency, model)
		require.InDelta(t, input, data[model].InputCostPerToken, 1e-15, model)
	}
	require.InDelta(t, 100e-6, data["kimi-k3"].OutputCostPerToken, 1e-15)
	require.InDelta(t, 2e-6, data["kimi-k3"].CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 0.025e-6, data["mimo-v2.6-pro"].CacheReadInputTokenCost, 1e-15, "keep the source's full precision")
	require.NotContains(t, raw, "kimi-k2.7", "never substitute Code pricing for an unverified model ID")
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)
	cost, err := billing.CalculateCost("qwen3.5-flash", UsageTokens{InputTokens: 300000}, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.36, cost.InputCost, 1e-12, "third tier must not silently use the first or second tier")
}

func TestDomesticCatalog_DeepSeekReferencePriceAndTimeBilling(t *testing.T) {
	body := []byte(`{"deepseek-v4-pro":{"currency":"CNY","input_cost_per_token":9e-6,"output_cost_per_token":27e-6,"cache_read_input_token_cost":0.3e-6,"deepseek_peak_price":true}}`)
	for _, override := range []string{`{}`, `{"deepseek-v4-pro":{"output_cost_per_token":7e-6}}`} {
		svc := newPricingServiceWithOverride(t, override)
		data, err := svc.parsePricingData(body)
		require.NoError(t, err)
		svc.pricingData = data
		billing := NewBillingService(&config.Config{}, svc)
		resolver := NewModelPricingResolver(nil, billing)
		plaza := &ModelPlazaService{pricingService: svc, billingService: billing, resolver: resolver}
		official := plaza.lookupOfficialPricing(context.Background(), "deepseek-v4-pro", PlatformDeepseek, map[string]*PlazaOfficialPricing{})
		require.NotNil(t, official)
		require.InDelta(t, 9e-6, *official.InputPrice, 1e-15, "official quote must match the source website")
		require.InDelta(t, 0.3e-6, *official.CacheReadPrice, 1e-15)
		for _, slot := range []struct {
			at     time.Time
			factor float64
		}{
			{time.Date(2026, 11, 23, 2, 0, 0, 0, time.UTC), 1},
			{time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC), 0.5},
			{time.Date(2026, 11, 22, 2, 0, 0, 0, time.UTC), 0.5},
		} {
			usage := UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000}
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: usage,
				RateMultiplier: 1, Resolver: resolver, PricingAt: slot.at})
			require.NoError(t, err)
			require.InDelta(t, 0.009*slot.factor, cost.InputCost, 1e-12, "peak quote must not be doubled again")
			require.InDelta(t, 0.0003*slot.factor, cost.CacheReadCost, 1e-12)
			output := 0.027 * slot.factor
			if override != `{}` {
				output = 0.007
			}
			require.InDelta(t, output, cost.OutputCost, 1e-12, "operator override stays final in every time window")
			groupInput := 2e-6
			group := &Group{ModelPricing: []ChannelModelPricing{{Models: []string{"deepseek-v4-pro"}, InputPrice: &groupInput}}}
			groupCost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-v4-pro", Group: group,
				Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1, Resolver: resolver, PricingAt: slot.at})
			require.NoError(t, err)
			require.InDelta(t, 0.002, groupCost.InputCost, 1e-12)
		}
	}
}
