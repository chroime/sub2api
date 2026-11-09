//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListGroupsWithAccountModels_KeepsAllActiveGroupOptionsAfterModelFiltering(t *testing.T) {
	channel := plazaPricedChannel(1, "prices", []int64{10, 20}, PlatformOpenAI, "gpt-6.1-sol")
	groups := []Group{
		{ID: 10, Name: "has-account", Platform: PlatformOpenAI, SortOrder: 2},
		{ID: 20, Name: "empty-priced-group", Platform: PlatformOpenAI, SortOrder: 1},
		{ID: 30, Name: "empty-unpriced-group", Platform: PlatformAnthropic, SortOrder: 3},
	}
	out, err := newPlazaService([]Channel{channel}, groups, nil).ListGroupsWithAccountModels(context.Background(),
		staticPlazaAccountModels(map[int64][]PlazaAccountModel{10: {{Name: "gpt-6.1-sol", Platform: PlatformOpenAI}}}))
	require.NoError(t, err)
	require.Len(t, out, 3, "model availability must not remove otherwise-visible group choices")
	require.Equal(t, []int64{20, 10, 30}, []int64{out[0].ID, out[1].ID, out[2].ID})
	require.Empty(t, out[0].Models, "a configured price must not invent account support")
	require.Len(t, out[1].Models, 1)
	require.Equal(t, "gpt-6.1-sol", out[1].Models[0].Name)
	require.Empty(t, out[2].Models)
}
