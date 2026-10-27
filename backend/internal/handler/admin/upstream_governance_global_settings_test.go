package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func globalSettingsTestRouter(t *testing.T, store *globalSettingsHandlerStore, durable bool) (*gin.Engine, *gov.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cipher, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	svc := gov.NewService(nil, nil, nil, cipher, durable)
	svc.SetGlobalSettingsStore(store)
	h := NewUpstreamGovernanceHandler(svc)
	r := gin.New()
	r.GET("/global-settings", h.GlobalSettings)
	r.PUT("/global-settings", h.SaveGlobalSettings)
	return r, svc
}

func assertGlobalSettingsSecretResponse(t *testing.T, response *httptest.ResponseRecorder, configured bool, secrets ...string) {
	t.Helper()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var payload struct {
		Data struct {
			AstrBot map[string]json.RawMessage `json:"astrbot"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.JSONEq(t, `""`, string(payload.Data.AstrBot["api_key"]))
	require.Equal(t, configured, string(payload.Data.AstrBot["api_key_configured"]) == "true")
	require.Contains(t, payload.Data.AstrBot, "api_key_configured")
	require.NotContains(t, payload.Data.AstrBot, "api_key_ciphertext")
	require.NotContains(t, payload.Data.AstrBot, "clear_api_key")
	for _, secret := range secrets {
		require.NotContains(t, response.Body.String(), secret)
	}
}

func TestGovernanceGlobalSettingsRoutesKeepKeysWriteOnly(t *testing.T) {
	store := &globalSettingsHandlerStore{}
	r, svc := globalSettingsTestRouter(t, store, true)

	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/global-settings", nil))
	assertGlobalSettingsSecretResponse(t, get, false)
	require.Contains(t, get.Body.String(), `"version":1`)

	body := `{"version":1,"notifications":{"enabled":true,"recipients":["ops@example.com"],"events":{"upstream_group_change":true,"upstream_rate_change":true,"pricing_change":true,"protection_change":true,"local_group_change":true,"health_change":true}},"local_monitor":{"enabled":true,"interval_seconds":5},"defaults":{"fast_interval_seconds":5,"full_interval_seconds":60},"astrbot":{"enabled":false,"base_url":"","endpoint_path":"/api/message","api_key":"astr-secret","targets":[],"timeout_seconds":15}}`
	save := httptest.NewRecorder()
	r.ServeHTTP(save, httptest.NewRequest(http.MethodPut, "/global-settings", strings.NewReader(body)))
	assertGlobalSettingsSecretResponse(t, save, true, "astr-secret")
	require.NotContains(t, store.value, "astr-secret")
	require.Contains(t, store.value, "api_key_ciphertext")
	require.NotContains(t, save.Body.String(), `"astra":`)
	read := httptest.NewRecorder()
	r.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/global-settings", nil))
	assertGlobalSettingsSecretResponse(t, read, true, "astr-secret")
	runtime, err := svc.GetRuntimeGlobalSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "astr-secret", runtime.AstrBot.APIKey)

	conflict := httptest.NewRecorder()
	r.ServeHTTP(conflict, httptest.NewRequest(http.MethodPut, "/global-settings", strings.NewReader(body)))
	require.Equal(t, http.StatusConflict, conflict.Code)
}

func TestGovernanceGlobalSettingsRoutesPreserveReplaceAndClearKeys(t *testing.T) {
	store := &globalSettingsHandlerStore{value: `{"version":1,"astrbot":{"api_key":"legacy-admin-secret"}}`}
	r, svc := globalSettingsTestRouter(t, store, true)
	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/global-settings", nil))
	assertGlobalSettingsSecretResponse(t, get, true, "legacy-admin-secret")

	for _, tt := range []struct {
		body string
		key  string
	}{
		{`{"version":1,"astrbot":{}}`, "legacy-admin-secret"},
		{`{"version":2,"astrbot":{"api_key":""}}`, "legacy-admin-secret"},
		{`{"version":3,"astrbot":{"api_key":"replacement-admin-secret"}}`, "replacement-admin-secret"},
		{`{"version":4,"astrbot":{"clear_api_key":true}}`, ""},
	} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/global-settings", strings.NewReader(tt.body)))
		assertGlobalSettingsSecretResponse(t, response, tt.key != "", "legacy-admin-secret", "replacement-admin-secret")
		require.NotContains(t, store.value, "legacy-admin-secret")
		require.NotContains(t, store.value, "replacement-admin-secret")
		runtime, err := svc.GetRuntimeGlobalSettings(t.Context())
		require.NoError(t, err)
		require.Equal(t, tt.key, runtime.AstrBot.APIKey)
	}
}

func TestGovernanceGlobalSettingsRoutesRejectForgedCiphertext(t *testing.T) {
	store := &globalSettingsHandlerStore{value: `{"version":1,"astrbot":{"api_key":"original-admin-secret"}}`}
	r, _ := globalSettingsTestRouter(t, store, true)
	original := store.value
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/global-settings", strings.NewReader(`{"version":1,"astrbot":{"api_key_ciphertext":"forged-ciphertext"}}`)))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, original, store.value)
	require.NotContains(t, response.Body.String(), "forged-ciphertext")
	require.NotContains(t, response.Body.String(), "original-admin-secret")
}

func TestGovernanceGlobalSettingsRoutesRequireDurableEncryption(t *testing.T) {
	store := &globalSettingsHandlerStore{}
	r, _ := globalSettingsTestRouter(t, store, false)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/global-settings", strings.NewReader(`{"version":1,"astrbot":{"api_key":"unsavable-admin-secret"}}`)))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), "unsavable-admin-secret")
	require.Empty(t, store.value)
}

type globalSettingsHandlerStore struct{ value string }

func (s *globalSettingsHandlerStore) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.value == "" || len(keys) == 0 {
		return map[string]string{}, nil
	}
	return map[string]string{keys[0]: s.value}, nil
}

func (s *globalSettingsHandlerStore) SetMultiple(_ context.Context, values map[string]string) error {
	for _, value := range values {
		s.value = value
		break
	}
	return nil
}
