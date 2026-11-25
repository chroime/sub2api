//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func plazaRuleMap(t *testing.T, model PlazaModel, key string) map[string]any {
	t.Helper()
	body, err := json.Marshal(model)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	rules, ok := fields[key].(map[string]any)
	require.True(t, ok, "model must expose %s", key)
	return rules
}

func TestPlazaPricingRules_OfficialPeakAndCustomSiteRemainSeparate(t *testing.T) {
	prices := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"deepseek-v4-pro": {Currency: "CNY", InputCostPerToken: 9e-6, OutputCostPerToken: 27e-6, CacheReadInputTokenCost: .3e-6, DeepSeekPeakPrice: true},
	})
	svc := newPlazaService([]Channel{plazaPricedChannel(1, "custom", []int64{10}, PlatformDeepseek, "deepseek-v4-pro")}, []Group{{ID: 10, Platform: PlatformDeepseek, RateMultiplier: 1}}, prices)
	svc.billingService = NewBillingService(&config.Config{}, prices)
	channel := plazaPricedChannel(1, "custom", []int64{10}, PlatformDeepseek, "deepseek-v4-pro")
	svc.resolver = NewModelPricingResolver(newTestChannelServiceForStats(t, &channel, 10, PlatformDeepseek), svc.billingService)
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	model := groups[0].Models[0]
	official := plazaRuleMap(t, model, "OfficialPricingRules")
	require.Equal(t, "CNY", official["currency"])
	require.Equal(t, "flat", official["context_status"])
	require.Equal(t, "peak_offpeak", official["time_status"])
	require.Equal(t, "peak", official["price_basis"])
	timeRule := official["time_rules"].([]any)[0].(map[string]any)
	require.Equal(t, .5, timeRule["base_multiplier"])
	require.Equal(t, "Asia/Shanghai", timeRule["timezone"])
	require.Equal(t, true, timeRule["weekdays_only"])
	require.Len(t, timeRule["periods"], 2)
	site := plazaRuleMap(t, model, "PricingRules")
	require.Equal(t, official["evaluated_at"], site["evaluated_at"])
	require.Equal(t, "uniform", site["time_status"], "custom flat site prices do not inherit the vendor peak rule")
}

func TestPlazaPricingRules_ContextScopeFollowsGroupToggle(t *testing.T) {
	prices := &PricingService{}
	data, err := prices.parsePricingData([]byte(domesticThreeTierJSON))
	require.NoError(t, err)
	prices.pricingData = data
	for _, enabled := range []bool{false, true} {
		ch := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{10}, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"qwen-tier-test"}}}}
		svc := newPlazaService([]Channel{ch}, []Group{{ID: 10, Platform: PlatformOpenAI, LongContextPricingEnabled: enabled, RateMultiplier: 1}}, prices)
		svc.billingService = NewBillingService(&config.Config{}, prices)
		svc.resolver = NewModelPricingResolver(newTestChannelServiceForStats(t, &ch, 10, PlatformOpenAI), svc.billingService)
		groups, err := svc.ListGroups(context.Background())
		require.NoError(t, err)
		model := groups[0].Models[0]
		official := plazaRuleMap(t, model, "OfficialPricingRules")
		require.EqualValues(t, 3, official["context_tier_count"])
		site := plazaRuleMap(t, model, "PricingRules")
		if enabled {
			require.Equal(t, "tiered", site["context_status"])
			require.EqualValues(t, 3, site["context_tier_count"])
		} else {
			require.Equal(t, "flat", site["context_status"])
			require.EqualValues(t, 1, site["context_tier_count"])
		}
	}
}

func TestPlazaPricingRules_MissingOfficialIsUnknown(t *testing.T) {
	svc := newPlazaService([]Channel{plazaPricedChannel(1, "custom", []int64{10}, PlatformOpenAI, "unlisted-model")}, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil)
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	rules := plazaRuleMap(t, groups[0].Models[0], "OfficialPricingRules")
	require.Empty(t, rules["currency"])
	require.Equal(t, "unknown", rules["context_status"])
	require.Equal(t, "unknown", rules["time_status"])
}

func TestPlazaPricingRules_OfficialFallbackKeepsListedPriceBasis(t *testing.T) {
	prices := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"deepseek-v4-pro": {Currency: "CNY", InputCostPerToken: 9e-6, OutputCostPerToken: 27e-6, CacheReadInputTokenCost: .3e-6, DeepSeekPeakPrice: true},
	})
	svc := newPlazaServiceWithBilling(nil, []Group{{ID: 10, Platform: PlatformDeepseek, RateMultiplier: 1}}, map[int64]string{10: PlatformDeepseek}, prices)
	groups, err := svc.ListGroupsWithAccountModels(context.Background(), func(context.Context, *Group, []PlazaAccountModel) []PlazaAccountModel {
		return []PlazaAccountModel{{Name: "deepseek-v4-pro", Platform: PlatformDeepseek}}
	})
	require.NoError(t, err)
	model := groups[0].Models[0]
	require.InDelta(t, 9e-6, *model.Pricing.InputPrice, 1e-15, "listed peak quote cannot silently become an off-peak quote at night")
	require.Equal(t, "peak", plazaRuleMap(t, model, "PricingRules")["price_basis"])
}

