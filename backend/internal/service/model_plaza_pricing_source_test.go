//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func plazaSourceModelsByName(t *testing.T, groups []PlazaGroup) map[string]PlazaModel {
	t.Helper()
	require.Len(t, groups, 1)
	out := make(map[string]PlazaModel, len(groups[0].Models))
	for _, m := range groups[0].Models {
		out[m.Name] = m
	}
	return out
}

func TestListPlazaGroups_PricingSourceOfficialByDefault(t *testing.T) {
	// 渠道只映射模型、未配置价格：展示官方目录价，来源标记为 official。
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-sonnet": {Mode: "chat", InputCostPerToken: 3e-6, OutputCostPerToken: 1.5e-5},
	})
	ch := Channel{
		ID: 1, Name: "mapping-only", Status: StatusActive, GroupIDs: []int64{10},
		ModelMapping: map[string]map[string]string{"anthropic": {"claude-sonnet": "claude-sonnet"}},
	}
	svc := newPlazaService([]Channel{ch}, []Group{{ID: 10, Platform: "anthropic", RateMultiplier: 1}}, pricingSvc)
	out, err := svc.ListGroups(context.Background())
	require.NoError(t, err)

	m := plazaSourceModelsByName(t, out)["claude-sonnet"]
	require.Equal(t, PlazaPricingSourceOfficial, m.PricingSource)
	require.NotNil(t, m.Pricing)
	require.InDelta(t, 3e-6, *m.Pricing.InputPrice, 1e-12)
}

func TestListPlazaGroups_PricingSourceChannelOverridesOfficial(t *testing.T) {
	// 渠道显式定价覆盖官方价：即使先遍历到只有官方回落价的渠道，也升级为渠道价。
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-sonnet": {Mode: "chat", InputCostPerToken: 9e-6, OutputCostPerToken: 4.5e-5},
	})
	unpriced := Channel{
		ID: 1, Name: "alpha", Status: StatusActive, GroupIDs: []int64{10},
		ModelMapping: map[string]map[string]string{"anthropic": {"claude-sonnet": "claude-sonnet"}},
	}
	priced := plazaPricedChannel(2, "beta", []int64{10}, "anthropic", "claude-sonnet")
	svc := newPlazaService([]Channel{unpriced, priced}, []Group{{ID: 10, Platform: "anthropic", RateMultiplier: 1}}, pricingSvc)
	out, err := svc.ListGroups(context.Background())
	require.NoError(t, err)

	m := plazaSourceModelsByName(t, out)["claude-sonnet"]
	require.Equal(t, PlazaPricingSourceChannel, m.PricingSource)
	require.InDelta(t, 3e-6, *m.Pricing.InputPrice, 1e-12, "应使用渠道定价而非官方价")
}

func TestListPlazaGroups_PricingSourceGroupWins(t *testing.T) {
	// 分组模型级定价是实收价，优先于渠道定价。
	ch := plazaPricedChannel(1, "ch", []int64{10}, "anthropic", "claude-sonnet")
	group := Group{
		ID: 10, Platform: "anthropic", RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"claude-sonnet"}, BillingMode: BillingModeToken,
			InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(5e-6),
		}},
	}
	svc := newPlazaService([]Channel{ch}, []Group{group}, nil)
	out, err := svc.ListGroups(context.Background())
	require.NoError(t, err)

	m := plazaSourceModelsByName(t, out)["claude-sonnet"]
	require.Equal(t, PlazaPricingSourceGroup, m.PricingSource)
	require.InDelta(t, 1e-6, *m.Pricing.InputPrice, 1e-12)
}

func staticPlazaAccountModels(byGroup map[int64][]PlazaAccountModel) PlazaAccountModelLister {
	return func(_ context.Context, g *Group) []PlazaAccountModel { return byGroup[g.ID] }
}

