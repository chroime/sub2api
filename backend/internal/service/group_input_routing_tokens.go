package service

import (
	"bytes"
	"encoding/json"
	"strings"
)

// EstimateGroupInputRoutingTokens counts the explicitly supplied text context
// with the same local tokenizer as count_tokens. It never contacts an upstream.
// Unknown media or server-held context must not accidentally qualify as a small
// request. The count is a routing estimate, never a replacement for billed usage.
func EstimateGroupInputRoutingTokens(body []byte, protocol, model string) (int, bool) {
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil || request == nil {
		return 0, false
	}
	for _, key := range []string{"previous_response_id", "conversation", "prompt", "cachedContent", "cached_content"} {
		if raw, exists := request[key]; exists && !emptyRoutingJSON(raw) {
			return 0, false
		}
	}
	if raw := request["background"]; bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
		return 0, false
	}
	var contextKeys, configKeys []string
	var inputKey string
	switch protocol {
	case "responses":
		inputKey = "input"
		contextKeys = []string{"input", "instructions"}
		configKeys = []string{"tools", "tool_choice", "text"}
	case "chat":
		inputKey = "messages"
		contextKeys = []string{"messages", "instructions"}
		configKeys = []string{"tools", "tool_choice", "functions", "function_call", "response_format"}
	case "messages":
		inputKey = "messages"
		contextKeys = []string{"messages", "system"}
		configKeys = []string{"tools", "tool_choice", "output_config"}
	case "gemini":
		inputKey = "contents"
		contextKeys = []string{"contents", "systemInstruction", "system_instruction"}
		configKeys = []string{"tools", "toolConfig", "tool_config"}
	default:
		return 0, false
	}
	input := bytes.TrimSpace(request[inputKey])
	if len(input) == 0 || (input[0] != '[' && !(protocol == "responses" && input[0] == '"')) {
		return 0, false
	}
	if protocol == "responses" && input[0] == '[' {
		var items []map[string]json.RawMessage
		if json.Unmarshal(input, &items) != nil {
			return 0, false
		}
		for _, item := range items {
			var id, kind string
			_ = json.Unmarshal(item["id"], &id)
			_ = json.Unmarshal(item["type"], &kind)
			if id != "" && (kind == "reasoning" || kind == "compaction" || (kind == "" && len(item["content"]) == 0)) {
				return 0, false
			}
		}
	}
	codec, err := openAIInputTokensCodecForModel(model)
	if err != nil {
		return 0, false
	}
	total := 0
	count := func(raw json.RawMessage) bool {
		if len(raw) == 0 || emptyRoutingJSON(raw) {
			return true
		}
		var compact bytes.Buffer
		if json.Compact(&compact, raw) != nil {
			return false
		}
		// Count structured roles, tool names and schemas as well as message text.
		// Request metadata and output token limits are intentionally not included.
		n, err := codec.Count(compact.String())
		if err != nil || n > int(^uint(0)>>1)-total {
			return false
		}
		total += n
		return true
	}
	for _, key := range contextKeys {
		raw, exists := request[key]
		if !exists {
			continue
		}
		var value any
		if json.Unmarshal(raw, &value) != nil || routingContextIsUnknown(value, 0) || !count(raw) {
			return 0, false
		}
	}
	for _, key := range configKeys {
		if !count(request[key]) {
			return 0, false
		}
	}
	if protocol == "gemini" {
		for _, key := range []string{"generationConfig", "generation_config"} {
			var config map[string]json.RawMessage
			if raw := request[key]; len(raw) > 0 {
				if json.Unmarshal(raw, &config) != nil {
					return 0, false
				}
				for _, schemaKey := range []string{"responseSchema", "responseJsonSchema", "response_schema", "response_json_schema"} {
					if !count(config[schemaKey]) {
						return 0, false
					}
				}
			}
		}
	}
	if total <= 0 {
		return 0, false
	}
	return total, true
}

func emptyRoutingJSON(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value == "" || value == "null" || value == `""`
}

func routingContextIsUnknown(value any, depth int) bool {
	if depth > 64 {
		return true
	}
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			if routingContextIsUnknown(item, depth+1) {
				return true
			}
		}
	case map[string]any:
		kind, _ := node["type"].(string)
		if kind != "" {
			switch kind {
			case "message", "text", "input_text", "output_text", "tool_use", "tool_result", "function", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "thinking", "reasoning", "summary_text", "refusal":
			default:
				return true
			}
		}
		for key, child := range node {
			switch key {
			case "image_url", "image", "input_audio", "audio", "video", "video_url", "file", "file_id", "file_data", "file_url", "inlineData", "inline_data", "fileData", "encrypted_content", "thoughtSignature", "thought_signature", "signature":
				text, isText := child.(string)
				if child != nil && (!isText || text != "") {
					return true
				}
			}
			// Arguments and tool input objects are ordinary user data; their field
			// names (e.g. type/image_url) are not protocol content-part markers.
			if key == "arguments" || key == "args" || key == "input" || key == "response" || key == "cache_control" {
				continue
			}
			if routingContextIsUnknown(child, depth+1) {
				return true
			}
		}
	}
	return false
}