func TestPlazaPricingRules_MediaAndPartialOverrides(t *testing.T) {
	prices := &PricingService{}
	data, err := prices.parsePricingData([]byte(`{"deepseek-v4-pro":{"currency":"CNY","deepseek_peak_price":true,"input_cost_per_token":9e-6,"output_cost_per_token":27e-6}}`))
	require.NoError(t, err)
	prices.pricingData = data
	billing := NewBillingService(&config.Config{}, prices)
	base, err := billing.GetModelPricing("deepseek-v4-pro")
	require.NoError(t, err)
	base.priceOverrides.input = true
	rule := deepseekPlazaTimeRule("deepseek-v4-pro", base)
	require.NotContains(t, rule.Fields, "input_price")
	require.Contains(t, rule.Fields, "output_price")
	rules := &PlazaPricingRules{TimeRules: []PlazaPricingTimeRule{*rule}}
	evaluatePlazaTimeRules(rules, nil, time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC))
	require.Equal(t, 1.0, rules.CurrentMultipliers["input_price"])
	require.Equal(t, .5, rules.CurrentMultipliers["output_price"])
	require.Equal(t, 1.0, rules.CurrentMultipliers["cache_write_12h_price"])
	media := plazaRules("USD", BillingModeImage, []PricingInterval{{PerRequestPrice: testPtrFloat64(.1)}, {PerRequestPrice: testPtrFloat64(.2)}}, true)
	require.Equal(t, "not_applicable", media.ContextStatus)
	require.Zero(t, media.ContextTierCount)
}

func TestPlazaPricingRules_OffPeakBasisWithoutOfficialReference(t *testing.T) {
	prices := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"deepseek-chat": {Currency: "USD", OfficialCurrency: "USD", InputCostPerToken: .28e-6, OutputCostPerToken: .42e-6},
	})
	svc := newPlazaServiceWithBilling(nil, []Group{{ID: 10, Platform: PlatformDeepseek, RateMultiplier: 1}}, map[int64]string{10: PlatformDeepseek}, prices)
	groups, err := svc.ListGroupsWithAccountModels(context.Background(), func(context.Context, *Group, []PlazaAccountModel) []PlazaAccountModel {
		return []PlazaAccountModel{{Name: "deepseek-chat", Platform: PlatformDeepseek}}
	})
	require.NoError(t, err)
	model := groups[0].Models[0]
	require.Nil(t, model.OfficialPricing)
	require.Equal(t, "peak_offpeak", model.PricingRules.TimeStatus)
	require.Equal(t, "off_peak", model.PricingRules.PriceBasis)
}

func TestPlazaPricingRules_CurrentSnapshotCombinesChannelAndGroup(t *testing.T) {
	svc := &ModelPlazaService{}
	model := PlazaModel{Name: "snapshot", PricingSource: PlazaPricingSourceChannel,
		Pricing:     &ChannelModelPricing{InputPrice: testPtrFloat64(9e-6)},
		TimePricing: &TimePricingSchedule{Timezone: "Asia/Shanghai", Periods: []TimePricingPeriod{{StartTime: "00:00", EndTime: "23:59", Multiplier: .5}}},
	}
	g := &Group{SubscriptionType: SubscriptionTypeSubscription, PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: 2}
	svc.fillPricingRules(context.Background(), &model, g, time.Date(2026, time.November, 24, 10, 0, 0, 0, time.UTC))
	rules := plazaRuleMap(t, model, "PricingRules")
	stamp, ok := rules["evaluated_at"].(string)
	require.True(t, ok, "time rules must share a captured evaluation timestamp")
	at, err := time.Parse(time.RFC3339Nano, stamp)
	require.NoError(t, err)
	factors, ok := rules["current_multipliers"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, .5*g.PeakMultiplierAt(at), factors["input_price"])
	for _, item := range rules["time_rules"].([]any) {
		rule := item.(map[string]any)
		if rule["source"] == "channel" {
			require.Equal(t, .5, rule["current_multiplier"])
		} else {
			require.Equal(t, g.PeakMultiplierAt(at), rule["current_multiplier"])
		}
	}
	g.PeakRateMultiplier = 0
	svc.fillPricingRules(context.Background(), &model, g, at)
	require.Zero(t, model.PricingRules.CurrentMultipliers["input_price"])
	require.Zero(t, model.PricingRules.CurrentMultipliers["cache_write_12h_price"])
}
