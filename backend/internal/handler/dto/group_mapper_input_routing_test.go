package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperInputRoutingAdminOnlyAndIsolation(t *testing.T) {
	group := &service.Group{ID: 7, InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{ID: "small", RequestType: "stream", InputTokensMode: "range", MinInputTokens: 50, MaxInputTokens: 1000, AccountIDs: []int64{11}, GroupIDs: []int64{21}}}}}
	admin := GroupFromServiceAdmin(group)
	raw, err := json.Marshal(admin)
	require.NoError(t, err)
	var payload struct {
		InputRouting service.GroupInputRoutingConfig `json:"input_routing"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, group.InputRouting, payload.InputRouting)
	require.Equal(t, 50, payload.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, 1000, payload.InputRouting.Rules[0].MaxInputTokens)
	admin.InputRouting.Rules[0].MinInputTokens = 75
	admin.InputRouting.Rules[0].AccountIDs[0] = 99
	require.Equal(t, 50, group.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, int64(11), group.InputRouting.Rules[0].AccountIDs[0])
	raw, err = json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "input_routing")
}
