//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorLowTokenStreamingMergeProbe(t *testing.T) {
	for _, provider := range []string{MonitorProviderOpenAI, MonitorProviderGrok, MonitorProviderAnthropic, MonitorProviderKimi, MonitorProviderZhipu, MonitorProviderDeepseek, MonitorProviderMiniMax} {
		t.Run(provider, func(t *testing.T) {
			swapMonitorHTTPClient(t)
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer r.Body.Close()
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["stream"] != true || r.Header.Get("Accept") != "text/event-stream" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				answer := answerFromOpenAIRequest(body)
				w.Header().Set("Content-Type", "text/event-stream")
				if provider == MonitorProviderAnthropic {
					fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
					fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"ignore\"}}\n\n")
					fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", answer)
				} else {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: [DONE]\n\n", answer)
				}
			}))
			t.Cleanup(server.Close)
			result := runCheckForModel(context.Background(), provider, server.URL, "probe-key", "configured-model", &CheckOptions{
				BodyOverrideMode: MonitorBodyOverrideModeMerge,
				BodyOverride:     map[string]any{"stream": true, "max_tokens": 16},
			})
			require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
			require.Equal(t, "configured-model", body["model"])
			require.Equal(t, float64(16), body["max_tokens"])
		})
	}
}

func TestMonitorAnthropicStreamingInitialText(t *testing.T) {
	text := extractMonitorSSEText([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"4\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"2\"}}\n\n"), MonitorProviderAnthropic, "")
	require.Equal(t, "42", text)
}

func TestMonitorStreamingProbeRejectsTerminalErrors(t *testing.T) {
	for _, tc := range []struct{ provider, apiMode, terminal string }{
		{MonitorProviderAnthropic, "", `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`},
		{MonitorProviderOpenAI, MonitorAPIModeChatCompletions, `{"error":{"message":"overloaded"}}`},
		{MonitorProviderOpenAI, MonitorAPIModeResponses, `{"type":"response.failed","response":{"error":{"message":"overloaded"}}}`},
	} {
		t.Run(tc.provider+tc.apiMode, func(t *testing.T) {
			swapMonitorHTTPClient(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				_ = r.Body.Close()
				answer := answerFromOpenAIRequest(body)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: %s\n\n", answer, tc.terminal)
			}))
			t.Cleanup(server.Close)
			result := runCheckForModel(context.Background(), tc.provider, server.URL, "test-key", "configured-model", &CheckOptions{
				APIMode: tc.apiMode, BodyOverrideMode: MonitorBodyOverrideModeMerge, BodyOverride: map[string]any{"stream": true},
			})
			require.Equal(t, MonitorStatusError, result.Status, "a failed stream must not mark a channel available")
			require.Contains(t, result.Message, "overloaded")
		})
	}
}
