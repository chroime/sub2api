package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotGroupUltraFastRoundTrip(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		key := &APIKey{User: &User{ID: 1, Status: StatusActive}, Group: &Group{
			ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: enabled,
		}}
		svc := &APIKeyService{}
		raw, err := json.Marshal(svc.snapshotFromAPIKey(context.Background(), key))
		require.NoError(t, err)
		var stored APIKeyAuthSnapshot
		require.NoError(t, json.Unmarshal(raw, &stored))
		restored, used, err := svc.applyAuthCacheEntry("isolated-ultrafast", &APIKeyAuthCacheEntry{Snapshot: &stored})
		require.NoError(t, err)
		require.True(t, used)
		require.True(t, restored.Group.Hydrated)
		require.Equal(t, enabled, restored.Group.ForceOpenAIUltrafast)
	}
}

func TestAPIKeyAuthSnapshotBeforeUltraFastMustReload(t *testing.T) {
	var old APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal([]byte(`{"version":33,"api_key_id":11,"user_id":1,"group_id":7,"status":"active","user":{"id":1,"status":"active"},"group":{"id":7,"platform":"openai","status":"active","force_openai_fast":false}}`), &old))
	svc := &APIKeyService{}
	restored, used, err := svc.applyAuthCacheEntry("isolated-old-ultrafast", &APIKeyAuthCacheEntry{Snapshot: &old})
	require.NoError(t, err)
	require.False(t, used, "old snapshots lack UltraFast; the repository must be consulted after enabling it")
	require.Nil(t, restored)
}
