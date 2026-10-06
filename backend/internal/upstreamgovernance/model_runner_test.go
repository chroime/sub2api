package upstreamgovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func modelFixtureRunner(handler func(*http.Request) (*http.Response, error)) ModelRunner {
	return NewConnector(func(context.Context, Site) (HTTPDoer, error) { return connectorDoer(handler), nil }).(ModelRunner)
}

func modelFixtureResponse(body, contentType string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func modelFixtureRequest(mode string) ModelRunRequest {
	model := "gpt-5"
	if mode == "anthropic" {
		model = "claude-sonnet-4-6"
	}
	if mode == "gemini" {
		model = "gemini-3-flash-preview"
	}
	return ModelRunRequest{Template: "candy", Effort: "medium", Config: ModelTestConfig{Model: model, APIMode: mode, MaxOutputTokens: 4096, TimeoutSeconds: 5, Concurrency: 6, Tokenizer: "o200k_base", TokenTolerancePercent: 10}}
}

func modelFixtureSite() Site { return Site{Platform: "sub2api", BaseURL: "https://upstream.example"} }

func TestModelRunnerProtocolsUsageAndExactRequest(t *testing.T) {
	for _, tc := range []struct {
		mode, path, body         string
		input, output, reasoning int64
	}{
		{"chat_completions", "/v1/chat/completions", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"21\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":300,\"completion_tokens\":11,\"completion_tokens_details\":{\"reasoning_tokens\":10}}}\n\ndata: [DONE]\n\n", 300, 11, 10},
		{"responses", "/v1/responses", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"reported-model\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"21\"}]}],\"usage\":{\"input_tokens\":301,\"output_tokens\":12,\"output_tokens_details\":{\"reasoning_tokens\":11}}}}\n\n", 301, 12, 11},
		{"anthropic", "/v1/messages", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":302,\"output_tokens\":1}}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"secret reasoning\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"21\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":13}}\n\ndata: {\"type\":\"message_stop\"}\n\n", 302, 13, -1},
		{"gemini", "/v1beta/models/gemini-3-flash-preview:streamGenerateContent", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"thought\":true,\"text\":\"hidden reasoning\"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"21\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":303,\"candidatesTokenCount\":14,\"thoughtsTokenCount\":12}}\n\n", 303, 14, 12},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			runner := modelFixtureRunner(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, tc.path, r.URL.Path)
				require.Equal(t, 6, ModelRequestConcurrency(r.Context()))
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.NotContains(t, string(raw), "fixture-secret-key")
				var payload map[string]any
				require.NoError(t, json.Unmarshal(raw, &payload))
				switch tc.mode {
				case "chat_completions":
					require.Equal(t, "medium", payload["reasoning_effort"])
				case "responses":
					require.Equal(t, "medium", payload["reasoning"].(map[string]any)["effort"])
					require.Equal(t, ModelCandyPrompt, payload["input"])
				case "anthropic":
					require.Equal(t, "medium", payload["output_config"].(map[string]any)["effort"])
				case "gemini":
					require.Equal(t, "MEDIUM", payload["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)["thinkingLevel"])
				}
				return modelFixtureResponse(tc.body, "text/event-stream"), nil
			})
			got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture-secret-key"}, modelFixtureRequest(tc.mode))
			require.NoError(t, err)
			require.True(t, got.Success)
			require.True(t, got.Completed)
			require.Equal(t, "21", got.ResponseText)
			require.Equal(t, "numeric_correct", got.CandyVerdict)
			require.NotNil(t, got.TTFTMS)
			require.Equal(t, tc.input, *got.Usage.InputTokens)
			require.Equal(t, tc.output, *got.Usage.OutputTokens)
			if tc.reasoning >= 0 {
				require.Equal(t, tc.reasoning, *got.Usage.ReasoningTokens)
			} else {
				require.Nil(t, got.Usage.ReasoningTokens)
			}
			require.True(t, json.Valid(got.RawUsage))
		})
	}
}

