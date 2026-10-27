//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountQualityGateBatchSelectionSkipsOnlyIsolatedModel(t *testing.T) {
	svc, _, _, _, _ := newTestBatchImagePublicService(true)
	accounts := []Account{testBatchImageAccount(101, AccountTypeAPIKey), testBatchImageAccount(202, AccountTypeAPIKey)}
	accounts[0].Priority = 0
	accounts[1].Priority = 9
	accounts[0].Extra = map[string]any{AccountQualityGatesExtraKey: map[string]any{
		"17": map[string]any{"scope": "model", "model": "gemini-2.5-flash-image", "blocked": true},
	}}
	svc.AccountRepo = &publicBatchImageAccountRepo{accounts: accounts}
	_, selected, err := svc.selectProviderAndAccount(context.Background(), testBatchImageOwner(), BatchImageProviderGeminiAPI, "gemini-2.5-flash-image")
	require.NoError(t, err)
	require.Equal(t, int64(202), selected.ID)
	_, selected, err = svc.selectProviderAndAccount(context.Background(), testBatchImageOwner(), BatchImageProviderGeminiAPI, "other-model")
	require.NoError(t, err)
	require.Equal(t, int64(101), selected.ID)
}

func TestAccountQualityGateGenericSelectionRechecksHydratedSnapshot(t *testing.T) {
	for _, model := range []string{"public-model", "other-model"} {
		t.Run(model, func(t *testing.T) {
			latest := qualityGateTestAccount("model", "upstream-model")
			latest.Platform = PlatformAnthropic
			latest.Concurrency = 1
			candidate := *latest
			candidate.Extra = nil
			cache := &snapshotHydrationCache{snapshot: []*Account{&candidate}, accounts: map[int64]*Account{latest.ID: latest}}
			svc := &GatewayService{schedulerSnapshot: NewSchedulerSnapshotService(cache, nil, nil, nil, nil)}
			selected, err := svc.SelectAccountForModel(context.Background(), nil, "", model)
			if model == "public-model" {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, selected)
			} else {
				require.NoError(t, err)
				require.Equal(t, latest.ID, selected.ID)
			}
		})
	}
}

func TestAccountQualityGateLatestIsolationReleasesAcquiredSlot(t *testing.T) {
	latest := qualityGateTestAccount("account", "")
	candidate := *latest
	candidate.Extra = nil
	cache := &snapshotHydrationCache{accounts: map[int64]*Account{latest.ID: latest}}
	svc := &GatewayService{schedulerSnapshot: NewSchedulerSnapshotService(cache, nil, nil, nil, nil)}
	releases := 0
	selected, err := svc.newSelectionResult(context.Background(), &candidate, true, func() { releases++ }, nil)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selected)
	require.Equal(t, 1, releases)
}
