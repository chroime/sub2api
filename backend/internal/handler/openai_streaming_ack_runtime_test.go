package handler

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type streamingACKHandlerRepo struct {
	service.SettingRepository
	mu    sync.Mutex
	value string
}

func (r *streamingACKHandlerRepo) GetValue(ctx context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value, nil
}

func (r *streamingACKHandlerRepo) Set(ctx context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value = value
	return nil
}

func TestStartSyntheticFirstResponseUsesRuntimeSwitchAndRetainsScope(t *testing.T) {
	for _, tc := range []struct {
		name           string
		configEnabled  bool
		runtimeValue   string
		accountEnabled bool
		group          int64
		stream         bool
		wantACK        bool
	}{
		{name: "runtime enable overrides disabled config", runtimeValue: "true", accountEnabled: true, group: 7, stream: true, wantACK: true},
		{name: "runtime disable overrides enabled config", configEnabled: true, runtimeValue: "false", accountEnabled: true, group: 7, stream: true},
		{name: "account opt in still required", runtimeValue: "true", group: 7, stream: true},
		{name: "group match still required", runtimeValue: "true", accountEnabled: true, group: 8, stream: true},
		{name: "non streaming remains untouched", runtimeValue: "true", accountEnabled: true, group: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Gateway: config.GatewayConfig{
				SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
					Enabled:                  tc.configEnabled,
					MinDelayMs:               5,
					MaxDelayMs:               5,
					UnderOneSecondPercent:    100,
					UnderOneSecondMaxDelayMs: 5,
				},
			}}
			settings := service.NewSettingService(&streamingACKHandlerRepo{value: tc.runtimeValue}, cfg)
			gateway := service.NewOpenAIGatewayService(
				nil, nil, nil, nil, nil, nil, nil, cfg,
				nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil,
			)
			h := &OpenAIGatewayHandler{cfg: cfg, gatewayService: gateway}
			account := syntheticFirstResponseEnabledAccount(7)
			account.Extra[service.OpenAISyntheticFirstResponseEnabledExtraKey] = tc.accountEnabled
			c, recorder := syntheticFirstResponseHandlerContext()
			stop := h.startSyntheticFirstResponse(c, tc.stream, time.Now(), account, &tc.group)
			t.Cleanup(stop)
			if tc.wantACK {
				require.Eventually(t, func() bool { return service.OpenAISyntheticFirstResponseCommitted(c) }, time.Second, time.Millisecond)
			} else {
				time.Sleep(20 * time.Millisecond)
			}
			stop()
			if tc.wantACK {
				require.Equal(t, ":\n\n", recorder.Body.String())
			} else {
				require.Empty(t, recorder.Body.String())
			}
		})
	}
}

func TestStreamingACKRuntimeSwitchOverHTTP(t *testing.T) {
	const semanticEvent = "data: {\"text\":\"hello\"}\n\n"
	const ackDelay = 200 * time.Millisecond
	var upstreamProduced atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fast") != "true" {
			timer := time.NewTimer(500 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		upstreamProduced.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, semanticEvent)
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{Gateway: config.GatewayConfig{
		SyntheticFirstResponse: config.GatewaySyntheticFirstResponseConfig{
			MinDelayMs:               int(ackDelay.Milliseconds()),
			MaxDelayMs:               int(ackDelay.Milliseconds()),
			UnderOneSecondPercent:    100,
			UnderOneSecondMaxDelayMs: int(ackDelay.Milliseconds()),
		},
	}}
	settings := service.NewSettingService(&streamingACKHandlerRepo{value: "false"}, cfg)
	gateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil,
	)
	h := &OpenAIGatewayHandler{cfg: cfg, gatewayService: gateway}
	settingsHandler := admin.NewSettingHandler(settings, nil, nil, nil, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/api/v1/admin/settings/streaming-ack", settingsHandler.UpdateStreamingACKSettings)
	router.POST("/v1/responses", func(c *gin.Context) {
		groupID := int64(7)
		stop := h.startSyntheticFirstResponse(c, true, time.Now(), syntheticFirstResponseEnabledAccount(groupID), &groupID)
		defer stop()
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream.URL+"?fast="+c.Query("fast"), nil)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		resp, err := upstream.Client().Do(request)
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(c.Writer, resp.Body)
		c.Writer.Flush()
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 3 * time.Second

	setEnabled := func(enabled string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/admin/settings/streaming-ack", strings.NewReader(`{"enabled":`+enabled+`}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Contains(t, string(body), `"enabled":`+enabled)
	}
	requestStream := func(fast, wantACK bool) {
		t.Helper()
		upstreamProduced.Store(false)
		path := "/v1/responses"
		if fast {
			path += "?fast=true"
		}
		start := time.Now()
		resp, err := client.Post(server.URL+path, "application/json", strings.NewReader(`{"stream":true}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		reader := bufio.NewReader(resp.Body)
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if wantACK {
			require.Equal(t, ":\n", line)
			require.False(t, upstreamProduced.Load(), "ACK must arrive before the delayed upstream emits bytes")
		} else {
			require.Equal(t, "data: {\"text\":\"hello\"}\n", line)
		}
		if fast {
			require.Less(t, time.Since(start), ackDelay, "fast semantic bytes must not wait for the ACK timer")
		}
		rest, err := io.ReadAll(reader)
		require.NoError(t, err)
		want := semanticEvent
		if wantACK {
			want = ":\n\n" + want
		}
		require.Equal(t, want, line+string(rest))
	}

	requestStream(false, false)
	setEnabled("true")
	requestStream(false, true)
	requestStream(true, false)
	setEnabled("false")
	requestStream(false, false)
}
