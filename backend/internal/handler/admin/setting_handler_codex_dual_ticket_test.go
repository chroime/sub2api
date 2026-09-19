package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func codexDualTicketResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

func TestSettingsCodexDualTicketIndependentWriteAndRead(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	ctx := context.Background()
	// Warm every cache before the update so stale settings cannot hide behind
	// the first runtime read after saving.
	require.False(t, h.settingService.GetOpenAICodexTicketEnabled(ctx, false))
	require.True(t, h.settingService.GetOpenAICodexTicketFailClosed(ctx, true))
	require.True(t, h.settingService.GetOpenAICodexTicket332Enabled(ctx, true))
	require.False(t, h.settingService.GetOpenAICodexTicket332FailClosed(ctx, false))
	require.Empty(t, h.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx))
	require.Empty(t, h.settingService.GetOpenAICodexTicket332HarvestProxyURL(ctx))
	values := map[string]any{
		"openai_codex_ticket_enabled":               true,
		"openai_codex_ticket_fail_closed":           false,
		"openai_codex_ticket_harvest_proxy_url":     "http://user:292-secret@first.example:8080",
		"openai_codex_ticket_332_enabled":           false,
		"openai_codex_ticket_332_fail_closed":       true,
		"openai_codex_ticket_332_harvest_proxy_url": "socks5h://user:332-secret@second.example:1080",
	}
	recorder := doUpdateSettings(t, h, values, nil)
	response := codexDualTicketResponse(t, recorder)
	for key, value := range values {
		switch typed := value.(type) {
		case bool:
			require.Equal(t, typed, response[key], key)
			if typed {
				require.Equal(t, "true", repo.values[key], key)
			} else {
				require.Equal(t, "false", repo.values[key], key)
			}
		case string:
			require.Equal(t, typed, repo.values[key], key)
			require.Equal(t, service.MaskProxyURL(typed), response[key], key)
		}
	}
	require.Equal(t, true, response["openai_codex_ticket_harvest_proxy_configured"])
	require.Equal(t, true, response["openai_codex_ticket_332_harvest_proxy_configured"])
	require.NotContains(t, recorder.Body.String(), "292-secret")
	require.NotContains(t, recorder.Body.String(), "332-secret")
	require.True(t, h.settingService.GetOpenAICodexTicketEnabled(ctx, false))
	require.False(t, h.settingService.GetOpenAICodexTicketFailClosed(ctx, true))
	require.False(t, h.settingService.GetOpenAICodexTicket332Enabled(ctx, true))
	require.True(t, h.settingService.GetOpenAICodexTicket332FailClosed(ctx, false))
	require.Equal(t, values["openai_codex_ticket_harvest_proxy_url"], h.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx))
	require.Equal(t, values["openai_codex_ticket_332_harvest_proxy_url"], h.settingService.GetOpenAICodexTicket332HarvestProxyURL(ctx))
	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	getResponse := codexDualTicketResponse(t, get)
	require.Equal(t, response["openai_codex_ticket_harvest_proxy_url"], getResponse["openai_codex_ticket_harvest_proxy_url"])
	require.Equal(t, response["openai_codex_ticket_332_harvest_proxy_url"], getResponse["openai_codex_ticket_332_harvest_proxy_url"])
	require.NotContains(t, get.Body.String(), "292-secret")
	require.NotContains(t, get.Body.String(), "332-secret")
}

