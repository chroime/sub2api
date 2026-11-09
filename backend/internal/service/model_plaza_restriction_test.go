//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlazaRestriction_AccountCatalogCannotBypassConfiguredKimiModels(t *testing.T) {
	channel := plazaPricedChannel(12, "domestic", []int64{85}, PlatformOpenAI, "kimi-k3-1", "kimi-k3.1-pro")
	channel.RestrictModels = true
	groups := []Group{{ID: 85, Platform: PlatformOpenAI, RateMultiplier: 0.5}}
	svc := newPlazaServiceWithBilling([]Channel{channel}, groups, map[int64]string{85: PlatformOpenAI}, nil)
	fallback, err := svc.billingService.GetModelPricing("kimi-k3")
	require.NoError(t, err)
	require.Equal(t, 3e-6, fallback.InputPricePerToken, "an official fallback exists but does not authorize the model")

	out, err := svc.ListGroupsWithAccountModels(context.Background(), staticPlazaAccountModels(map[int64][]PlazaAccountModel{
		85: {{Name: "kimi-k3", Platform: PlatformOpenAI}, {Name: "kimi-k3-1", Platform: PlatformOpenAI}, {Name: "kimi-k3.1-pro", Platform: PlatformOpenAI}},
	}))
	require.NoError(t, err)
	models := plazaSourceModelsByName(t, out)
	require.NotContains(t, models, "kimi-k3")
	require.Len(t, models, 2)
	require.Equal(t, PlazaPricingSourceChannel, models["kimi-k3-1"].PricingSource)
	require.Equal(t, PlazaPricingSourceChannel, models["kimi-k3.1-pro"].PricingSource)
}

func TestPlazaRestriction_UsesGatewayBillingModelAndFiltersMappingOnlyEntries(t *testing.T) {
	for _, tt := range []struct {
		source string
		want   []string
	}{
		{BillingModelSourceRequested, []string{"allowed-model"}},
		{BillingModelSourceChannelMapped, []string{"alias", "alias-preview", "allowed-model"}},
		{BillingModelSourceResponse, []string{"alias", "alias-preview", "allowed-model"}},
		// Upstream restriction requires per-account mappings, which a plaza model does not carry.
		{BillingModelSourceUpstream, []string{"alias", "alias-preview", "allowed-model", "kimi-k3"}},
	} {
		t.Run(tt.source, func(t *testing.T) {
			channel := plazaPricedChannel(1, "channel", []int64{10}, PlatformOpenAI, "allowed-model")
			channel.RestrictModels = true
			channel.BillingModelSource = tt.source
			channel.ModelMapping = map[string]map[string]string{PlatformOpenAI: {
				"alias": "allowed-model", "alias-*": "allowed-model", "kimi-k3": "kimi-k3",
			}}
			catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
				"kimi-k3": {InputCostPerToken: 3e-6, OutputCostPerToken: 15e-6},
			})
			svc := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI}}, catalog)
			out, err := svc.ListGroupsWithAccountModels(context.Background(), staticPlazaAccountModels(map[int64][]PlazaAccountModel{
				10: {{Name: "allowed-model", Platform: PlatformOpenAI}, {Name: "alias", Platform: PlatformOpenAI}, {Name: "alias-preview", Platform: PlatformOpenAI}, {Name: "kimi-k3", Platform: PlatformOpenAI}},
			}))
			require.NoError(t, err)
			require.Len(t, out, 1)
			names := make([]string, 0, len(out[0].Models))
			for _, model := range out[0].Models {
				names = append(names, model.Name)
			}
			require.Equal(t, tt.want, names)
		})
	}
}

