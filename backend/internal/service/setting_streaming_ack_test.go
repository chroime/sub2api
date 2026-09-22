package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type streamingACKSettingRepo struct {
	SettingRepository
	value    string
	exists   bool
	readErr  error
	writeErr error
	reads    int
}

func (r *streamingACKSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	r.reads++
	if r.readErr != nil {
		return "", r.readErr
	}
	if !r.exists {
		return "", ErrSettingNotFound
	}
	return r.value, nil
}

func (r *streamingACKSettingRepo) Set(ctx context.Context, key, value string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.value, r.exists = value, true
	return nil
}

func streamingACKTestConfig(enabled bool) *config.Config {
	return &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			Enabled:                  enabled,
			MinDelayMs:               600,
			MaxDelayMs:               1500,
			UnderOneSecondPercent:    90,
			UnderOneSecondMaxDelayMs: 900,
		},
	}}
}

func TestGetStreamingACKSettingsFallbackAndOverrides(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, value := range []string{"missing", "false", "true"} {
			t.Run(strconv.FormatBool(fallback)+"/"+value, func(t *testing.T) {
				repo := &streamingACKSettingRepo{value: value, exists: value != "missing"}
				cfg := streamingACKTestConfig(fallback)
				svc := NewSettingService(repo, cfg)
				settings, err := svc.GetStreamingACKSettings(context.Background())
				require.NoError(t, err)
				want := fallback
				if repo.exists {
					want = value == "true"
				}
				require.Equal(t, want, settings.Enabled)
				require.Equal(t, want, svc.IsStreamingACKEnabled(context.Background()))
				require.Equal(t, fallback, cfg.Gateway.SyntheticFirstResponse.Enabled)
			})
		}
	}
}

func TestStreamingACKReadFailureDoesNotEnableFromConfig(t *testing.T) {
	for _, value := range []string{"database failure", "", "invalid", "1"} {
		t.Run(value, func(t *testing.T) {
			repo := &streamingACKSettingRepo{value: value, exists: true}
			if value == "database failure" {
				repo.readErr = errors.New(value)
			}
			svc := NewSettingService(repo, streamingACKTestConfig(true))
			settings, err := svc.GetStreamingACKSettings(context.Background())
			require.Error(t, err)
			require.Nil(t, settings)
			require.False(t, svc.IsStreamingACKEnabled(context.Background()))
		})
	}
}

func TestSetStreamingACKSettingsUpdatesCacheOnlyAfterPersistence(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(strconv.FormatBool(initial), func(t *testing.T) {
			repo := &streamingACKSettingRepo{exists: true, value: strconv.FormatBool(initial)}
			cfg := streamingACKTestConfig(initial)
			svc := NewSettingService(repo, cfg)
			require.Equal(t, initial, svc.IsStreamingACKEnabled(context.Background()))

			repo.writeErr = errors.New("write failed")
			err := svc.SetStreamingACKSettings(context.Background(), &StreamingACKSettings{Enabled: !initial})
			require.Error(t, err)
			require.Equal(t, initial, svc.IsStreamingACKEnabled(context.Background()))
			require.Equal(t, strconv.FormatBool(initial), repo.value)

			repo.writeErr = nil
			require.NoError(t, svc.SetStreamingACKSettings(context.Background(), &StreamingACKSettings{Enabled: !initial}))
			require.Equal(t, strconv.FormatBool(!initial), repo.value)
			repo.readErr = errors.New("should not query after save")
			require.Equal(t, !initial, svc.IsStreamingACKEnabled(context.Background()))
			require.Equal(t, initial, cfg.Gateway.SyntheticFirstResponse.Enabled)
		})
	}
}

func TestStreamingACKRuntimeRefreshesExpiredCacheAndFailsClosed(t *testing.T) {
	repo := &streamingACKSettingRepo{exists: true, value: "false"}
	svc := NewSettingService(repo, streamingACKTestConfig(true))
	require.False(t, svc.IsStreamingACKEnabled(context.Background()))
	repo.value = "true"
	require.False(t, svc.IsStreamingACKEnabled(context.Background()))
	require.Equal(t, 1, repo.reads)
	svc.streamingACKCache.Store(&cachedStreamingACKSettings{expiresAt: time.Now().Add(-time.Second).UnixNano()})
	require.True(t, svc.IsStreamingACKEnabled(context.Background()))
	require.Equal(t, 2, repo.reads)

	repo.readErr = errors.New("database unavailable")
	svc.streamingACKCache.Store(&cachedStreamingACKSettings{enabled: true, expiresAt: time.Now().Add(-time.Second).UnixNano()})
	require.False(t, svc.IsStreamingACKEnabled(context.Background()))
	require.False(t, svc.IsStreamingACKEnabled(context.Background()))
	require.Equal(t, 3, repo.reads)
}

func TestOpenAIStreamingACKConfigPreservesTimingAndDoesNotMutateConfig(t *testing.T) {
	cfg := streamingACKTestConfig(false)
	original := cfg.Gateway.SyntheticFirstResponse
	svc := NewSettingService(&streamingACKSettingRepo{exists: true, value: "true"}, cfg)
	gateway := &OpenAIGatewayService{cfg: cfg, settingService: svc}
	runtime := gateway.SyntheticFirstResponseConfig(context.Background())
	require.True(t, runtime.Enabled)
	runtime.Enabled = false
	require.Equal(t, original, runtime)
	require.Equal(t, original, cfg.Gateway.SyntheticFirstResponse)
}
