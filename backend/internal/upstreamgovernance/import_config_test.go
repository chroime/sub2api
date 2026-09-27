package upstreamgovernance

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreviewFreezesAccountImportDefaults(t *testing.T) {
	s, _, _, local := setupEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected[0].AccountName = ""
	preview, err := s.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example--0.8", preview.Rows[0].Selection.AccountName)
	raw, err := json.Marshal(preview.Rows[0].Selection)
	require.NoError(t, err)
	var saved map[string]any
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Equal(t, map[string]any{
		"concurrency":                         float64(5000),
		"priority":                            float64(1),
		"model_mapping":                       map[string]any{"gpt-fixture": "gpt-fixture"},
		"upstream_billing_rate_sync_enabled":  true,
		"quota_daily_limit":                   float64(10000),
		"quota_weekly_limit":                  float64(700000),
		"quota_limit":                         float64(10000000),
		"openai_long_context_billing_enabled": true,
	}, saved["account_config"])
	result, err := s.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Len(t, local.changes, 1)
	require.Equal(t, preview.Rows[0].Selection.AccountConfig, local.changes[0].AccountConfig)
}

func TestPreviewCopiesExplicitConfigurationAndRejectsInvalidSettings(t *testing.T) {
	s, _, _, local := setupEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected[0].AccountConfig = &AccountConfig{Concurrency: 99, ModelMapping: map[string]string{"client-*": "upstream-model"}, QuotaLimit: 12}
	preview, err := s.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	selected[0].AccountConfig.Concurrency = 123
	selected[0].AccountConfig.ModelMapping["client-*"] = "changed-after-preview"
	result, err := s.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, 99, local.changes[0].AccountConfig.Concurrency)
	require.Equal(t, "upstream-model", local.changes[0].AccountConfig.ModelMapping["client-*"])
	for name, config := range map[string]AccountConfig{
		"concurrency":      {Concurrency: 0},
		"negative quota":   {Concurrency: 1, QuotaDailyLimit: -1},
		"nonfinite quota":  {Concurrency: 1, QuotaWeeklyLimit: math.Inf(1)},
		"invalid wildcard": {Concurrency: 1, ModelMapping: map[string]string{"gpt*bad": "model"}},
		"wildcard target":  {Concurrency: 1, ModelMapping: map[string]string{"gpt-*": "model-*"}},
	} {
		t.Run(name, func(t *testing.T) {
			selected[0].AccountConfig = &config
			_, err := s.Preview(t.Context(), 1, selected)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestPreviewDefaultNameUsesReviewedCostPrecision(t *testing.T) {
	s, _, _, _ := setupEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected[0].AccountName = ""
	selected[0].CostMultiplier = 1.23456
	preview, err := s.Preview(t.Context(), 1, selected)
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example--1.2346", preview.Rows[0].Selection.AccountName)
	require.Equal(t, 1.2346, preview.Rows[0].Selection.CostMultiplier)
}

func TestApplyRecoversAfterSyncedRateChanges(t *testing.T) {
	s, store, connector, local := setupEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	preview, err := s.Preview(t.Context(), 1, selections())
	require.NoError(t, err)
	store.failBindingAfterAccount = true
	result, err := s.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	account := local.accounts[preview.Rows[0].Marker]
	account.CostMultiplier, account.Fingerprint = 1.25, "probe-updated"
	store.failBindingAfterAccount = false
	result, err = s.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, 1, connector.keyCalls)
}

func TestApplyRequiresPreviewContainingAccountSettings(t *testing.T) {
	s, _, connector, _ := setupEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	preview, err := s.Preview(t.Context(), 1, selections())
	require.NoError(t, err)
	preview.Rows[0].Selection.AccountConfig = nil
	_, err = s.Apply(t.Context(), 1, preview.ID)
	require.ErrorIs(t, err, ErrConflict)
	require.Zero(t, connector.keyCalls)
}
