//go:build unit

package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPlazaCatalogDTO_PreservesEmptyActualListAndSanitizesBothModelLists(t *testing.T) {
	price := 2e-6
	g := service.PlazaGroup{ID: 1, Name: "catalog-only", Platform: service.PlatformOpenAI,
		CatalogModels: []service.PlazaModel{{Name: "gpt-6.1-sol", Platform: service.PlatformOpenAI,
			PricingSource: service.PlazaPricingSourceChannel,
			Pricing:       &service.ChannelModelPricing{ID: 42, ChannelID: 99, Models: []string{"gpt-6.1-sol"}, InputPrice: &price},
		}}}
	body, err := json.Marshal(toModelPlazaGroupDTO(&g, nil))
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, []any{}, decoded["models"])
	catalog := decoded["catalog_models"].([]any)
	require.Len(t, catalog, 1)
	model := catalog[0].(map[string]any)
	require.Equal(t, "gpt-6.1-sol", model["name"])
	require.Equal(t, "channel", model["pricing_source"])
	pricing := model["pricing"].(map[string]any)
	require.Equal(t, price, pricing["input_price"])
	require.NotContains(t, pricing, "id")
	require.NotContains(t, pricing, "channel_id")
	require.NotContains(t, pricing, "models")
}

func TestPlazaCatalogDTO_HiddenGroupsDoNotExposeTheirCatalog(t *testing.T) {
	groups := []service.PlazaGroup{
		{ID: 1, Name: "public", CatalogModels: []service.PlazaModel{{Name: "public-model"}}},
		{ID: 2, Name: "private", IsExclusive: true, CatalogModels: []service.PlazaModel{{Name: "private-model"}}},
		{ID: 3, Name: "ungranted-public", CatalogModels: []service.PlazaModel{{Name: "ungranted-model"}}},
	}
	visible := filterPlazaVisibleGroups(groups, map[int64]struct{}{1: {}}, true)
	body, err := json.Marshal(toLocalizedModelPlazaGroups(visible, nil, "zh-CN"))
	require.NoError(t, err)
	require.Contains(t, string(body), "public-model")
	require.NotContains(t, string(body), "private-model")
	require.NotContains(t, string(body), "ungranted-model")
}