func TestSettingsCodexDualTicketPartialUpdatesPreserveOtherModeAndSecrets(t *testing.T) {
	initial := map[string]string{
		"openai_codex_ticket_enabled":               "true",
		"openai_codex_ticket_fail_closed":           "true",
		"openai_codex_ticket_harvest_proxy_url":     "http://user:292-secret@first.example:8080",
		"openai_codex_ticket_332_enabled":           "false",
		"openai_codex_ticket_332_fail_closed":       "false",
		"openai_codex_ticket_332_harvest_proxy_url": "socks5h://user:332-secret@second.example:1080",
	}
	h, repo := newStepUpSwitchTestHandler(t, initial)
	recorder := doUpdateSettings(t, h, map[string]any{"openai_codex_ticket_332_enabled": true}, nil)
	codexDualTicketResponse(t, recorder)
	require.Equal(t, "true", repo.values["openai_codex_ticket_332_enabled"])
	for _, payload := range []map[string]any{
		{"site_name": "independent settings"},
		{"openai_codex_ticket_harvest_proxy_url": "", "openai_codex_ticket_332_harvest_proxy_url": ""},
		{"openai_codex_ticket_harvest_proxy_url": service.MaskProxyURL(initial["openai_codex_ticket_harvest_proxy_url"]), "openai_codex_ticket_332_harvest_proxy_url": service.MaskProxyURL(initial["openai_codex_ticket_332_harvest_proxy_url"])},
	} {
		codexDualTicketResponse(t, doUpdateSettings(t, h, payload, nil))
		require.Equal(t, "true", repo.values["openai_codex_ticket_enabled"])
		require.Equal(t, "true", repo.values["openai_codex_ticket_fail_closed"])
		require.Equal(t, "true", repo.values["openai_codex_ticket_332_enabled"])
		require.Equal(t, "false", repo.values["openai_codex_ticket_332_fail_closed"])
		require.Equal(t, "http://user:292-secret@first.example:8080", repo.values["openai_codex_ticket_harvest_proxy_url"])
		require.Equal(t, "socks5h://user:332-secret@second.example:1080", repo.values["openai_codex_ticket_332_harvest_proxy_url"])
	}
}

func TestSettingsCodexDualTicketDefaultsAndMaskedGet(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	h.settingService = service.NewSettingService(repo, &config.Config{Gateway: config.GatewayConfig{
		OpenAICodexTicket: config.OpenAICodexTicketConfig{FailClosed: true},
	}})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	response := codexDualTicketResponse(t, recorder)
	require.Equal(t, false, response["openai_codex_ticket_enabled"])
	require.Equal(t, true, response["openai_codex_ticket_fail_closed"])
	require.Equal(t, false, response["openai_codex_ticket_332_enabled"])
	require.Equal(t, false, response["openai_codex_ticket_332_fail_closed"])
	for _, key := range []string{"openai_codex_ticket_harvest_proxy_url", "openai_codex_ticket_332_harvest_proxy_url"} {
		require.Equal(t, "", response[key])
	}
	for _, key := range []string{"openai_codex_ticket_harvest_proxy_configured", "openai_codex_ticket_332_harvest_proxy_configured"} {
		require.Equal(t, false, response[key])
	}
}

func TestSettingsCodexDualTicketInvalidProxyIsAtomic(t *testing.T) {
	for _, key := range []string{"openai_codex_ticket_harvest_proxy_url", "openai_codex_ticket_332_harvest_proxy_url"} {
		t.Run(key, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{key: "http://previous.example:8080", "openai_codex_ticket_enabled": "false"})
			recorder := doUpdateSettings(t, h, map[string]any{key: "ftp://user:invalid-secret@proxy.example:21", "openai_codex_ticket_enabled": true}, nil)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), "invalid-secret")
			require.Equal(t, "http://previous.example:8080", repo.values[key])
			require.Equal(t, "false", repo.values["openai_codex_ticket_enabled"])
		})
	}
}

func TestSettingsCodexDualTicketAuditRecordsKeysWithoutSecrets(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	before, err := h.settingService.GetAllSettings(context.Background())
	require.NoError(t, err)
	values := map[string]string{
		"openai_codex_ticket_enabled":               "true",
		"openai_codex_ticket_fail_closed":           "true",
		"openai_codex_ticket_harvest_proxy_url":     "http://user:292-secret@first.example:8080",
		"openai_codex_ticket_332_enabled":           "true",
		"openai_codex_ticket_332_fail_closed":       "true",
		"openai_codex_ticket_332_harvest_proxy_url": "http://user:332-secret@second.example:8080",
	}
	for key, value := range values {
		repo.values[key] = value
	}
	after, err := h.settingService.GetAllSettings(context.Background())
	require.NoError(t, err)
	changed := diffSettings(before, after, nil, nil, UpdateSettingsRequest{})
	for key := range values {
		require.Contains(t, changed, key)
	}
	raw, err := json.Marshal(changed)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
	var req UpdateSettingsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"openai_codex_ticket_harvest_proxy_url":"http://user:292-secret@first.example:8080","openai_codex_ticket_332_harvest_proxy_url":"http://user:332-secret@second.example:8080"}`), &req))
	raw, err = json.Marshal(settingsAuditRequest(req))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "292-secret")
	require.NotContains(t, string(raw), "332-secret")
}
