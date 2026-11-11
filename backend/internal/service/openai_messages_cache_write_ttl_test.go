//go:build unit

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIMessagesRetainsCacheWriteTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"buffered", "stream"} {
		for _, placement := range []string{"response", "event"} {
			t.Run(mode+"/"+placement, func(t *testing.T) {
				const usage = `{"input_tokens":400,"output_tokens":10,"cache_creation_input_tokens":300,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":300,"cache_write_tokens_details":{"ttl_30m_tokens":100,"ttl_12h_tokens":200}}}`
				response := map[string]any{"id": "resp_ttl_compat", "model": "gpt-6-luna", "status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]string{"type": "output_text", "text": "ok"}}}}}
				event := map[string]any{"type": "response.completed", "response": response}
				if placement == "response" {
					response["usage"] = json.RawMessage(usage)
				} else {
					event["usage"] = json.RawMessage(usage)
				}
				payload, err := json.Marshal(event)
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\n"))}
				svc := &OpenAIGatewayService{}
				handle := svc.handleAnthropicBufferedStreamingResponse
				if mode == "stream" {
					handle = svc.handleAnthropicStreamingResponse
				}
				result, err := handle(resp, c, &Account{ID: 40, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "client-alias", "gpt-6-luna", "gpt-6-luna", time.Now())
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "client-alias", result.Model)
				require.Contains(t, rec.Body.String(), "ok")
				require.Equal(t, 400, result.Usage.InputTokens)
				require.Equal(t, 300, result.Usage.CacheCreationInputTokens)
				require.Equal(t, 100, result.Usage.CacheCreation30mInputTokens)
				require.Equal(t, 200, result.Usage.CacheCreation12hInputTokens)
			})
		}
	}
}

func TestOpenAIMessagesTTLDoesNotOverrideCanonicalAggregate(t *testing.T) {
	typed := &apicompat.ResponsesUsage{InputTokens: 400, OutputTokens: 10, CacheCreationInputTokens: 300}
	for _, raw := range []OpenAIUsage{
		{CacheCreationInputTokens: 0, CacheCreation30mInputTokens: 100, CacheCreation12hInputTokens: 200},
		{CacheCreationInputTokens: 300, CacheCreation30mInputTokens: 100, CacheCreation12hInputTokens: 201},
		{CacheCreationInputTokens: 300, CacheCreation30mInputTokens: -1, CacheCreation12hInputTokens: 301},
	} {
		got := copyOpenAIUsageFromResponsesUsage(typed, raw)
		require.Equal(t, 300, got.CacheCreationInputTokens)
		require.Zero(t, got.CacheCreation30mInputTokens)
		require.Zero(t, got.CacheCreation12hInputTokens)
	}
}
