package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCompactSelectionUsesActualOutboundModel(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		for _, route := range []string{"legacy", "load", "advanced"} {
			t.Run(mode+"/"+route, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				account := ticketTestAccount(41)
				account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 2
				account.Extra = map[string]any{"codex_ticket_mode": mode, "openai_compact_mode": OpenAICompactModeForceOn}
				svc := &OpenAIGatewayService{
					cfg: &config.Config{Gateway: config.GatewayConfig{
						OpenAICompactModel:   "gpt-5.5",
						OpenAICodexTicket:    config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
						OpenAICodexTicket332: config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true},
					}},
					accountRepo: stubOpenAIAccountRepo{accounts: []Account{*account}}, cache: &stubGatewayCache{},
				}
				if route != "legacy" {
					svc.cfg.Gateway.Scheduling.LoadBatchEnabled = true
					svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
				}
				var selected *AccountSelectionResult
				var err error
				if route == "advanced" {
					svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
					selected, _, err = svc.SelectAccountWithScheduler(context.Background(), nil, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, true)
				} else {
					selected, err = svc.selectAccountWithLoadAwareness(context.Background(), nil, PlatformOpenAI, "", "gpt-6-astra", nil, true, "", true)
				}
				require.NoError(t, err, "compact uses ungated gpt-5.5 even when requested gpt-6-astra has no ticket")
				require.NotNil(t, selected)
				require.Equal(t, account.ID, selected.Account.ID)
				if selected.ReleaseFunc != nil {
					selected.ReleaseFunc()
				}
				require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra", false), "ordinary requests still require a ticket")
			})
		}
	}
}