func TestPlazaRestriction_PreservesGatewayNormalizationAndCompositePlatformIsolation(t *testing.T) {
	channel := Channel{ID: 1, Name: "multi", Status: StatusActive, GroupIDs: []int64{10, 20}, RestrictModels: true,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformAnthropic, Models: []string{"CLAUDE-SONNET-4-5", "shared"}, InputPrice: testPtrFloat64(3e-6)},
			{Platform: PlatformOpenAI, Models: []string{"KIMI-*"}, InputPrice: testPtrFloat64(2e-6)},
		},
	}
	groups := []Group{{ID: 10, Platform: PlatformOpenAI}, {ID: 20, Platform: PlatformComposite}}
	lister := staticPlazaAccountModels(map[int64][]PlazaAccountModel{
		10: {{Name: "KiMi-k3", Platform: PlatformOpenAI}, {Name: "shared", Platform: PlatformOpenAI}},
		20: {{Name: "CLAUDE-SONNET-4-5", Platform: PlatformAnthropic}, {Name: "Claude-Sonnet-4.5", Platform: PlatformAnthropic}, {Name: "shared", Platform: PlatformAnthropic}, {Name: "shared", Platform: PlatformOpenAI}, {Name: "KiMi-k3", Platform: PlatformOpenAI}},
	})
	out, err := newPlazaService([]Channel{channel}, groups, nil).ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	require.Len(t, out, 2)
	byGroup := make(map[int64][]string)
	for _, group := range out {
		for _, model := range group.Models {
			byGroup[group.ID] = append(byGroup[group.ID], model.Platform+":"+model.Name)
		}
	}
	require.Equal(t, []string{"openai:KiMi-k3"}, byGroup[10])
	require.ElementsMatch(t, []string{"anthropic:CLAUDE-SONNET-4-5", "anthropic:Claude-Sonnet-4.5", "anthropic:shared", "openai:KiMi-k3"}, byGroup[20])
}

func TestPlazaRestriction_EffectiveChannelUsesRepositoryOrderBeforeDisplaySorting(t *testing.T) {
	for _, tt := range []struct {
		name         string
		lastStatus   string
		lastRestrict bool
		wantKimi     bool
	}{
		{"last active restricts", StatusActive, true, false},
		{"last active allows", StatusActive, false, true},
		{"last inactive allows", StatusDisabled, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first := plazaPricedChannel(1, "z-first", []int64{10}, PlatformOpenAI, "configured-model")
			first.RestrictModels = !tt.lastRestrict
			last := plazaPricedChannel(2, "a-last", []int64{10}, PlatformOpenAI, "configured-model")
			last.Status, last.RestrictModels = tt.lastStatus, tt.lastRestrict
			out, err := newPlazaService([]Channel{first, last}, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil).
				ListGroupsWithAccountModels(context.Background(), staticPlazaAccountModels(map[int64][]PlazaAccountModel{
					10: {{Name: "configured-model", Platform: PlatformOpenAI}, {Name: "kimi-k3", Platform: PlatformOpenAI}},
				}))
			require.NoError(t, err)
			_, exists := plazaSourceModelsByName(t, out)["kimi-k3"]
			require.Equal(t, tt.wantKimi, exists)
		})
	}
}

func TestPlazaRestriction_GroupPriceDoesNotOverrideGatewayRestriction(t *testing.T) {
	channel := plazaPricedChannel(1, "restricted", []int64{10}, PlatformOpenAI, "configured-model")
	channel.RestrictModels = true
	group := Group{ID: 10, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{
		{Models: []string{"kimi-k3"}, InputPrice: testPtrFloat64(1e-6)},
	}}
	out, err := newPlazaService([]Channel{channel}, []Group{group}, nil).ListGroupsWithAccountModels(
		context.Background(), staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "configured-model", Platform: PlatformOpenAI}, {Name: "kimi-k3", Platform: PlatformOpenAI}}}))
	require.NoError(t, err)
	require.NotContains(t, plazaSourceModelsByName(t, out), "kimi-k3")
}

func TestPlazaRestriction_OmitsGroupWhenAllModelsAreRestricted(t *testing.T) {
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{10}, RestrictModels: true,
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"kimi-k3": "kimi-k3"}},
	}
	out, err := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil).ListGroups(context.Background())
	require.NoError(t, err)
	require.Empty(t, out)
}
