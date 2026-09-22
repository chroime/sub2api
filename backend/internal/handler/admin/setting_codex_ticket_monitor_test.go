package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexTicketMonitorStub struct {
	snapshot service.OpenAICodexTicketMonitorSnapshot
}

func (s codexTicketMonitorStub) GetOpenAICodexTicketMonitor() service.OpenAICodexTicketMonitorSnapshot {
	return s.snapshot
}

func TestCodexTicketMonitorSnapshotContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	proxyID := int64(7)
	h := NewSettingHandler(nil, nil, nil, nil, nil, nil, nil)
	h.SetCodexTicketMonitorService(codexTicketMonitorStub{service.OpenAICodexTicketMonitorSnapshot{
		UpdatedAt: now,
		States:    []service.OpenAICodexTicketMonitorState{{Mode: "292", AccountID: 8, AccountName: "test account", Model: "gpt-6-astra", Status: "ready", Phase: "persist", ProxyID: &proxyID, ProxyName: "test route", Length: 292, ExpiresAt: &now, LastError: "", UpdatedAt: now, Uses: 4, LastUsedAt: &now}},
		Events:    []service.OpenAICodexTicketMonitorEvent{{ID: 1, Time: now, Mode: "332", AccountID: 8, AccountName: "test account", Model: "gpt-6-astra", Status: "cooldown", Phase: "capture", ProxyID: &proxyID, ProxyName: "test route", HTTPStatus: 429, Length: 332, ErrorCode: "rate_limited", NextAttemptAt: &now}},
	}})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/codex-tickets/monitor", nil)
	h.GetCodexTicketMonitor(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"code":0,"message":"success","data":{
		"updated_at":"2026-09-20T01:02:03Z",
		"states":[{"mode":"292","account_id":8,"account_name":"test account","model":"gpt-6-astra","status":"ready","phase":"persist","proxy_id":7,"proxy_name":"test route","length":292,"expires_at":"2026-09-20T01:02:03Z","last_error":"","updated_at":"2026-09-20T01:02:03Z","uses":4,"last_used_at":"2026-09-20T01:02:03Z"}],
		"events":[{"id":1,"time":"2026-09-20T01:02:03Z","mode":"332","account_id":8,"account_name":"test account","model":"gpt-6-astra","status":"cooldown","phase":"capture","proxy_id":7,"proxy_name":"test route","http_status":429,"length":332,"error_code":"rate_limited","next_attempt_at":"2026-09-20T01:02:03Z"}]
	}}`, recorder.Body.String())
}

func TestCodexTicketMonitorEmptyRuntimeAndMissingDependency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, nil, nil, nil, nil, nil, nil)
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/codex-tickets/monitor", nil)
		h.GetCodexTicketMonitor(c)
		return recorder
	}
	require.Equal(t, http.StatusServiceUnavailable, request().Code)
	h.SetCodexTicketMonitorService(&service.OpenAIGatewayService{})
	response := request()
	require.Equal(t, http.StatusOK, response.Code)
	data := codexDualTicketResponse(t, response)
	require.Equal(t, []any{}, data["states"])
	require.Equal(t, []any{}, data["events"])
	require.NotEmpty(t, data["updated_at"])
}
