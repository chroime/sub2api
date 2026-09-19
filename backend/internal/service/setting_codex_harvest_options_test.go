package service

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexHarvestOptionRepo struct {
	SettingRepository
	mu               sync.Mutex
	values           map[string]string
	err              error
	getAllErr        error
	reads            int
	blockNext        bool
	entered, release chan struct{}
}

func (r *codexHarvestOptionRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	r.reads++
	block := r.blockNext
	r.blockNext = false
	err := r.err
	r.mu.Unlock()
	if block {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return values, err
}

func (r *codexHarvestOptionRepo) GetAll(context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.values), r.getAllErr
}

func (r *codexHarvestOptionRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[key], r.err
}

func (r *codexHarvestOptionRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	maps.Copy(r.values, values)
	return nil
}

func TestCodexHarvestOptionsIndependentCacheAndConfigFallback(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAICodexTicket:    config.OpenAICodexTicketConfig{VerifyEnabled: true, HarvestProxyIDs: []int64{4}, HarvestConcurrency: 7},
		OpenAICodexTicket332: config.OpenAICodexTicketConfig{HarvestProxyIDs: []int64{8}, HarvestConcurrency: 2},
	}}
	repo := &codexHarvestOptionRepo{values: map[string]string{
		SettingKeyOpenAICodexTicket332VerifyEnabled:      "true",
		SettingKeyOpenAICodexTicket332HarvestProxyIDs:    "[3,9]",
		SettingKeyOpenAICodexTicket332HarvestConcurrency: "5",
	}}
	s := NewSettingService(repo, cfg)
	ctx := context.Background()
	first := s.GetOpenAICodexTicketHarvestOptions(ctx, "292", s.codexTicketHarvestConfigOptions("292"))
	require.Equal(t, CodexTicketHarvestOptions{true, []int64{4}, 7}, first)
	second := s.GetOpenAICodexTicketHarvestOptions(ctx, "332", s.codexTicketHarvestConfigOptions("332"))
	require.Equal(t, CodexTicketHarvestOptions{true, []int64{3, 9}, 5}, second)
	first.ProxyIDs[0], second.ProxyIDs[0] = 100, 200
	require.Equal(t, []int64{4}, cfg.Gateway.OpenAICodexTicket.HarvestProxyIDs)
	require.Equal(t, []int64{8}, cfg.Gateway.OpenAICodexTicket332.HarvestProxyIDs)
	require.Equal(t, []int64{4}, s.GetOpenAICodexTicketHarvestOptions(ctx, "292", CodexTicketHarvestOptions{}).ProxyIDs)
	require.Equal(t, []int64{3, 9}, s.GetOpenAICodexTicketHarvestOptions(ctx, "332", CodexTicketHarvestOptions{}).ProxyIDs)
	require.Equal(t, 2, repo.reads, "cached reads must not query storage repeatedly")
	settings := s.parseSettings(repo.values)
	require.True(t, settings.OpenAICodexTicketVerifyEnabled)
	require.Equal(t, []int64{4}, settings.OpenAICodexTicketHarvestProxyIDs)
	require.Equal(t, 7, settings.OpenAICodexTicketHarvestConcurrency)
	require.Equal(t, []int64{3, 9}, settings.OpenAICodexTicket332HarvestProxyIDs)
}

func TestCodexHarvestOptionsMalformedStorageAndExplicitClear(t *testing.T) {
	fallback := CodexTicketHarvestOptions{true, []int64{4}, 6}
	for _, raw := range []string{"null", "[0]", "[-1]", "[2,2]", "not-json", "[1.5]"} {
		t.Run(raw, func(t *testing.T) {
			repo := &codexHarvestOptionRepo{values: map[string]string{
				SettingKeyOpenAICodexTicketVerifyEnabled:      "not-bool",
				SettingKeyOpenAICodexTicketHarvestProxyIDs:    raw,
				SettingKeyOpenAICodexTicketHarvestConcurrency: "17",
			}}
			s := NewSettingService(repo, nil)
			require.Equal(t, fallback, s.GetOpenAICodexTicketHarvestOptions(context.Background(), "292", fallback))
		})
	}
	repo := &codexHarvestOptionRepo{values: map[string]string{SettingKeyOpenAICodexTicketHarvestProxyIDs: "[]"}}
	s := NewSettingService(repo, nil)
	require.Equal(t, []int64{}, s.GetOpenAICodexTicketHarvestOptions(context.Background(), "292", fallback).ProxyIDs)
	require.Equal(t, CodexTicketHarvestOptions{false, []int64{}, 3}, s.GetOpenAICodexTicketHarvestOptions(context.Background(), "off", CodexTicketHarvestOptions{}))
}

