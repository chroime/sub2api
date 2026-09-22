package service

import (
	"encoding/json"
	"strings"
)

// validateOpenAICodexTicket332Stream consumes the whole captured SSE response,
// including events following completion, before it can publish a 332 ticket.
func validateOpenAICodexTicket332Stream(body []byte) error {
	return validateOpenAICodexTicketStream(body, "")
}

func validateOpenAICodexTicketStream(body []byte, expectedModel string) error {
	completed, failed := false, false
	var reportedFailure *openAICodexTicketProbeFailure
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
			Type     string          `json:"type"`
			Error    json.RawMessage `json:"error"`
			Response struct {
				Status string          `json:"status"`
				Model  string          `json:"model"`
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
		for _, raw := range []json.RawMessage{event.Error, event.Response.Error} {
			if failure := openAICodexTicketStreamFailure(raw); failure != nil {
				reportedFailure = failure
				failed = true
			}
		}
		if typ == "response.completed" || typ == "response.done" {
			if status != "" && status != "completed" {
				failed = true
			}
			completed = true
			if expectedModel != "" && event.Response.Model != expectedModel {
				failed = true
				if reportedFailure == nil {
					reportedFailure = &openAICodexTicketProbeFailure{Code: "model_mismatch"}
				}
			}
		}
	}
	for _, line := range strings.Split(string(body), "\n") {
		consume(parser.AddLine(strings.TrimRight(line, "\r")))
	}
	consume(parser.Finish())
	if reportedFailure != nil {
		return reportedFailure
	}
	if failed {
		return &openAICodexTicketProbeFailure{Code: "stream_invalid"}
	}
	if !completed {
		return &openAICodexTicketProbeFailure{Code: "stream_incomplete"}
	}
	return nil
}
