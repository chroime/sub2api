package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestQwenOfficialCatalogListPricesAndBilling(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	var raw map[string]map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	svc := &PricingService{}
	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	for model, want := range map[string][3]float64{
		"qwen4-max":     {10e-6, 30e-6, 0.5e-6},
		"qwen3.8-max":   {12e-6, 36e-6, 1.5e-6},
		"qwen3.8-flash": {0.8e-6, 2.7e-6, 0.1e-6},
		"qwen3.7-plus":  {2e-6, 8e-6, 0.4e-6},
		"qwen3.7-max":   {12e-6, 36e-6, 2.4e-6},
		"qwen3.5-flash": {0.2e-6, 2e-6, 0.02e-6},
	} {
		require.NotNil(t, data[model], model)
		require.Equal(t, "CNY", data[model].Currency, model)
		require.Empty(t, data[model].OfficialCurrency, model)
		require.InDelta(t, want[0], data[model].InputCostPerToken, 1e-15, model)
		require.InDelta(t, want[1], data[model].OutputCostPerToken, 1e-15, model)
		require.InDelta(t, want[2], data[model].CacheReadInputTokenCost, 1e-15, model)
		require.Equal(t, "https://platform.qianwenai.com/pricing/api", raw[model]["pricing_reference"], model)
		require.Equal(t, "list_before_discount", raw[model]["pricing_basis"], model)
	}
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)
	for _, tc := range []struct {
		input int
		price float64
	}{
		{256000, 2e-6}, {256001, 6e-6}, {300000, 6e-6},
	} {
		cost, err := billing.CalculateCost("qwen3.7-plus", UsageTokens{InputTokens: tc.input}, 1)
		require.NoError(t, err)
		require.InDelta(t, float64(tc.input)*tc.price, cost.InputCost, 1e-12, "whole-request undiscounted context tier")
	}
	require.Len(t, data["qwen3.7-flash"].ContextPricingTiers, 3)
	require.Len(t, data["qwen3-coder-flash"].ContextPricingTiers, 4)
	require.InDelta(t, 0.12e-6, *data["qwen3.5-flash"].ContextPricingTiers[2].CacheReadPrice, 1e-15)
}
