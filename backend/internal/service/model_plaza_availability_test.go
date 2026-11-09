//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlazaAvailability_ChannelPricesDoNotAdvertiseModelsWithoutGroupAccounts(t *testing.T) {
	channel := plazaPricedChannel(1, "shared-prices", []int64{10, 20}, PlatformOpenAI, "gpt-6.1-sol", "kimi-k3", "deepseek-v4.1-flash")
	groups := []Group{{ID: 10, Platform: PlatformOpenAI}, {ID: 20, Platform: PlatformOpenAI}}
	out, err := newPlazaService([]Channel{channel}, groups, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{
			10: {{Name: "gpt-6.1-sol", Platform: PlatformOpenAI}},
			20: {{Name: "deepseek-v4.1-flash", Platform: PlatformOpenAI}},
		}))
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Len(t, out[0].Models, 1, "a shared price table is not evidence that this group's accounts serve every model")
	require.Equal(t, "gpt-6.1-sol", out[0].Models[0].Name)
	require.Equal(t, PlazaPricingSourceChannel, out[0].Models[0].PricingSource)
	require.Len(t, out[1].Models, 1)
	require.Equal(t, "deepseek-v4.1-flash", out[1].Models[0].Name)
}

func TestPlazaAvailability_ChannelAliasUsesMappedAccountModelAndPublicAllowlist(t *testing.T) {
	channel := plazaPricedChannel(1, "aliases", []int64{10}, PlatformOpenAI, "public-alias", "unavailable-alias", "upstream-model")
	channel.ModelMapping = map[string]map[string]string{PlatformOpenAI: {"public-alias": "upstream-model", "unavailable-alias": "missing-model"}}
	group := Group{ID: 10, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-alias", "unavailable-alias"}}}
	out, err := newPlazaService([]Channel{channel}, []Group{group}, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "public-alias", Platform: PlatformOpenAI}, {Name: "upstream-model", Platform: PlatformOpenAI}}}))
	require.NoError(t, err)
	models := plazaSourceModelsByName(t, out)
	require.Len(t, models, 1)
	require.Contains(t, models, "public-alias", "the channel maps the public ID to an account-served model before scheduling")
	require.Equal(t, PlazaPricingSourceChannel, models["public-alias"].PricingSource)
}

func TestPlazaAvailability_AccountSupportIsScopedByPlatform(t *testing.T) {
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{10}, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformOpenAI, Models: []string{"gpt-6.1-sol", "claude-sonnet-5-5", "grok-4.7"}, InputPrice: testPtrFloat64(2e-6)},
		{Platform: PlatformAnthropic, Models: []string{"gpt-6.1-sol", "claude-sonnet-5-5"}, InputPrice: testPtrFloat64(3e-6)},
	}}
	out, err := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformComposite}}, nil).
		ListGroupsWithAccountModels(context.Background(), staticPlazaAccountModels(map[int64][]PlazaAccountModel{
			10: {{Name: "gpt-6.1-sol", Platform: PlatformOpenAI}, {Name: "claude-sonnet-5-5", Platform: PlatformAnthropic}},
		}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	names := make([]string, 0, len(out[0].Models))
	for _, model := range out[0].Models {
		names = append(names, model.Platform+":"+model.Name)
	}
	require.Equal(t, []string{"anthropic:claude-sonnet-5-5", "openai:gpt-6.1-sol"}, names)
}

func TestPlazaAvailability_GroupAllowlistAlsoFiltersChannelPricedModels(t *testing.T) {
	channel := plazaPricedChannel(1, "prices", []int64{10}, PlatformOpenAI, "gpt-6.1-sol", "gpt-6.1-terra")
	group := Group{ID: 10, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6.1-sol"}}}
	out, err := newPlazaService([]Channel{channel}, []Group{group}, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "gpt-6.1-sol", Platform: PlatformOpenAI}, {Name: "gpt-6.1-terra", Platform: PlatformOpenAI}}}))
	require.NoError(t, err)
	models := plazaSourceModelsByName(t, out)
	require.Len(t, models, 1)
	require.Contains(t, models, "gpt-6.1-sol")
}

func TestPlazaAvailability_QueriesAccountSupportForPublicChannelCandidates(t *testing.T) {
	channel := plazaPricedChannel(1, "mapped", []int64{10}, PlatformOpenAI, "public-alias")
	channel.ModelMapping = map[string]map[string]string{PlatformOpenAI: {"public-alias": "upstream-model"}}
	lister := func(_ context.Context, _ *Group, candidates []PlazaAccountModel) []PlazaAccountModel {
		for _, candidate := range candidates {
			if candidate.Name == "public-alias" && candidate.Platform == PlatformOpenAI {
				return []PlazaAccountModel{candidate}
			}
		}
		return nil
	}
	out, err := newPlazaService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil).
		ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	models := plazaSourceModelsByName(t, out)
	require.Contains(t, models, "public-alias", "the lister verifies the actual request path for the candidate public ID")
}

func TestPlazaAvailability_VerifiesConcreteGroupAllowlistCandidates(t *testing.T) {
	group := Group{ID: 10, Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6.1-sol", "unavailable-model"}}}
	lister := func(_ context.Context, _ *Group, candidates []PlazaAccountModel) []PlazaAccountModel {
		for _, candidate := range candidates {
			if candidate.Name == "gpt-6.1-sol" && candidate.Platform == PlatformOpenAI {
				return []PlazaAccountModel{candidate}
			}
		}
		return nil
	}
	out, err := newPlazaService(nil, []Group{group}, nil).ListGroupsWithAccountModels(context.Background(), lister)
	require.NoError(t, err)
	models := plazaSourceModelsByName(t, out)
	require.Len(t, models, 1)
	require.Contains(t, models, "gpt-6.1-sol")
}
