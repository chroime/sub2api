package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimateGroupInputRoutingTokensExplicitContext(t *testing.T) {
	for _, protocol := range []string{"responses", "chat", "messages", "gemini"} {
		t.Run(protocol, func(t *testing.T) {
			var body string
			switch protocol {
			case "responses":
				body = `{"input":"Hello","instructions":"Remember every instruction","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`
			case "chat":
				body = `{"messages":[{"role":"system","content":"Remember every instruction"},{"role":"user","content":"Hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`
			case "messages":
				body = `{"system":"Remember every instruction","messages":[{"role":"user","content":"Hello"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`
			case "gemini":
				body = `{"contents":[{"role":"user","parts":[{"text":"Hello"}]}],"systemInstruction":{"parts":[{"text":"Remember every instruction"}]},"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"object"}}]}]}`
			}
			count, known := EstimateGroupInputRoutingTokens([]byte(body), protocol, "gpt-6-astra")
			require.True(t, known)
			require.Greater(t, count, 10, "count system/history/tool schema, not only the last user text")
			larger := strings.Replace(body, "Hello", strings.Repeat("More context 中文测试 ", 150), 1)
			largerCount, known := EstimateGroupInputRoutingTokens([]byte(larger), protocol, "gpt-6-astra")
			require.True(t, known)
			require.Greater(t, largerCount, count+150)
		})
	}
}

func TestEstimateGroupInputRoutingTokensIgnoresOutputAndRequestMode(t *testing.T) {
	base := `{"messages":[{"role":"user","content":"Hello"}]}`
	count, known := EstimateGroupInputRoutingTokens([]byte(base), "chat", "gpt-6-astra")
	require.True(t, known)
	for _, extra := range []string{`,"stream":true`, `,"stream":false`, `,"max_tokens":1000000,"temperature":1,"metadata":{"note":"not an input message"}`} {
		got, known := EstimateGroupInputRoutingTokens([]byte(strings.TrimSuffix(base, "}")+extra+"}"), "chat", "gpt-6-astra")
		require.True(t, known)
		require.Equal(t, count, got)
	}
}

func TestEstimateGroupInputRoutingTokensUnknownContextDoesNotMatch(t *testing.T) {
	for _, tc := range []struct{ protocol, body string }{
		{"responses", `{"input":"Hello","previous_response_id":"resp_old"}`},
		{"responses", `{"input":"Hello","conversation":{"id":"conv_old"}}`},
		{"responses", `{"input":"Hello","prompt":{"id":"pmpt_old"}}`},
		{"responses", `{"input":[{"type":"item_reference","id":"old"}]}`},
		{"responses", `{"input":[{"id":"msg_old"}]}`},
		{"responses", `{"input":[{"type":"reasoning","id":"rs_old","summary":[]}]}`},
		{"responses", `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`},
		{"responses", `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.test/a.png"}]}]}`},
		{"chat", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`},
		{"messages", `{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"text","data":"large document"}}]}]}`},
		{"messages", `{"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","data":"AAAA"}}]}]}]}`},
		{"gemini", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`},
		{"gemini", `{"contents":[{"parts":[{"text":"hello"}]}],"cachedContent":"cachedContents/old"}`},
		{"chat", `{not-json}`},
		{"chat", `{}`},
		{"chat", `{"messages":"not a message list"}`},
		{"realtime", `{"input":"Hello"}`},
	} {
		t.Run(fmt.Sprintf("%s-%s", tc.protocol, tc.body), func(t *testing.T) {
			count, known := EstimateGroupInputRoutingTokens([]byte(tc.body), tc.protocol, "gpt-6-astra")
			require.False(t, known)
			require.Equal(t, 0, count)
		})
	}
}

func TestEstimateGroupInputRoutingTokensAllowsTextToolResultsAndSchemas(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","tool_call_id":"call_1","content":"A tool result"}],"tools":[{"type":"function","function":{"name":"image_url","parameters":{"type":"object","properties":{"image_url":{"type":"string"}}}}}]}`)
	count, known := EstimateGroupInputRoutingTokens(body, "chat", "gpt-6-astra")
	require.True(t, known, "tool schema field names are not media payloads")
	require.Greater(t, count, 10)
}

func TestEstimateGroupInputRoutingTokensAllowsProtocolMetadataAndToolData(t *testing.T) {
	for _, tc := range []struct{ protocol, body string }{
		{"messages", `{"system":[{"type":"text","text":"Instructions","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"Hello"}]}`},
		{"chat", `{"messages":[{"role":"assistant","tool_calls":[{"id":"call1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call1","content":"result"}]}`},
		{"gemini", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"type":"invoice","id":"business-id","image_url":"just tool data"}}}]}]}`},
		{"gemini", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"call1","name":"lookup","args":{"q":"x"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"call1","name":"lookup","response":{"type":"invoice"}}}]}]}`},
	} {
		count, known := EstimateGroupInputRoutingTokens([]byte(tc.body), tc.protocol, "gpt-6.1-sol")
		require.True(t, known, tc.body)
		require.Greater(t, count, 0)
	}
}
