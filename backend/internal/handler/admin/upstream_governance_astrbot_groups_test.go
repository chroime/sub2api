package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGovernanceAstrBotGroupsEndpointReadsLiveNamesAndKeepsDraftPrivate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer handler-secret", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"status":"ok","data":{"complete":true,"groups":[{"bot_id":"Bot","umo":"Bot:GroupMessage:123","group_id":"123","group_name":"Current group"}]}}`))
	}))
	defer upstream.Close()
	store := &globalSettingsHandlerStore{}
	r, svc := globalSettingsTestRouter(t, store, true)
	r.POST("/astrbot-groups", NewUpstreamGovernanceHandler(svc).AstrBotGroups)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/astrbot-groups", strings.NewReader(`{"base_url":"`+upstream.URL+`","api_key":"handler-secret","timeout_seconds":5}`)))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Contains(t, response.Body.String(), `"group_name":"Current group"`)
	require.NotContains(t, response.Body.String(), "handler-secret")
	require.Empty(t, store.value)
}

func TestGovernanceAstrBotGroupsEndpointReturnsStablePrivateErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("upstream-secret-canary"))
	}))
	defer upstream.Close()
	r, svc := globalSettingsTestRouter(t, &globalSettingsHandlerStore{}, true)
	r.POST("/astrbot-groups", NewUpstreamGovernanceHandler(svc).AstrBotGroups)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/astrbot-groups", strings.NewReader(`{"base_url":"`+upstream.URL+`","api_key":"handler-secret","timeout_seconds":5}`)))
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Contains(t, response.Body.String(), `"reason":"astrbot_scope_required"`)
	require.NotContains(t, response.Body.String(), "upstream-secret-canary")
	require.NotContains(t, response.Body.String(), "handler-secret")
}
