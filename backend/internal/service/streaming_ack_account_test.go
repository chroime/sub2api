//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamingACKAccountPlatformsAndGroupIsolation(t *testing.T) {
	group, other := int64(42), int64(99)
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			a := &Account{Platform: platform, Extra: map[string]any{"streaming_ack_enabled": true}, GroupIDs: []int64{group}}
			require.True(t, a.IsOpenAISyntheticFirstResponseEnabledForGroup(&group))
			require.False(t, a.IsOpenAISyntheticFirstResponseEnabledForGroup(&other))
			require.False(t, a.IsOpenAISyntheticFirstResponseEnabledForGroup(nil))
			a.GroupIDs = nil
			require.True(t, a.IsOpenAISyntheticFirstResponseEnabledForGroup(nil))
			a.ParentAccountID = &other
			require.False(t, a.IsOpenAISyntheticFirstResponseEnabledForGroup(nil))
		})
	}
}

func TestStreamingACKGenericKeyOverridesLegacyOpenAI(t *testing.T) {
	for _, value := range []any{false, "true", nil} {
		a := &Account{Platform: PlatformOpenAI, Extra: map[string]any{
			"streaming_ack_enabled": value, OpenAISyntheticFirstResponseEnabledExtraKey: true,
		}}
		require.False(t, a.IsOpenAISyntheticFirstResponseEnabled())
	}
	require.False(t, (&Account{Platform: "unknown", Extra: map[string]any{"streaming_ack_enabled": true}}).IsOpenAISyntheticFirstResponseEnabled())
}

func TestStreamingACKValidateGenericKey(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok} {
		for _, invalid := range []any{"true", 1, nil} {
			require.Error(t, ValidateOpenAISyntheticFirstResponseExtra(platform, map[string]any{"streaming_ack_enabled": invalid}))
		}
		require.NoError(t, ValidateOpenAISyntheticFirstResponseExtra(platform, map[string]any{"streaming_ack_enabled": true}))
	}
}

func TestStreamingACKGenericExtraUpdateValidates(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{ID: 1, Platform: PlatformAnthropic}}
	svc := &adminServiceImpl{accountRepo: repo}
	require.Error(t, svc.UpdateAccountExtra(context.Background(), 1, map[string]any{"streaming_ack_enabled": "true"}))
	require.Zero(t, repo.updateExtraCalls)
	require.NoError(t, svc.UpdateAccountExtra(context.Background(), 1, map[string]any{"streaming_ack_enabled": false}))
	require.Equal(t, 1, repo.updateExtraCalls)
}

func TestStreamingACKBulkAllowsMixedPlatformsRejectsShadow(t *testing.T) {
	input := &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Extra: map[string]any{"streaming_ack_enabled": true}}
	settings, err := normalizeBulkOpenAISettings(input)
	require.NoError(t, err)
	require.True(t, settings.any())
	targets := map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI},
		2: {ID: 2, Platform: PlatformAnthropic},
	}
	_, err = validateBulkOpenAISettingsTargets(input, settings, targets)
	require.NoError(t, err)
	parent := int64(3)
	targets[2].ParentAccountID = &parent
	_, err = validateBulkOpenAISettingsTargets(input, settings, targets)
	require.Error(t, err)
	_, err = normalizeBulkOpenAISettings(&BulkUpdateAccountsInput{Extra: map[string]any{"streaming_ack_enabled": "true"}})
	require.Error(t, err)
}

func TestStreamingACKNativeUsageKeepsRealLatency(t *testing.T) {
	ctx, _ := syntheticFirstResponseTestContext(t, "native-usage")
	ack, real := 650, 7400
	ctx.Set(openAISyntheticFirstResponseKey, &openAISyntheticFirstResponse{ackMs: &ack})
	result := &ForwardResult{RequestID: "native-usage", Model: "claude-sonnet-4", Stream: true, FirstTokenMs: &real}
	ApplyStreamingACKResult(ctx, result)
	svc := &GatewayService{}
	usage := svc.buildRecordUsageLog(context.Background(), &recordUsageCoreInput{}, result,
		&APIKey{ID: 1}, &User{ID: 2}, &Account{ID: 3, Platform: PlatformAnthropic}, nil,
		result.Model, 1, 1, 1, 0, false, nil)
	require.Equal(t, real, *usage.FirstTokenMs)
	require.NotNil(t, usage.StreamingAckMs)
	require.Equal(t, ack, *usage.StreamingAckMs)
	ApplyStreamingACKResult(ctx, nil)
	result.FirstTokenMs = nil
	ApplyStreamingACKResult(ctx, result)
	require.Nil(t, result.FirstTokenMs)
}

func TestStreamingACKNativeConfigUsesGlobalSwitch(t *testing.T) {
	cfg := streamingACKTestConfig(false)
	svc := NewSettingService(&streamingACKSettingRepo{exists: true, value: "true"}, cfg)
	gateway := &GatewayService{cfg: cfg, settingService: svc}
	runtime := gateway.SyntheticFirstResponseConfig(context.Background())
	require.True(t, runtime.Enabled)
	runtime.Enabled = false
	require.Equal(t, cfg.Gateway.SyntheticFirstResponse, runtime)
	require.NoError(t, svc.SetStreamingACKSettings(context.Background(), &StreamingACKSettings{Enabled: false}))
	require.False(t, gateway.SyntheticFirstResponseConfig(context.Background()).Enabled)
}
