package handler

import (
	"bytes"
	"encoding/json"
	"strings"
)

const (
	codexBrowserTabsTool             = "browser_tabs"
	codexAmbientBrowserContextOpen   = `<in-app-browser-context source="ambient-ui-state">`
	codexAmbientBrowserContextClose  = `</in-app-browser-context>`
	codexAmbientBrowserContextNotice = "automatically supplied ambient UI state"
)

// Desktop injects the in-app browser tab list as a standalone output, without a
// model tool call. HTTP Responses rejects that item unless it is rewritten to
// untrusted user text before call_id validation.
func normalizeCodexBrowserContext(body []byte) ([]byte, bool) {
	if !mayContainCodexCallOutputCandidate(body, codexBrowserTabsTool) || !hasUniqueJSONMembers(body) {
		return body, false
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil {
		return body, false
	}
	var input []json.RawMessage
	if json.Unmarshal(request["input"], &input) != nil {
		return body, false
	}
	changed := false
	for i, raw := range input {
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		readString := func(key string) string {
			var value string
			_ = json.Unmarshal(item[key], &value)
			return value
		}
		if readString("type") != "function_call_output" || readString("namespace") != "agent" || readString("name") != codexBrowserTabsTool {
			continue
		}
		if callID, exists := item["call_id"]; exists {
			var value string
			if bytes.Equal(bytes.TrimSpace(callID), []byte("null")) || json.Unmarshal(callID, &value) != nil || strings.TrimSpace(value) != "" {
				continue
			}
		}
		output := readString("output")
		if !validCodexAmbientBrowserContext(output) {
			continue
		}
		message := map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]string{{
				"type": "input_text",
				"text": "<external_" + codexBrowserTabsTool + ">\n" + output + "\n</external_" + codexBrowserTabsTool + ">",
			}},
		}
		encoded, err := json.Marshal(message)
		if err != nil {
			return body, false
		}
		input[i] = encoded
		changed = true
	}
	if !changed {
		return body, false
	}
	encodedInput, err := json.Marshal(input)
	if err != nil {
		return body, false
	}
	request["input"] = encodedInput
	encoded, err := json.Marshal(request)
	if err != nil {
		return body, false
	}
	return encoded, true
}

func validCodexAmbientBrowserContext(output string) bool {
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, codexAmbientBrowserContextOpen) || !strings.HasSuffix(trimmed, codexAmbientBrowserContextClose) {
		return false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, codexAmbientBrowserContextOpen), codexAmbientBrowserContextClose))
	if inner == "" || strings.Contains(inner, codexAmbientBrowserContextOpen) || strings.Contains(inner, codexAmbientBrowserContextClose) {
		return false
	}
	return strings.Contains(inner, codexAmbientBrowserContextNotice)
}
