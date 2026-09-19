package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	openAICodexTicketMode292 = "292"
	openAICodexTicketMode332 = "332"
	openAICodexTicketModeOff = "off"
)

// OpenAICodexTicketMode selects an explicitly configured protocol, never a plan
// inferred from credentials. Only an absent key inherits official 292 behavior.
func OpenAICodexTicketMode(account *Account) string {
	if !isOpenAICodexTicketAccount(account) {
		return openAICodexTicketModeOff
	}
	raw, exists := account.Extra["codex_ticket_mode"]
	if !exists {
		return openAICodexTicketMode292
	}
	mode, ok := raw.(string)
	if ok {
		switch mode {
		case openAICodexTicketMode292:
			return openAICodexTicketMode292
		case openAICodexTicketMode332:
			return openAICodexTicketMode332
		}
	}
	return openAICodexTicketModeOff
}

func openAICodexTicketTargetLength(mode string) int {
	switch mode {
	case openAICodexTicketMode292:
		return 292
	case openAICodexTicketMode332:
		return 332
	default:
		return 0
	}
}

func openAICodexTicketModeKey(mode string, accountID int64, model string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", mode, accountID, strings.TrimSpace(model))
}

func openAICodexTicketModeExtraKey(mode, model string) string {
	return openAICodexTicketExtraKeyPrefix + mode + ":" + strings.TrimSpace(model)
}

func (s *OpenAIGatewayService) openAICodexTicketConfigForMode(mode string) config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{FailClosed: mode == openAICodexTicketMode292}
	if s != nil && s.cfg != nil {
		switch mode {
		case openAICodexTicketMode292:
			cfg = s.cfg.Gateway.OpenAICodexTicket
		case openAICodexTicketMode332:
			cfg = s.cfg.Gateway.OpenAICodexTicket332
		}
	}
	// Named protocols have fixed lengths even if a legacy config overrides one.
	cfg.TargetLength = openAICodexTicketTargetLength(mode)
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = 3600
	}
	if cfg.RefreshBeforeSeconds <= 0 {
		cfg.RefreshBeforeSeconds = 600
	}
	if cfg.HarvestProbeIntervalSeconds <= 0 {
		cfg.HarvestProbeIntervalSeconds = 6
	}
	if cfg.HarvestAttemptTimeoutSeconds <= 0 {
		cfg.HarvestAttemptTimeoutSeconds = 25
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	return cfg
}

func (s *OpenAIGatewayService) openAICodexTicketRuntimeConfig(ctx context.Context, mode string) config.OpenAICodexTicketConfig {
	cfg := s.openAICodexTicketConfigForMode(mode)
	if cfg.TargetLength == 0 || s == nil {
		cfg.Enabled = false
		return cfg
	}
	if s.settingService != nil {
		if mode == openAICodexTicketMode332 {
			cfg.Enabled = s.settingService.GetOpenAICodexTicket332Enabled(ctx, cfg.Enabled)
			cfg.FailClosed = s.settingService.GetOpenAICodexTicket332FailClosed(ctx, cfg.FailClosed)
		} else {
			cfg.Enabled = s.settingService.GetOpenAICodexTicketEnabled(ctx, cfg.Enabled)
			cfg.FailClosed = s.settingService.GetOpenAICodexTicketFailClosed(ctx, cfg.FailClosed)
		}
	}
	return cfg
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyForMode(ctx context.Context, mode string) string {
	if s != nil && s.settingService != nil {
		var proxy string
		switch mode {
		case openAICodexTicketMode332:
			proxy = s.settingService.GetOpenAICodexTicket332HarvestProxyURL(ctx)
		case openAICodexTicketMode292:
			proxy = s.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx)
		}
		if proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(s.openAICodexTicketConfigForMode(mode).HarvestProxyURL)
}

func openAICodexTicketGatesModel(cfg config.OpenAICodexTicketConfig, model string) bool {
	model = normalizeOpenAICodexTicketModel(model)
	if !cfg.Enabled || model == "" {
		return false
	}
	for _, candidate := range cfg.Models {
		if normalizeOpenAICodexTicketModel(candidate) == model {
			return true
		}
	}
	return false
}
