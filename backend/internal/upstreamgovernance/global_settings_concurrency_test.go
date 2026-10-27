package upstreamgovernance

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type concurrentGlobalSettingsStore struct {
	mu                  sync.Mutex
	raw                 string
	reads, writes       int
	firstWriteStarted   chan struct{}
	releaseFirstWrite   chan struct{}
	firstWriteCompleted chan struct{}
	secondRead          chan struct{}
}

func (s *concurrentGlobalSettingsStore) GetMultiple(context.Context, []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.reads == 2 {
		close(s.secondRead)
	}
	return map[string]string{GlobalSettingsKey: s.raw}, nil
}

func (s *concurrentGlobalSettingsStore) SetMultiple(ctx context.Context, values map[string]string) error {
	s.mu.Lock()
	s.writes++
	first := s.writes == 1
	s.mu.Unlock()
	wait := s.firstWriteCompleted
	if first {
		close(s.firstWriteStarted)
		wait = s.releaseFirstWrite
	}
	select {
	case <-wait:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	s.raw = values[GlobalSettingsKey]
	s.mu.Unlock()
	if first {
		close(s.firstWriteCompleted)
	}
	return nil
}

func TestGlobalSettingsConcurrentSaveDoesNotRestoreClearedKey(t *testing.T) {
	ciphertext, err := (fakeCipher{}).Encrypt("obsolete-global-secret")
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]any{
		"version": 1,
		"astrbot": map[string]string{"api_key_ciphertext": ciphertext},
	})
	require.NoError(t, err)
	store := &concurrentGlobalSettingsStore{
		raw: string(raw), firstWriteStarted: make(chan struct{}), releaseFirstWrite: make(chan struct{}),
		firstWriteCompleted: make(chan struct{}), secondRead: make(chan struct{}),
	}
	svc := NewService(nil, nil, nil, fakeCipher{}, true)
	svc.SetGlobalSettingsStore(store)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	clearResult := make(chan error, 1)
	go func() {
		_, err := svc.SaveGlobalSettings(ctx, GlobalSettings{Version: 1, AstrBot: AstrBotSettings{ClearAPIKey: true}})
		clearResult <- err
	}()
	select {
	case <-store.firstWriteStarted:
	case <-ctx.Done():
		t.Fatal("clear operation did not reach the controlled write")
	}
	preserveResult := make(chan error, 1)
	go func() {
		_, err := svc.SaveGlobalSettings(ctx, GlobalSettings{Version: 1})
		preserveResult <- err
	}()
	// If saves are not serialized, the second request reads the old ciphertext
	// while the clear is paused. Its write is forced to follow the clear.
	readBeforeClear := false
	select {
	case <-store.secondRead:
		readBeforeClear = true
	case <-time.After(100 * time.Millisecond):
	}
	close(store.releaseFirstWrite)
	require.NoError(t, <-clearResult)
	assert.ErrorIs(t, <-preserveResult, ErrConflict)
	assert.False(t, readBeforeClear, "another save must not read the old version before clear is committed")
	settings, err := svc.GetRuntimeGlobalSettings(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, settings.Version)
	assert.Empty(t, settings.AstrBot.APIKey)
	assert.False(t, settings.AstrBot.APIKeyConfigured)
	store.mu.Lock()
	assert.Equal(t, 1, store.writes, "the stale request must not write settings")
	store.mu.Unlock()
}