func TestModelRunnerIncompleteTruncatedMissingUsageAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, wantCode string
		success                           bool
	}{
		{"incomplete", "data: {\"choices\":[{\"delta\":{\"content\":\"21\"}}]}\n\n", "text/event-stream", "incomplete_stream", false},
		{"truncated", "data: {\"choices\":[{\"delta\":{\"content\":\"21\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", "text/event-stream", "output_truncated", false},
		{"missingusage", "data: {\"choices\":[{\"delta\":{\"content\":\"21\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", "text/event-stream", "", true},
		{"jsonfallback", `{"choices":[{"message":{"content":"21"},"finish_reason":"stop"}]}`, "application/json", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) { return modelFixtureResponse(tc.body, tc.contentType), nil })
			got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, modelFixtureRequest("chat_completions"))
			require.Equal(t, tc.success, got.Success)
			require.Equal(t, tc.wantCode, got.ErrorCode)
			if tc.success {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Nil(t, got.Usage.OutputTokens)
			require.Equal(t, "usage_missing", got.Tokens.OutputState)
			if tc.contentType == "application/json" {
				require.Nil(t, got.TTFTMS)
				require.False(t, got.Streamed)
			}
		})
	}
}

func TestModelRunnerTTFTIgnoresHeartbeatAndReasoning(t *testing.T) {
	reader, writer := io.Pipe()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer writer.Close()
		fmt.Fprint(writer, ": heartbeat\n\ndata: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"not visible\"}}]}\n\n")
		time.Sleep(30 * time.Millisecond)
		fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"21\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}()
	runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
	})
	got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, modelFixtureRequest("chat_completions"))
	<-finished
	require.NoError(t, err)
	require.NotNil(t, got.TTFTMS)
	require.GreaterOrEqual(t, *got.TTFTMS, int64(20))
	require.Equal(t, "21", got.ResponseText)
}

func TestModelRunnerUnsupportedEffortIsNeverOmitted(t *testing.T) {
	called := false
	runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })
	for _, tc := range []struct{ mode, model string }{{"anthropic", "claude-sonnet-4-5"}, {"gemini", "gemini-3-pro-preview"}, {"chat_completions", "gpt-4o"}} {
		r := modelFixtureRequest(tc.mode)
		r.Config.Model = tc.model
		got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, r)
		require.Error(t, err)
		require.Equal(t, "effort_unsupported", got.ErrorCode)
		require.Equal(t, "unsupported", got.EffortSupport)
	}
	require.False(t, called)
}

func TestModelRunnerBoundedResponseAndUnknownUsage(t *testing.T) {
	r := &modelBoundedReader{reader: strings.NewReader("123456789"), max: 8}
	_, err := io.ReadAll(r)
	require.ErrorIs(t, err, errModelBodyLimit)
	state := modelStreamState{result: &ModelRunResult{}, mode: "chat_completions"}
	object, err := modelObject([]byte(`{"usage":{"prompt_tokens":-2,"completion_tokens":1.5}}`))
	require.NoError(t, err)
	state.consumeChat(object, true)
	require.Equal(t, int64(-2), *state.result.Usage.InputTokens)
	require.Equal(t, int64(-1), *state.result.Usage.OutputTokens)
}

func TestModelRunnerFirstContentTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
	})
	r := modelFixtureRequest("chat_completions")
	r.Config.FirstContentTimeoutSeconds = 1
	r.Config.IdleTimeoutSeconds = 4
	got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, r)
	require.Error(t, err)
	require.Equal(t, "first_content_timeout", got.ErrorCode)
	require.Nil(t, got.TTFTMS)
}

func TestModelRunnerIdleTimeoutAndCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel_%t", cancelEarly), func(t *testing.T) {
			reader, writer := io.Pipe()
			defer writer.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := make(chan struct{})
			go func() {
				fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"part\"}}]}\n\n")
				close(ready)
				if cancelEarly {
					cancel()
				}
			}()
			runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
			})
			r := modelFixtureRequest("chat_completions")
			r.Config.FirstContentTimeoutSeconds = 4
			r.Config.IdleTimeoutSeconds = 1
			got, err := runner.RunModel(ctx, modelFixtureSite(), RemoteKey{Key: "fixture"}, r)
			<-ready
			require.Error(t, err)
			if cancelEarly {
				require.Equal(t, "cancelled", got.ErrorCode)
			} else {
				require.Equal(t, "stream_idle_timeout", got.ErrorCode)
				require.Equal(t, "part", got.ResponseText)
				require.NotNil(t, got.TTFTMS)
			}
			require.False(t, got.Completed)
		})
	}
}

