package service

import (
	"encoding/json"
	"errors"
	"strings"
)

// validateOpenAICodexTicket332Stream consumes the whole captured SSE response,
// including events following completion, before it can publish a 332 ticket.
func validateOpenAICodexTicket332Stream(body []byte) error {
	completed, failed := false, false
	var parser openAICompatSSEFrameParser
	consume := func(frame openAICompatSSEFrame, hasData bool) {
		eventType := strings.TrimSpace(frame.EventType)
		if eventType == "response.failed" || eventType == "response.incomplete" || eventType == "error" {
			failed = true
		}
		if !hasData || strings.TrimSpace(frame.Data) == "[DONE]" {
			return
		}
		var event struct {
			Type     string `json:"type"`
			Response struct {
				Status string          `json:"status"`
				Error  json.RawMessage `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(frame.Data), &event) != nil {
			failed = true
			return
		}
		typ := strings.TrimSpace(event.Type)
		if typ == "" {
			typ = strings.TrimSpace(eventType)
		}
		if typ == "response.failed" || typ == "response.incomplete" || typ == "error" {
			failed = true
		}
		status := strings.TrimSpace(event.Response.Status)
		if status == "failed" || status == "incomplete" || status == "cancelled" {
			failed = true
		}
		if len(event.Response.Error) > 0 && string(event.Response.Error) != "null" {
			failed = true
		}
		if typ == "response.completed" || typ == "response.done" {
			if status != "" && status != "completed" {
				failed = true
			}
			completed = true
		}
	}
	for _, line := range strings.Split(string(body), "\n") {
		consume(parser.AddLine(strings.TrimRight(line, "\r")))
	}
	consume(parser.Finish())
	if failed {
		return errors.New("codex ticket probe stream failed")
	}
	if !completed {
		return errors.New("codex ticket probe stream incomplete")
	}
	return nil
}
