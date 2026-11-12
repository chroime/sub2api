//go:build unit

package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPublicGroupProjectionsKeepIdentityAndVisibility(t *testing.T) {
	var plaza service.PlazaGroup
	require.NoError(t, json.Unmarshal([]byte(`{"id":7,"name":"group","identity":{"label":"自营","color":"teal"}}`), &plaza))
	var ref service.AvailableGroupRef
	require.NoError(t, json.Unmarshal([]byte(`{"id":7,"name":"group","identity":{"label":"自营","color":"teal"}}`), &ref))
	filtered := filterUserVisibleGroups([]service.AvailableGroupRef{ref}, map[int64]struct{}{7: {}})
	require.Len(t, filtered, 1)
	for _, value := range []any{toModelPlazaGroupDTO(&plaza, nil), filtered[0]} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(raw, &fields))
		require.Equal(t, map[string]any{"label": "自营", "color": "teal"}, fields["identity"])
		for _, private := range []string{"input_routing", "profit_min_margin", "model_routing", "account_ids"} {
			require.NotContains(t, fields, private)
		}
	}
	require.Empty(t, filterUserVisibleGroups([]service.AvailableGroupRef{ref}, map[int64]struct{}{}))
}
