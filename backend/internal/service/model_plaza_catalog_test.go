//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlazaCatalog_AllGroupsPreservesPricedModelsWhileSelectedGroupUsesAccounts(t *testing.T) {
	channels := []Channel{
		plazaPricedChannel(1, "openai", []int64{10}, PlatformOpenAI, "gpt-6.1-sol", "kimi-k2.7"),
		plazaPricedChannel(2, "claude", []int64{20}, PlatformAnthropic, "claude-sonnet-5-5"),
	}
	groups := []Group{{ID: 10, Platform: PlatformOpenAI}, {ID: 20, Platform: PlatformAnthropic}}
	out, err := newPlazaService(channels, groups, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "gpt-6.1-sol", Platform: PlatformOpenAI}}}))
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, "gpt-6.1-sol", out[0].Models[0].Name)
	require.Len(t, out[0].CatalogModels, 2, "all-groups catalog must retain configured prices even without account support")
	require.Equal(t, []string{"gpt-6.1-sol", "kimi-k2.7"}, []string{out[0].CatalogModels[0].Name, out[0].CatalogModels[1].Name})
	require.Equal(t, out[0].Models[0], out[0].CatalogModels[0], "catalog and actual models share the same calculated price")
	require.Empty(t, out[1].Models)
	require.Len(t, out[1].CatalogModels, 1)
	require.Equal(t, "claude-sonnet-5-5", out[1].CatalogModels[0].Name)
	require.Equal(t, PlazaPricingSourceChannel, out[1].CatalogModels[0].PricingSource)
}

func TestPlazaCatalog_StillHonorsConfiguredChannelAndGroupRestrictions(t *testing.T) {
	channel := plazaPricedChannel(1, "domestic", []int64{10}, PlatformOpenAI, "kimi-k2.7", "kimi-k3-1")
	channel.RestrictModels = true
	group := Group{ID: 10, Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"kimi-k3*"}}}
	out, err := newPlazaService([]Channel{channel}, []Group{group}, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "kimi-k3", Platform: PlatformOpenAI}}}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Empty(t, out[0].Models, "an unpriced restricted account model must not appear in a selected group")
	require.Len(t, out[0].CatalogModels, 1)
	require.Equal(t, "kimi-k3-1", out[0].CatalogModels[0].Name)
}

func TestPlazaCatalog_PreservesVerifiedCompositeRouteAliasesUnderChannelRestrictions(t *testing.T) {
	channel := plazaPricedChannel(1, "routed", []int64{10}, PlatformOpenAI, "gpt-6.1-sol")
	channel.RestrictModels = true
	group := Group{ID: 10, Platform: PlatformComposite}
	// The account lister has already resolved the public route and verified the
	// concrete upstream model against this channel's restrictions.
	out, err := newPlazaService([]Channel{channel}, []Group{group}, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "public-alias", Platform: PlatformOpenAI, ChannelRestrictionVerified: true}}}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, "public-alias", out[0].Models[0].Name)
	require.Contains(t, plazaModelsByName(out[0].CatalogModels), "public-alias")
}
