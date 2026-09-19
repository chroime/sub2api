package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotGroupStreamingACKRoundtrip(t *testing.T) {
	for _, tc := range []struct {
		name     string
		policy   *bool
		wantJSON string
	}{
		{name: "legacy", wantJSON: "null"},
		{name: "enabled", policy: testPtrBool(true), wantJSON: "true"},
		{name: "disabled", policy: testPtrBool(false), wantJSON: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(51)
			apiKey := &APIKey{
				ID: 83, UserID: 41, GroupID: &groupID, Key: "sk-streaming-ack", Status: StatusActive,
				User: &User{ID: 41, Status: StatusActive},
				Group: &Group{
					ID: groupID, Platform: PlatformOpenAI, Status: StatusActive,
					StreamingACKEnabled: tc.policy,
				},
			}
			svc := &APIKeyService{}
			payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
			require.NoError(t, err)

			var raw struct {
				Snapshot struct {
					Group map[string]json.RawMessage `json:"group"`
				} `json:"snapshot"`
			}
			require.NoError(t, json.Unmarshal(payload, &raw))
			require.Equal(t, tc.wantJSON, string(raw.Snapshot.Group["streaming_ack_enabled"]), "cache JSON must distinguish legacy, enabled and disabled policies")

			var cached APIKeyAuthCacheEntry
			require.NoError(t, json.Unmarshal(payload, &cached))
			materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
			require.NoError(t, err)
			require.True(t, used)
			require.NotNil(t, materialized.Group)
			require.True(t, materialized.Group.Hydrated)
			require.Equal(t, tc.policy, materialized.Group.StreamingACKEnabled)
		})
	}
}

func TestAPIKeyAuthSnapshotGroupStreamingACKIsolatesPointers(t *testing.T) {
	for _, policy := range []bool{true, false} {
		name := "disabled"
		if policy {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			apiKey := &APIKey{
				User:  &User{ID: 41, Status: StatusActive},
				Group: &Group{ID: 51, StreamingACKEnabled: testPtrBool(policy)},
			}
			svc := &APIKeyService{}
			entry := &APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)}

			// Repository object changes must not alter an existing L1 cache entry.
			*apiKey.Group.StreamingACKEnabled = !policy
			first, used, err := svc.applyAuthCacheEntry("first-request", entry)
			require.NoError(t, err)
			require.True(t, used)
			require.NotNil(t, first.Group.StreamingACKEnabled)
			require.Equal(t, policy, *first.Group.StreamingACKEnabled)

			second, used, err := svc.applyAuthCacheEntry("second-request", entry)
			require.NoError(t, err)
			require.True(t, used)
			require.NotNil(t, second.Group.StreamingACKEnabled)

			// A hydrated request must not share policy state with the cache or another request.
			*first.Group.StreamingACKEnabled = !policy
			require.Equal(t, policy, *second.Group.StreamingACKEnabled)
			third, used, err := svc.applyAuthCacheEntry("third-request", entry)
			require.NoError(t, err)
			require.True(t, used)
			require.NotNil(t, third.Group.StreamingACKEnabled)
			require.Equal(t, policy, *third.Group.StreamingACKEnabled)
		})
	}
}

func TestAPIKeyAuthSnapshotWithoutGroupStreamingACKIsRejected(t *testing.T) {
	svc := &APIKeyService{}
	apiKey, used, err := svc.applyAuthCacheEntry("old-group-streaming-ack", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 26},
	})

	require.NoError(t, err)
	require.False(t, used, "snapshots without group ACK policy must be loaded from the repository")
	require.Nil(t, apiKey)
}
