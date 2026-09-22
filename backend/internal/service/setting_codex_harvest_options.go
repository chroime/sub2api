package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/singleflight"
)

type CodexTicketHarvestOptions struct {
	VerifyEnabled bool
	ProxyIDs      []int64
	Concurrency   int
}

type cachedCodexTicketHarvestOptions struct {
	options     CodexTicketHarvestOptions
	expiresAt   int64
	initialized bool
}

func cloneCodexTicketHarvestOptions(options CodexTicketHarvestOptions) CodexTicketHarvestOptions {
	options.ProxyIDs = append([]int64{}, options.ProxyIDs...)
	return options
}

func codexTicketHarvestOptionKeys(mode string) ([]string, bool) {
	switch mode {
	case "292":
		return []string{SettingKeyOpenAICodexTicketVerifyEnabled, SettingKeyOpenAICodexTicketHarvestProxyIDs, SettingKeyOpenAICodexTicketHarvestConcurrency}, true
	case "332":
		return []string{SettingKeyOpenAICodexTicket332VerifyEnabled, SettingKeyOpenAICodexTicket332HarvestProxyIDs, SettingKeyOpenAICodexTicket332HarvestConcurrency}, true
	default:
		return nil, false
	}
}

func normalizeCodexTicketHarvestFallback(options CodexTicketHarvestOptions) CodexTicketHarvestOptions {
	if options.Concurrency < 1 || options.Concurrency > 16 {
		options.Concurrency = 3
	}
	if config.ValidateCodexTicketHarvestOptions(options.ProxyIDs, options.Concurrency) != nil {
		options.ProxyIDs = nil
	}
	return cloneCodexTicketHarvestOptions(options)
}

func (s *SettingService) codexTicketHarvestConfigOptions(mode string) CodexTicketHarvestOptions {
	var cfg config.OpenAICodexTicketConfig
	if s != nil && s.cfg != nil {
		if mode == "332" {
			cfg = s.cfg.Gateway.OpenAICodexTicket332
		} else {
			cfg = s.cfg.Gateway.OpenAICodexTicket
		}
	}
	return normalizeCodexTicketHarvestFallback(CodexTicketHarvestOptions{cfg.VerifyEnabled, cfg.HarvestProxyIDs, cfg.HarvestConcurrency})
}

func parseCodexTicketHarvestOptions(values map[string]string, keys []string, fallback CodexTicketHarvestOptions) CodexTicketHarvestOptions {
	options := cloneCodexTicketHarvestOptions(fallback)
	switch strings.TrimSpace(values[keys[0]]) {
	case "true":
		options.VerifyEnabled = true
	case "false":
		options.VerifyEnabled = false
	}
	if raw := strings.TrimSpace(values[keys[1]]); raw != "" && raw != "null" {
		var ids []int64
		if json.Unmarshal([]byte(raw), &ids) == nil && config.ValidateCodexTicketHarvestOptions(ids, options.Concurrency) == nil {
			options.ProxyIDs = append([]int64{}, ids...)
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(values[keys[2]])); err == nil && n >= 1 && n <= 16 {
		options.Concurrency = n
	}
	return options
}

func (s *SettingService) codexTicketHarvestOptionsCache(mode string) (*atomic.Value, *singleflight.Group) {
	if mode == "332" {
		return &s.openAICodexTicket332HarvestOptionsCache, &s.openAICodexTicket332HarvestOptionsSF
	}
	return &s.openAICodexTicketHarvestOptionsCache, &s.openAICodexTicketHarvestOptionsSF
}

// GetOpenAICodexTicketHarvestOptions reads all options for one mode together.
// Cache snapshots never share mutable proxy-ID slices with callers or config.
func (s *SettingService) GetOpenAICodexTicketHarvestOptions(ctx context.Context, mode string, fallback CodexTicketHarvestOptions) CodexTicketHarvestOptions {
	fallback = normalizeCodexTicketHarvestFallback(fallback)
	keys, supported := codexTicketHarvestOptionKeys(mode)
	if s == nil || s.settingRepo == nil || !supported {
		return fallback
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return fallback
	}
	cache, flight := s.codexTicketHarvestOptionsCache(mode)
	if cached, ok := cache.Load().(*cachedCodexTicketHarvestOptions); ok && time.Now().UnixNano() < cached.expiresAt {
		return cloneCodexTicketHarvestOptions(cached.options)
	}
	ch := flight.DoChan(mode, func() (any, error) {
		// One caller may leave while others still need this shared read. Each
		// waiter observes its own cancellation below; retries share this budget.
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for {
			snapshot := cache.Load()
			if cached, ok := snapshot.(*cachedCodexTicketHarvestOptions); ok && time.Now().UnixNano() < cached.expiresAt {
				return cached.options, nil
			}
			values, err := s.settingRepo.GetMultiple(dbCtx, keys)
			if dbCtx.Err() != nil {
				err = dbCtx.Err()
			}
			if err != nil && !errors.Is(err, ErrSettingNotFound) {
				if cached, ok := cache.Load().(*cachedCodexTicketHarvestOptions); ok && cached.initialized {
					return cached.options, nil
				}
				return fallback, nil
			}
			options := parseCodexTicketHarvestOptions(values, keys, fallback)
			if cache.CompareAndSwap(snapshot, &cachedCodexTicketHarvestOptions{options: options, initialized: true, expiresAt: time.Now().Add(5 * time.Second).UnixNano()}) {
				return options, nil
			}
			// A committed save fenced this query. Read the current generation;
			// its old result must reach neither the cache nor an active waiter.
		}
	})
	select {
	case <-ctx.Done():
		return fallback
	case result := <-ch:
		if options, ok := result.Val.(CodexTicketHarvestOptions); ok && result.Err == nil {
			return cloneCodexTicketHarvestOptions(options)
		}
		return fallback
	}
}

func (s *SettingService) InvalidateOpenAICodexTicketHarvestOptions(mode string) {
	if s == nil {
		return
	}
	if _, ok := codexTicketHarvestOptionKeys(mode); !ok {
		return
	}
	cache, flight := s.codexTicketHarvestOptionsCache(mode)
	next := &cachedCodexTicketHarvestOptions{}
	if previous, ok := cache.Load().(*cachedCodexTicketHarvestOptions); ok {
		*next = *previous
	}
	next.expiresAt = 0
	cache.Store(next)
	flight.Forget(mode)
}
