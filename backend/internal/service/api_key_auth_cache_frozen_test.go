package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotFrozenBalanceRoundtrip(t *testing.T) {
	key := &APIKey{ID: 1, UserID: 2, Status: StatusActive, User: &User{ID: 2, Status: StatusActive, Balance: 0, FrozenBalance: .08}}
	svc := &APIKeyService{}
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))
	materialized, used, err := svc.applyAuthCacheEntry("pending-funds", &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, key.User.FrozenBalance, materialized.User.FrozenBalance)
}

func TestAPIKeyAuthSnapshotWithoutFrozenBalanceIsEvicted(t *testing.T) {
	svc := &APIKeyService{}
	_, used, err := svc.applyAuthCacheEntry("old-funds", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 24}})
	require.NoError(t, err)
	require.False(t, used)
}