func TestCodexHarvestOptionsSaveInvalidatesEvenIfSettingsRereadFails(t *testing.T) {
	for _, rereadFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reread fails"}[rereadFails], func(t *testing.T) {
			repo := &codexHarvestOptionRepo{values: map[string]string{SettingKeyOpenAICodexTicketHarvestConcurrency: "3"}}
			s := NewSettingService(repo, &config.Config{})
			ctx := context.Background()
			require.Equal(t, 3, s.GetOpenAICodexTicketHarvestOptions(ctx, "292", CodexTicketHarvestOptions{}).Concurrency)
			if rereadFails {
				repo.getAllErr = errors.New("post-write read unavailable")
			}
			require.NoError(t, s.UpdateSettingsOmitting(ctx, &SystemSettings{OpenAICodexTicketHarvestConcurrency: 7}, OmittedSettingKeys{SettingKeySiteName: {}}))
			require.Equal(t, 7, s.GetOpenAICodexTicketHarvestOptions(ctx, "292", CodexTicketHarvestOptions{}).Concurrency, "a committed partial update must invalidate cached options")
		})
	}
}

func TestCodexHarvestOptionsKeepsLastValueDuringOutageAndHonorsCancellation(t *testing.T) {
	repo := &codexHarvestOptionRepo{values: map[string]string{SettingKeyOpenAICodexTicketHarvestConcurrency: "9"}}
	s := NewSettingService(repo, nil)
	fallback := CodexTicketHarvestOptions{false, []int64{}, 3}
	ctx := context.Background()
	require.Equal(t, 9, s.GetOpenAICodexTicketHarvestOptions(ctx, "292", fallback).Concurrency)
	repo.err = errors.New("temporary outage")
	s.InvalidateOpenAICodexTicketHarvestOptions("292")
	require.Equal(t, 9, s.GetOpenAICodexTicketHarvestOptions(ctx, "292", fallback).Concurrency)
	s.InvalidateOpenAICodexTicketHarvestOptions("332")
	require.Equal(t, fallback, s.GetOpenAICodexTicketHarvestOptions(ctx, "332", fallback))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	readsBefore := repo.reads
	require.Equal(t, fallback, s.GetOpenAICodexTicketHarvestOptions(cancelled, "292", fallback))
	require.Equal(t, readsBefore, repo.reads)
}

func TestCodexHarvestOptionsSaveFencesStaleInFlightReads(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		t.Run(mode, func(t *testing.T) {
			keys, _ := codexTicketHarvestOptionKeys(mode)
			repo := &codexHarvestOptionRepo{values: map[string]string{keys[2]: "3"}, blockNext: true, entered: make(chan struct{}), release: make(chan struct{})}
			s := NewSettingService(repo, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan CodexTicketHarvestOptions, 1)
			go func() { done <- s.GetOpenAICodexTicketHarvestOptions(ctx, mode, CodexTicketHarvestOptions{}) }()
			select {
			case <-repo.entered:
			case <-ctx.Done():
				t.Fatal("initial read did not start")
			}
			require.NoError(t, repo.SetMultiple(ctx, map[string]string{keys[2]: "8", keys[1]: "[7]"}))
			s.InvalidateOpenAICodexTicketHarvestOptions(mode)
			want := CodexTicketHarvestOptions{false, []int64{7}, 8}
			require.Equal(t, want, s.GetOpenAICodexTicketHarvestOptions(ctx, mode, CodexTicketHarvestOptions{}))
			close(repo.release)
			select {
			case got := <-done:
				require.Equal(t, want, got)
			case <-ctx.Done():
				t.Fatal("old read did not finish")
			}
			require.Equal(t, want, s.GetOpenAICodexTicketHarvestOptions(ctx, mode, CodexTicketHarvestOptions{}))
		})
	}
}

