package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const SettingKeyStreamingACKEnabled = "streaming_ack_enabled"

const (
	streamingACKCacheTTL  = 5 * time.Second
	streamingACKErrorTTL  = time.Second
	streamingACKDBTimeout = time.Second
)

type StreamingACKSettings struct {
	Enabled bool `json:"enabled"`
}

type cachedStreamingACKSettings struct {
	enabled   bool
	expiresAt int64
}

// GetStreamingACKSettings reads the persisted override for the administration UI.
// Only an absent setting falls back to deployment configuration; read and parse
// failures must not be reported as a successfully loaded disabled/enabled switch.
func (s *SettingService) GetStreamingACKSettings(ctx context.Context) (*StreamingACKSettings, error) {
	if s == nil || s.settingRepo == nil {
		return nil, fmt.Errorf("streaming ACK settings repository is unavailable")
	}
	s.streamingACKMu.Lock()
	defer s.streamingACKMu.Unlock()
	settings, err := s.readStreamingACKSettings(ctx)
	if err != nil {
		return nil, err
	}
	s.storeStreamingACKCache(settings.Enabled, streamingACKCacheTTL)
	return settings, nil
}

func (s *SettingService) readStreamingACKSettings(ctx context.Context) (*StreamingACKSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyStreamingACKEnabled)
	if errors.Is(err, ErrSettingNotFound) {
		return &StreamingACKSettings{Enabled: s.cfg != nil && s.cfg.Gateway.SyntheticFirstResponse.Enabled}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get streaming ACK settings: %w", err)
	}
	switch strings.TrimSpace(value) {
	case "true":
		return &StreamingACKSettings{Enabled: true}, nil
	case "false":
		return &StreamingACKSettings{Enabled: false}, nil
	default:
		return nil, fmt.Errorf("invalid streaming ACK enabled setting")
	}
}

// SetStreamingACKSettings changes the current instance immediately after the
// write succeeds. Other instances refresh within the short runtime cache TTL.
func (s *SettingService) SetStreamingACKSettings(ctx context.Context, settings *StreamingACKSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("streaming ACK settings repository is unavailable")
	}
	if settings == nil {
		return fmt.Errorf("streaming ACK settings cannot be nil")
	}
	// Serialize refreshes and writes so an in-flight old read cannot overwrite a
	// successful update with stale cache contents.
	s.streamingACKMu.Lock()
	defer s.streamingACKMu.Unlock()
	if err := s.settingRepo.Set(ctx, SettingKeyStreamingACKEnabled, strconv.FormatBool(settings.Enabled)); err != nil {
		return fmt.Errorf("save streaming ACK settings: %w", err)
	}
	s.storeStreamingACKCache(settings.Enabled, streamingACKCacheTTL)
	return nil
}

// IsStreamingACKEnabled is the cached request-path gate. Storage errors fail
// closed without enabling ACK from a potentially overridden deployment default.
func (s *SettingService) IsStreamingACKEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	if cached, ok := s.streamingACKCache.Load().(*cachedStreamingACKSettings); ok && time.Now().UnixNano() < cached.expiresAt {
		return cached.enabled
	}
	s.streamingACKMu.Lock()
	defer s.streamingACKMu.Unlock()
	if cached, ok := s.streamingACKCache.Load().(*cachedStreamingACKSettings); ok && time.Now().UnixNano() < cached.expiresAt {
		return cached.enabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), streamingACKDBTimeout)
	defer cancel()
	settings, err := s.readStreamingACKSettings(dbCtx)
	if err != nil {
		slog.Warn("failed to refresh streaming ACK settings", "error", err)
		s.storeStreamingACKCache(false, streamingACKErrorTTL)
		return false
	}
	s.storeStreamingACKCache(settings.Enabled, streamingACKCacheTTL)
	return settings.Enabled
}

func (s *SettingService) storeStreamingACKCache(enabled bool, ttl time.Duration) {
	s.streamingACKCache.Store(&cachedStreamingACKSettings{
		enabled:   enabled,
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

// SyntheticFirstResponseConfig returns an independent snapshot, preserving all
// timing settings while applying the persisted runtime gate.
func (s *OpenAIGatewayService) SyntheticFirstResponseConfig(ctx context.Context) config.GatewaySyntheticFirstResponseConfig {
	var cfg config.GatewaySyntheticFirstResponseConfig
	if s == nil {
		return cfg
	}
	if s.cfg != nil {
		cfg = s.cfg.Gateway.SyntheticFirstResponse
	}
	if s.settingService != nil {
		cfg.Enabled = s.settingService.IsStreamingACKEnabled(ctx)
	}
	return cfg
}

// SyntheticFirstResponseConfig applies the same runtime gate to native and
// converted non-OpenAI SSE transports.
func (s *GatewayService) SyntheticFirstResponseConfig(ctx context.Context) config.GatewaySyntheticFirstResponseConfig {
	var cfg config.GatewaySyntheticFirstResponseConfig
	if s == nil {
		return cfg
	}
	if s.cfg != nil {
		cfg = s.cfg.Gateway.SyntheticFirstResponse
	}
	if s.settingService != nil {
		cfg.Enabled = s.settingService.IsStreamingACKEnabled(ctx)
	}
	return cfg
}
