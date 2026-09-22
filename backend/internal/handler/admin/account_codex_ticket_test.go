package admin

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountResponseCodexTicketsUsesConfiguredPolicy(t *testing.T) {
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	h := &AccountHandler{cfg: &config.Config{}}
	require.Empty(t, h.accountResponseFromService(account).CodexTurnTickets)
	require.Empty(t, h.accountListResponseFromService(account).CodexTurnTickets)
	h.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"configured-model"}, FailClosed: false}
	status := h.accountListResponseFromService(account).CodexTurnTickets
	require.Len(t, status, 1)
	require.Equal(t, "configured-model", status[0].Model)
	require.False(t, status[0].Blocked)
	h.cfg.Gateway.OpenAICodexTicket.FailClosed = true
	require.True(t, h.accountResponseFromService(account).CodexTurnTickets[0].Blocked)
}

func TestAccountResponseCodexTicketsReadsLiveSettingsAfterRestart(t *testing.T) {
	cfg := &config.Config{}
	repo := &settingHandlerRepoStub{values: map[string]string{service.SettingKeyOpenAICodexTicketEnabled: "true"}}
	settings := service.NewSettingService(repo, cfg)
	h := &AccountHandler{cfg: cfg}
	h.SetCodexTicketSettings(settings)
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeSetupToken}
	require.Len(t, h.accountListResponseFromService(account).CodexTurnTickets, 2)
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled)
	repo.values[service.SettingKeyOpenAICodexTicketEnabled] = "false"
	settings.InvalidateOpenAICodexTicketEnabledCache()
	require.Empty(t, h.accountResponseFromService(account).CodexTurnTickets)
}

func TestAccountResponseCodexTicketsUsesSelected332PolicyAndLiveFailClosed(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"official-model"}}
	cfg.Gateway.OpenAICodexTicket332 = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: false, Models: []string{"gpt-6-astra"}}
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	settings := service.NewSettingService(repo, cfg)
	h := &AccountHandler{cfg: cfg}
	h.SetCodexTicketSettings(settings)
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account"},
		Extra:       map[string]any{"codex_ticket_mode": "332"}}
	statuses := h.accountResponseFromService(account).CodexTurnTickets
	require.Len(t, statuses, 1)
	require.Equal(t, "332", statuses[0].Mode)
	require.Equal(t, "gpt-6-astra", statuses[0].Model)
	require.False(t, statuses[0].Blocked)
	repo.values[service.SettingKeyOpenAICodexTicket332FailClosed] = "true"
	settings.InvalidateOpenAICodexTicket332FailClosedCache()
	require.True(t, h.accountListResponseFromService(account).CodexTurnTickets[0].Blocked)
	issued := time.Now().Truncate(time.Second)
	material := make([]byte, 249)
	material[0] = 0x80
	binary.BigEndian.PutUint64(material[1:9], uint64(issued.Unix()))
	account.Extra["codex_turn_ticket:332:gpt-6-astra"] = map[string]any{
		"state": base64.URLEncoding.EncodeToString(material), "length": 332,
		"issued_at": issued, "expires_at": issued.Add(50 * time.Minute),
		"credential_hash": service.OpenAICodexTicketCredentialHash(account),
	}
	statuses = h.accountResponseFromService(account).CodexTurnTickets
	require.True(t, statuses[0].Ready)
	require.False(t, statuses[0].Blocked)
	account.Extra["codex_ticket_mode"] = "off"
	require.Empty(t, h.accountResponseFromService(account).CodexTurnTickets)
}