func TestCodexHarvestOptionsInvalidatedReadRetriesWithoutAnotherCaller(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		t.Run(mode, func(t *testing.T) {
			keys, _ := codexTicketHarvestOptionKeys(mode)
			repo := &codexHarvestOptionRepo{
				values:    map[string]string{keys[0]: "false", keys[1]: "[]", keys[2]: "3"},
				blockNext: true, entered: make(chan struct{}), release: make(chan struct{}),
			}
			release := sync.OnceFunc(func() { close(repo.release) })
			t.Cleanup(release)
			s := NewSettingService(repo, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan CodexTicketHarvestOptions, 1)
			go func() { done <- s.GetOpenAICodexTicketHarvestOptions(ctx, mode, CodexTicketHarvestOptions{}) }()
			select {
			case <-repo.entered:
			case <-ctx.Done():
				t.Fatal("initial read did not start")
			}
			require.NoError(t, repo.SetMultiple(ctx, map[string]string{keys[0]: "true", keys[1]: "[7]", keys[2]: "1"}))
			s.InvalidateOpenAICodexTicketHarvestOptions(mode)
			// The blocked caller must refresh itself; no other reader fills the cache first.
			release()
			want := CodexTicketHarvestOptions{true, []int64{7}, 1}
			select {
			case got := <-done:
				require.Equal(t, want, got)
			case <-ctx.Done():
				t.Fatal("invalidated read did not finish")
			}
			require.Equal(t, want, s.GetOpenAICodexTicketHarvestOptions(ctx, mode, CodexTicketHarvestOptions{}))
		})
	}
}

type codexHarvestWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *codexHarvestWaitingContext) Done() <-chan struct{} {
	// GetOpenAICodexTicketHarvestOptions checks Done after joining the shared read.
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestCodexHarvestOptionsCanceledLeaderPreservesLiveWaiterSettings(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		t.Run(mode, func(t *testing.T) {
			keys, _ := codexTicketHarvestOptionKeys(mode)
			repo := &codexHarvestOptionRepo{
				values:  map[string]string{keys[0]: "true", keys[1]: "[7]", keys[2]: "1"},
				entered: make(chan struct{}), release: make(chan struct{}),
			}
			release := sync.OnceFunc(func() { close(repo.release) })
			t.Cleanup(release)
			s := NewSettingService(repo, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			fallback := CodexTicketHarvestOptions{false, []int64{}, 3}
			want := CodexTicketHarvestOptions{true, []int64{7}, 1}
			require.Equal(t, want, s.GetOpenAICodexTicketHarvestOptions(ctx, mode, fallback))
			s.InvalidateOpenAICodexTicketHarvestOptions(mode)
			repo.mu.Lock()
			repo.blockNext = true
			repo.mu.Unlock()

			leaderCtx, cancelLeader := context.WithCancel(ctx)
			defer cancelLeader()
			leaderDone := make(chan CodexTicketHarvestOptions, 1)
			go func() { leaderDone <- s.GetOpenAICodexTicketHarvestOptions(leaderCtx, mode, fallback) }()
			select {
			case <-repo.entered:
			case <-ctx.Done():
				t.Fatal("leader read did not start")
			}
			waiterCtx := &codexHarvestWaitingContext{Context: ctx, waiting: make(chan struct{})}
			waiterDone := make(chan CodexTicketHarvestOptions, 1)
			go func() { waiterDone <- s.GetOpenAICodexTicketHarvestOptions(waiterCtx, mode, fallback) }()
			select {
			case <-waiterCtx.waiting:
			case <-ctx.Done():
				t.Fatal("live caller did not join the shared read")
			}
			cancelLeader()
			select {
			case <-leaderDone:
			case <-ctx.Done():
				t.Fatal("canceled leader did not return promptly")
			}
			release()
			select {
			case got := <-waiterDone:
				require.Equal(t, want, got, "another caller's cancellation must not reset active settings")
			case <-ctx.Done():
				t.Fatal("live waiter did not finish")
			}
		})
	}
}
