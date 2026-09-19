package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperExposesLegacyStreamingACKOnlyToAdmins(t *testing.T) {
	group := &service.Group{ID: 7, Name: "legacy", Platform: service.PlatformAnthropic}

	userJSON, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "streaming_ack_enabled")

	adminJSON, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), `"streaming_ack_enabled":null`)
}

func TestGroupMapperStreamingACKPreservesExplicitBoolean(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		group := &service.Group{StreamingACKEnabled: &enabled}
		encoded, err := json.Marshal(GroupFromServiceAdmin(group))
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.Equal(t, enabled, fields["streaming_ack_enabled"])
	}
}