func TestModelRunnerProtocolErrorsAndTrailingUsage(t *testing.T) {
	runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) {
		return modelFixtureResponse("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"21\"}]},\"finishReason\":\"STOP\"}]}\n\ndata: {\"usageMetadata\":{\"promptTokenCount\":250,\"candidatesTokenCount\":1}}\n\n", "text/event-stream"), nil
	})
	got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, modelFixtureRequest("gemini"))
	require.NoError(t, err)
	require.Equal(t, int64(250), *got.Usage.InputTokens)
	require.Equal(t, "within_tolerance", got.Tokens.OutputState, "Gemini candidate usage excludes separate thoughts")
	for _, tc := range []struct {
		status              int
		body, code, support string
	}{
		{400, `{"error":{"message":"unsupported reasoning_effort fixture-secret"}}`, "effort_unsupported", "unsupported"},
		{401, `{"message":"fixture-secret"}`, "authentication_failed", "supported_parameter"},
		{403, `{"message":"fixture-secret"}`, "upstream_forbidden", "supported_parameter"},
		{429, `{}`, "rate_limited", "supported_parameter"},
	} {
		runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) {
			r := modelFixtureResponse(tc.body, "application/json")
			r.StatusCode = tc.status
			return r, nil
		})
		got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, modelFixtureRequest("responses"))
		require.Error(t, err)
		require.Equal(t, tc.code, got.ErrorCode)
		require.Equal(t, tc.support, got.EffortSupport)
		require.NotContains(t, err.Error(), "fixture-secret")
		require.Empty(t, got.ResponseText)
	}
}

func TestModelRunnerRejectsUnsafeOriginBeforeFactory(t *testing.T) {
	called := false
	runner := modelFixtureRunner(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })
	for _, origin := range []string{"http://upstream.example", "https://user:pass@upstream.example", "https://upstream.example?x=1", "https://upstream.example/path"} {
		s := modelFixtureSite()
		s.BaseURL = origin
		got, err := runner.RunModel(t.Context(), s, RemoteKey{Key: "fixture"}, modelFixtureRequest("responses"))
		require.Error(t, err)
		require.Equal(t, "invalid_origin", got.ErrorCode)
	}
	require.False(t, called)
	require.Zero(t, ModelRequestConcurrency(context.Background()))
}

func TestModelRunnerProbeRequiresCurrentNonce(t *testing.T) {
	var nonces []string
	for _, echo := range []bool{false, true, true} {
		runner := modelFixtureRunner(func(r *http.Request) (*http.Response, error) {
			var body struct {
				Input string `json:"input"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			nonce := strings.TrimPrefix(body.Input, "Reply with exactly this verification token: ")
			require.True(t, strings.HasPrefix(nonce, "GOV_PROBE_"))
			nonces = append(nonces, nonce)
			answer := "OK"
			if echo {
				answer = nonce
			}
			event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": answer}}}}}})
			return modelFixtureResponse("data: "+string(event)+"\n\n", "text/event-stream"), nil
		})
		r := modelFixtureRequest("responses")
		r.Template = "probe"
		got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, r)
		require.Equal(t, echo, got.Success)
		if echo {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
			require.Equal(t, "verification_failed", got.ErrorCode)
		}
		require.Equal(t, echo, *got.Tokens.MarkersPassed)
	}
	require.NotEqual(t, nonces[0], nonces[1])
	require.NotEqual(t, nonces[1], nonces[2])
}

func TestModelRunnerLatencyExcludesClientPreparation(t *testing.T) {
	runner := NewConnector(func(ctx context.Context, _ Site) (HTTPDoer, error) {
		require.Equal(t, 6, ModelRequestConcurrency(ctx))
		time.Sleep(1100 * time.Millisecond)
		return connectorDoer(func(*http.Request) (*http.Response, error) {
			return modelFixtureResponse("data: {\"choices\":[{\"delta\":{\"content\":\"21\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", "text/event-stream"), nil
		}), nil
	}).(ModelRunner)
	r := modelFixtureRequest("chat_completions")
	r.Config.FirstContentTimeoutSeconds = 1
	start := time.Now()
	got, err := runner.RunModel(t.Context(), modelFixtureSite(), RemoteKey{Key: "fixture"}, r)
	require.NoError(t, err, "client preparation must not consume first-content allowance")
	require.GreaterOrEqual(t, time.Since(start).Milliseconds()-got.DurationMS, int64(1000))
	require.NotNil(t, got.TTFTMS)
}
