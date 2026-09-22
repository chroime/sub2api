package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexDualTicketRuntimeSettingsAreIndependent(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketEnabled: "true", SettingKeyOpenAICodexTicketFailClosed: "false",
		SettingKeyOpenAICodexTicket332Enabled: "false", SettingKeyOpenAICodexTicket332FailClosed: "true",
		SettingKeyOpenAICodexTicketHarvestProxyURL:    "http://first.example:8080",
		SettingKeyOpenAICodexTicket332HarvestProxyURL: "socks5h://second.example:1080",
	}}}
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAICodexTicket: config.OpenAICodexTicketConfig{FailClosed: true},
	}}
	s := NewSettingService(repo, cfg)
	ctx := context.Background()
	require.True(t, s.GetOpenAICodexTicketEnabled(ctx, false))
	require.False(t, s.GetOpenAICodexTicketFailClosed(ctx, true))
	require.False(t, s.GetOpenAICodexTicket332Enabled(ctx, true))
	require.True(t, s.GetOpenAICodexTicket332FailClosed(ctx, false))
	require.Equal(t, "http://first.example:8080", s.GetOpenAICodexTicketHarvestProxyURL(ctx))
	require.Equal(t, "socks5h://second.example:1080", s.GetOpenAICodexTicket332HarvestProxyURL(ctx))

	repo.values[SettingKeyOpenAICodexTicketEnabled] = "false"
	repo.values[SettingKeyOpenAICodexTicketFailClosed] = "true"
	repo.values[SettingKeyOpenAICodexTicket332Enabled] = "true"
	repo.values[SettingKeyOpenAICodexTicket332FailClosed] = "false"
	repo.values[SettingKeyOpenAICodexTicketHarvestProxyURL] = "http://updated-first.example:8080"
	repo.values[SettingKeyOpenAICodexTicket332HarvestProxyURL] = "http://updated-second.example:8080"
	s.refreshCachedSettings(&SystemSettings{})
	require.False(t, s.GetOpenAICodexTicketEnabled(ctx, true))
	require.True(t, s.GetOpenAICodexTicketFailClosed(ctx, false))
	require.True(t, s.GetOpenAICodexTicket332Enabled(ctx, false))
	require.False(t, s.GetOpenAICodexTicket332FailClosed(ctx, true))
	require.Equal(t, "http://updated-first.example:8080", s.GetOpenAICodexTicketHarvestProxyURL(ctx))
	require.Equal(t, "http://updated-second.example:8080", s.GetOpenAICodexTicket332HarvestProxyURL(ctx))
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled)
	require.True(t, cfg.Gateway.OpenAICodexTicket.FailClosed)
	require.False(t, cfg.Gateway.OpenAICodexTicket332.Enabled)
	require.False(t, cfg.Gateway.OpenAICodexTicket332.FailClosed)
}

