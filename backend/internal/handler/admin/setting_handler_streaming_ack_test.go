package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type streamingACKAdminRepo struct {
	service.SettingRepository
	value    string
	exists   bool
	readErr  error
	writeErr error
}

func (r *streamingACKAdminRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	if !r.exists {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}

func (r *streamingACKAdminRepo) Set(ctx context.Context, key, value string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.value, r.exists = value, true
	return nil
}

func streamingACKAdminRequest(h *SettingHandler, method, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/streaming-ack", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPut {
		h.UpdateStreamingACKSettings(c)
	} else {
		h.GetStreamingACKSettings(c)
	}
	return recorder
}

func TestStreamingACKSettingsRequiresExplicitBoolean(t *testing.T) {
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"true"}`, `{"enabled":1}`, `null`} {
		t.Run(body, func(t *testing.T) {
			repo := &streamingACKAdminRepo{exists: true, value: "true"}
			h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)
			recorder := streamingACKAdminRequest(h, http.MethodPut, body)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, "true", repo.value)
		})
	}
}

func TestStreamingACKSettingsAdminReadAndWrite(t *testing.T) {
	repo := &streamingACKAdminRepo{}
	cfg := &config.Config{Gateway: config.GatewayConfig{SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{Enabled: true}}}
	svc := service.NewSettingService(repo, cfg)
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	initial := streamingACKAdminRequest(h, http.MethodGet, "")
	require.Equal(t, http.StatusOK, initial.Code)
	require.Contains(t, initial.Body.String(), `"enabled":true`)

	for _, value := range []string{"false", "true"} {
		updated := streamingACKAdminRequest(h, http.MethodPut, `{"enabled":`+value+`}`)
		require.Equal(t, http.StatusOK, updated.Code)
		require.Contains(t, updated.Body.String(), `"enabled":`+value)
		require.Equal(t, value, repo.value)
		require.Equal(t, value == "true", svc.IsStreamingACKEnabled(context.Background()))
	}
	require.True(t, cfg.Gateway.SyntheticFirstResponse.Enabled)
}

func TestStreamingACKSettingsAdminReturnsErrors(t *testing.T) {
	repo := &streamingACKAdminRepo{readErr: errors.New("database offline")}
	svc := service.NewSettingService(repo, &config.Config{})
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	read := streamingACKAdminRequest(h, http.MethodGet, "")
	require.Equal(t, http.StatusInternalServerError, read.Code)

	repo.readErr = nil
	repo.writeErr = errors.New("write failed")
	write := streamingACKAdminRequest(h, http.MethodPut, `{"enabled":true}`)
	require.Equal(t, http.StatusInternalServerError, write.Code)
	require.False(t, repo.exists)
	require.False(t, svc.IsStreamingACKEnabled(context.Background()))
}
