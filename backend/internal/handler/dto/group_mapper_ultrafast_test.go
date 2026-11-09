package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperUltraFastIsAdminOnly(t *testing.T) {
	group := &service.Group{ID: 7, Platform: service.PlatformOpenAI, ForceOpenAIUltrafast: true}
	userJSON, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "force_openai_ultrafast")
	adminJSON, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(adminJSON, &fields))
	require.Equal(t, true, fields["force_openai_ultrafast"])
	group.ForceOpenAIUltrafast = false
	adminJSON, err = json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(adminJSON, &fields))
	require.Equal(t, false, fields["force_openai_ultrafast"])
}