func TestListGroupsWithAccountModels_MergesAccountOnlyModelsAtOfficialPrice(t *testing.T) {
	// 渠道有 claude-sonnet（渠道价）；账号额外暴露 claude-opus（仅官方价）与目录查不到的模型。
	pricingSvc := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"claude-sonnet": {Mode: "chat", InputCostPerToken: 9e-6, OutputCostPerToken: 4.5e-5},
		"claude-opus":   {Mode: "chat", InputCostPerToken: 1.5e-5, OutputCostPerToken: 7.5e-5},
	})
	ch := plazaPricedChannel(1, "ch", []int64{10}, "anthropic", "claude-sonnet")
	svc := newPlazaService([]Channel{ch}, []Group{{ID: 10, Platform: "anthropic", RateMultiplier: 1}}, pricingSvc)
	lister := staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {
		{Name: "claude-sonnet", Platform: "anthropic"},
		{Name: "claude-opus", Platform: "anthropic"},
		{Name: "internal-unpriced", Platform: "anthropic"},
	}})

	out, err := svc.ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	byName := plazaSourceModelsByName(t, out)
	require.Len(t, byName, 3)

	sonnet := byName["claude-sonnet"]
	require.Equal(t, PlazaPricingSourceChannel, sonnet.PricingSource, "渠道已有的模型保持渠道价")
	require.InDelta(t, 3e-6, *sonnet.Pricing.InputPrice, 1e-12)

	opus := byName["claude-opus"]
	require.Equal(t, PlazaPricingSourceOfficial, opus.PricingSource)
	require.InDelta(t, 1.5e-5, *opus.Pricing.InputPrice, 1e-12)

	unpriced := byName["internal-unpriced"]
	require.Empty(t, unpriced.PricingSource)
	require.Nil(t, unpriced.Pricing)
}

func TestListGroupsWithAccountModels_KeepsChannelModelsWhenAccountsMissing(t *testing.T) {
	// 分组下账号丢失（列不出任何模型）时，渠道配置的模型仍照常展示。
	ch := plazaPricedChannel(1, "ch", []int64{10}, "anthropic", "claude-sonnet", "claude-haiku")
	svc := newPlazaService([]Channel{ch}, []Group{{ID: 10, Name: "no-accounts", Platform: "anthropic", RateMultiplier: 1}}, nil)
	lister := staticPlazaAccountModels(nil)

	out, err := svc.ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 2)
	require.Equal(t, PlazaPricingSourceChannel, out[0].Models[0].PricingSource)
}

func TestListGroupsWithAccountModels_GroupWithoutChannelIsReturned(t *testing.T) {
	groups := []Group{{ID: 2, Name: "OpenAI", Platform: "openai", RateMultiplier: 1}}
	svc := newPlazaService(nil, groups, nil)
	lister := staticPlazaAccountModels(map[int64][]PlazaAccountModel{2: {{Name: "gpt-6-sol", Platform: "openai"}}})

	out, err := svc.ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, int64(2), out[0].ID)
	require.Equal(t, "gpt-6-sol", out[0].Models[0].Name)
}

func TestListGroupsWithAccountModels_PlatformIsolationAndWildcards(t *testing.T) {
	groups := []Group{
		{ID: 10, Name: "anthropic-only", Platform: "anthropic", RateMultiplier: 1},
		{ID: 20, Name: "composite", Platform: PlatformComposite, RateMultiplier: 1},
	}
	svc := newPlazaService(nil, groups, nil)
	lister := staticPlazaAccountModels(map[int64][]PlazaAccountModel{
		10: {
			{Name: "claude-sonnet", Platform: "anthropic"},
			{Name: "gpt-5", Platform: "openai"},       // 平台不符，丢弃
			{Name: "claude-*", Platform: "anthropic"}, // 通配映射键，丢弃
		},
		20: {
			{Name: "claude-sonnet", Platform: "anthropic"},
			{Name: "gpt-5", Platform: "openai"},
		},
	})

	out, err := svc.ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	require.Len(t, out, 2)
	byGroup := map[string][]string{}
	for _, g := range out {
		for _, m := range g.Models {
			byGroup[g.Name] = append(byGroup[g.Name], m.Name)
		}
	}
	require.Equal(t, []string{"claude-sonnet"}, byGroup["anthropic-only"])
	require.Equal(t, []string{"claude-sonnet", "gpt-5"}, byGroup["composite"])
}

func TestListGroupsWithAccountModels_NilListerMatchesListGroups(t *testing.T) {
	ch := plazaPricedChannel(1, "ch", []int64{10}, "anthropic", "claude-sonnet")
	groups := []Group{{ID: 10, Platform: "anthropic", RateMultiplier: 1}, {ID: 2, Platform: "openai", RateMultiplier: 1}}
	svc := newPlazaService([]Channel{ch}, groups, nil)

	withNil, err := svc.ListGroupsWithAccountModels(context.Background(), nil)
	require.NoError(t, err)
	plain, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	require.Equal(t, plain, withNil)
	require.Len(t, plain, 1, "无渠道、无账号模型的分组仍不返回")
}
