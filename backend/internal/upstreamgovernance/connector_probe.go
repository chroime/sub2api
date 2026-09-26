package upstreamgovernance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func urlQuery(value string) string { return url.QueryEscape(value) }
func (c *platformConnector) Probe(ctx context.Context, s Site, key RemoteKey, platform, model string) (ProbeResult, error) {
	started := time.Now()
	result := ProbeResult{}
	if key.Key == "" || strings.TrimSpace(model) == "" || len(model) > 256 {
		return result, ErrInvalid
	}
	headers := http.Header{}
	session := Session{}
	var path string
	var payload any
	switch platform {
	case "openai":
		path = "/v1/chat/completions"
		session.AccessToken = key.Key
		payload = map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": "Reply OK."}}, "max_tokens": 8, "stream": false}
	case "anthropic":
		path = "/v1/messages"
		headers.Set("x-api-key", key.Key)
		headers.Set("anthropic-version", "2023-06-01")
		payload = map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": "Reply OK."}}, "max_tokens": 8, "stream": false}
	case "gemini":
		if strings.ContainsAny(model, "/\\?#%") || model == "." || model == ".." {
			return result, ErrInvalid
		}
		path = "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
		headers.Set("x-goog-api-key", key.Key)
		payload = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Reply OK."}}}}, "generationConfig": map[string]any{"maxOutputTokens": 8}}
	default:
		return result, ErrUnsupported
	}
	_, raw, e := c.request(ctx, s, session, "POST", path, payload, headers, false)
	result.LatencyMS = time.Since(started).Milliseconds()
	if e != nil {
		result.ErrorCode = "upstream_request_failed"
		return result, e
	}
	var response struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Error) > 0 && string(response.Error) != "null" {
		result.ErrorCode = "invalid_text_response"
		return result, ErrUnsupported
	}
	switch platform {
	case "openai":
		for _, choice := range response.Choices {
			if strings.TrimSpace(choice.Message.Content) != "" {
				result.Success = true
			}
		}
	case "anthropic":
		for _, block := range response.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				result.Success = true
			}
		}
	case "gemini":
		for _, candidate := range response.Candidates {
			for _, part := range candidate.Content.Parts {
				if strings.TrimSpace(part.Text) != "" {
					result.Success = true
				}
			}
		}
	}
	if !result.Success {
		result.ErrorCode = "invalid_text_response"
		return result, ErrUnsupported
	}
	return result, nil
}