func TestCodexDualTicketMissingSettingsUseIndependentFallbacks(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}
	s := NewSettingService(repo, &config.Config{Gateway: config.GatewayConfig{
		OpenAICodexTicket: config.OpenAICodexTicketConfig{FailClosed: true},
	}})
	settings := s.parseSettings(repo.values)
	require.False(t, settings.OpenAICodexTicketEnabled)
	require.True(t, settings.OpenAICodexTicketFailClosed)
	require.False(t, settings.OpenAICodexTicket332Enabled)
	require.False(t, settings.OpenAICodexTicket332FailClosed)
	require.False(t, s.GetOpenAICodexTicketEnabled(context.Background(), false))
	require.True(t, s.GetOpenAICodexTicketFailClosed(context.Background(), true))
	require.False(t, s.GetOpenAICodexTicket332Enabled(context.Background(), false))
	require.False(t, s.GetOpenAICodexTicket332FailClosed(context.Background(), false))
	require.Empty(t, s.GetOpenAICodexTicketHarvestProxyURL(context.Background()))
	require.Empty(t, s.GetOpenAICodexTicket332HarvestProxyURL(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.True(t, s.GetOpenAICodexTicket332Enabled(ctx, true))
	require.False(t, s.GetOpenAICodexTicketFailClosed(ctx, false))
	require.Empty(t, s.GetOpenAICodexTicket332HarvestProxyURL(ctx))
}

func TestCodexDualTicketRuntimeCacheExpiryAndStorageFailure(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketFailClosed:         "true",
		SettingKeyOpenAICodexTicket332FailClosed:      "false",
		SettingKeyOpenAICodexTicket332HarvestProxyURL: "http://last.example:8080",
	}}}
	s := NewSettingService(repo, nil)
	ctx := context.Background()
	require.True(t, s.GetOpenAICodexTicketFailClosed(ctx, false))
	require.False(t, s.GetOpenAICodexTicket332FailClosed(ctx, true))
	require.Equal(t, "http://last.example:8080", s.GetOpenAICodexTicket332HarvestProxyURL(ctx))
	repo.values[SettingKeyOpenAICodexTicket332HarvestProxyURL] = "http://other-instance.example:8080"
	s.openAICodexTicket332HarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{value: "http://last.example:8080"})
	require.Equal(t, "http://other-instance.example:8080", s.GetOpenAICodexTicket332HarvestProxyURL(ctx))
	s.InvalidateOpenAICodexTicketFailClosedCache()
	s.InvalidateOpenAICodexTicket332FailClosedCache()
	s.InvalidateOpenAICodexTicket332HarvestProxyCache()
	repo.err = errors.New("temporary storage outage")
	require.True(t, s.GetOpenAICodexTicketFailClosed(ctx, false), "preserve the latest known mode policy during an outage")
	require.False(t, s.GetOpenAICodexTicket332FailClosed(ctx, true))
	require.Equal(t, "http://other-instance.example:8080", s.GetOpenAICodexTicket332HarvestProxyURL(ctx))
	// Invalidating an unread key must not turn its fallback into a fabricated false.
	s.InvalidateOpenAICodexTicket332EnabledCache()
	require.True(t, s.GetOpenAICodexTicket332Enabled(ctx, true))
}

type blockedCodexTicketSettingRepo struct {
	SettingRepository
	mu               sync.Mutex
	value            string
	blocked          bool
	entered, release chan struct{}
}

func (r *blockedCodexTicketSettingRepo) GetValue(ctx context.Context, _ string) (string, error) {
	r.mu.Lock()
	value, block := r.value, !r.blocked
	r.blocked = true
	r.mu.Unlock()
	if block {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return value, nil
}

func TestCodexDualTicketInvalidationFencesStaleInFlightReads(t *testing.T) {
	for _, tc := range []struct {
		name, oldValue, newValue string
		get                      func(*SettingService) any
		invalidate               func(*SettingService)
		want                     any
	}{
		{"332 enabled", "false", "true", func(s *SettingService) any { return s.GetOpenAICodexTicket332Enabled(context.Background(), false) }, (*SettingService).InvalidateOpenAICodexTicket332EnabledCache, true},
		{"292 fail closed", "false", "true", func(s *SettingService) any { return s.GetOpenAICodexTicketFailClosed(context.Background(), true) }, (*SettingService).InvalidateOpenAICodexTicketFailClosedCache, true},
		{"332 proxy", "http://old.example:8080", "http://new.example:8080", func(s *SettingService) any { return s.GetOpenAICodexTicket332HarvestProxyURL(context.Background()) }, (*SettingService).InvalidateOpenAICodexTicket332HarvestProxyCache, "http://new.example:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &blockedCodexTicketSettingRepo{value: tc.oldValue, entered: make(chan struct{}), release: make(chan struct{})}
			s := NewSettingService(repo, nil)
			done := make(chan any, 1)
			go func() { done <- tc.get(s) }()
			select {
			case <-repo.entered:
			case <-time.After(time.Second):
				t.Fatal("initial settings read did not start")
			}
			repo.mu.Lock()
			repo.value = tc.newValue
			repo.mu.Unlock()
			tc.invalidate(s)
			require.Equal(t, tc.want, tc.get(s))
			close(repo.release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("initial read did not finish")
			}
			require.Equal(t, tc.want, tc.get(s), "an old query must not restore a stale cache after settings were saved")
		})
	}
}
