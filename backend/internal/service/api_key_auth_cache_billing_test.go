package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotBillingTokenAdjustmentRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()
	apiKey.Group.BillingInputTokenMultiplier = 1.25
	apiKey.Group.BillingOutputTokenMultiplier = 1.75
	apiKey.Group.BillingTokenAdjustmentMinInputTokens = 123

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.NotNil(t, snapshot.Group)

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized)
	require.NotNil(t, materialized.Group)
	require.InDelta(t, 1.25, materialized.Group.BillingInputTokenMultiplier, 1e-12)
	require.InDelta(t, 1.75, materialized.Group.BillingOutputTokenMultiplier, 1e-12)
	require.Equal(t, 123, materialized.Group.BillingTokenAdjustmentMinInputTokens)
}

func TestAPIKeyService_RejectsV27AuthSnapshotWithoutBillingTokenAdjustment(t *testing.T) {
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), profitAuthTestAPIKey())
	require.NotNil(t, snapshot)
	snapshot.Version = 27

	materialized, used, err := svc.applyAuthCacheEntry("sk-old-billing-adjustment", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.False(t, used, "v27 snapshots must be evicted after billing token adjustment fields are added")
	require.Nil(t, materialized)
}
