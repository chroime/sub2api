package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotGroupInputRoutingRoundTripAndIsolation(t *testing.T) {
	config := GroupInputRoutingConfig{Enabled: true, Rules: []GroupInputRoutingRule{{ID: "small", Name: "Small", Enabled: true, RequestType: "sync", MinInputTokens: 50, MaxInputTokens: 1000, AccountIDs: []int64{11}, GroupIDs: []int64{21}, FallbackToOriginal: true}}}
	apiKey := &APIKey{User: &User{ID: 1, Status: StatusActive}, Group: &Group{ID: 7, InputRouting: config}}
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	apiKey.Group.InputRouting.Rules[0].AccountIDs[0] = 99
	require.Equal(t, int64(11), snapshot.Group.InputRouting.Rules[0].AccountIDs[0])
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var stored APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.Equal(t, snapshot.Group.InputRouting, stored.Group.InputRouting)
	entry := &APIKeyAuthCacheEntry{Snapshot: &stored}
	first, used, err := svc.applyAuthCacheEntry("first", entry)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, int64(7), first.Group.ID, "routing must not change the billing group")
	require.Equal(t, 50, first.Group.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, 1000, first.Group.InputRouting.Rules[0].MaxInputTokens)
	first.Group.InputRouting.Rules[0].MinInputTokens = 75
	first.Group.InputRouting.Rules[0].AccountIDs[0] = 98
	first.Group.InputRouting.Rules[0].GroupIDs[0] = 98
	second, used, err := svc.applyAuthCacheEntry("second", entry)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, 50, second.Group.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, 1000, second.Group.InputRouting.Rules[0].MaxInputTokens)
	require.Equal(t, []int64{11}, second.Group.InputRouting.Rules[0].AccountIDs)
	require.Equal(t, []int64{21}, second.Group.InputRouting.Rules[0].GroupIDs)
	_, used, err = svc.applyAuthCacheEntry("old", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 32}})
	require.NoError(t, err)
	require.False(t, used, "snapshots without routing must reload the repository")
}

func TestAPIKeyAuthSnapshotGroupInputRoutingLegacyThresholdConvertsOnce(t *testing.T) {
	// This is a pre-range v33 cache payload. Building it from the current Go
	// struct would emit min_input_tokens and miss the legacy decode path.
	const legacy = `{
		"version":33,"api_key_id":11,"user_id":1,"group_id":7,"status":"active",
		"user":{"id":1,"status":"active"},
		"group":{"id":7,"platform":"openai","status":"active","input_routing":{
			"enabled":true,"rules":[{
				"id":"legacy","name":"Small","enabled":true,"request_type":"all",
				"max_input_tokens":100,"account_ids":[21],"group_ids":[],"fallback_to_original":false
			}]
		}}
	}`
	var stored APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal([]byte(legacy), &stored))
	require.NotNil(t, stored.Group)
	require.Len(t, stored.Group.InputRouting.Rules, 1)
	require.Equal(t, 0, stored.Group.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, 99, stored.Group.InputRouting.Rules[0].MaxInputTokens)
	svc := &APIKeyService{}
	key := svc.snapshotToAPIKey("fixture-key", &stored)
	for round := range 2 {
		for _, check := range []struct {
			tokens int
			match  bool
		}{
			{99, true}, {100, false},
		} {
			ctx := WithGroupInputRoutingRequest(context.Background(), key.Group, check.tokens, false)
			_, matched := GroupInputRoutingDecisionFromContext(ctx)
			require.Equal(t, check.match, matched, "restored round %d, input %d", round, check.tokens)
		}

		encoded, err := json.Marshal(svc.snapshotFromAPIKey(context.Background(), key))
		require.NoError(t, err)
		var reloaded APIKeyAuthSnapshot
		require.NoError(t, json.Unmarshal(encoded, &reloaded))
		var used bool
		key, used, err = svc.applyAuthCacheEntry("fixture-key", &APIKeyAuthCacheEntry{Snapshot: &reloaded})
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, int64(7), key.Group.ID)
		require.Equal(t, 0, key.Group.InputRouting.Rules[0].MinInputTokens)
		require.Equal(t, 99, key.Group.InputRouting.Rules[0].MaxInputTokens, "cache rewrites must not reduce the legacy threshold again")
	}
}

func TestAPIKeyAuthSnapshotGroupInputRoutingAllModeKeepsUnrestrictedMatching(t *testing.T) {
	config := GroupInputRoutingConfig{Enabled: true, Rules: []GroupInputRoutingRule{{
		ID: "all-lengths", Enabled: true, RequestType: "sync", InputTokensMode: "all", MinInputTokens: 500, MaxInputTokens: 600, AccountIDs: []int64{21},
	}}}
	svc := &APIKeyService{}
	key := &APIKey{User: &User{ID: 1, Status: StatusActive}, Group: &Group{ID: 7, InputRouting: config}}
	raw, err := json.Marshal(svc.snapshotFromAPIKey(context.Background(), key))
	require.NoError(t, err)
	var snapshot APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(raw, &snapshot))
	restored, used, err := svc.applyAuthCacheEntry("fixture-key", &APIKeyAuthCacheEntry{Snapshot: &snapshot})
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, "all", restored.Group.InputRouting.Rules[0].InputTokensMode)
	require.Equal(t, 500, restored.Group.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, 600, restored.Group.InputRouting.Rules[0].MaxInputTokens)
	for _, tokens := range []int{20, 800_000} {
		ctx := WithGroupInputRoutingRequest(context.Background(), restored.Group, tokens, false)
		_, matched := GroupInputRoutingDecisionFromContext(ctx)
		require.True(t, matched, "auth cache must preserve all-length semantics")
	}
	ctx := WithGroupInputRoutingRequest(context.Background(), restored.Group, 20, true)
	_, matched := GroupInputRoutingDecisionFromContext(ctx)
	require.False(t, matched, "request type must remain effective after cache restore")
}
