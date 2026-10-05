package upstreamgovernance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

type modelStreamState struct {
	result    *ModelRunResult
	mode      string
	started   time.Time
	clock     *modelStreamClock
	text      strings.Builder
	usages    []json.RawMessage
	terminal  bool
	hasFinish bool
}

func (s *modelStreamState) addText(text string) {
	if text == "" {
		return
	}
	s.text.WriteString(text)
	if strings.TrimSpace(text) != "" {
		if s.clock != nil {
			s.clock.content()
		}
		if s.result.Streamed && s.result.TTFTMS == nil {
			ms := time.Since(s.started).Milliseconds()
			s.result.TTFTMS = &ms
		}
	}
}

func (s *modelStreamState) finish() {
	s.result.ResponseText = s.text.String()
	s.result.Completed = s.terminal && s.hasFinish
	if len(s.usages) > 0 {
		s.result.RawUsage, _ = json.Marshal(s.usages)
	}
}

func modelObject(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, ErrUnsupported
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, ErrUnsupported
	}
	return value, nil
}

func modelMap(value any) map[string]any { v, _ := value.(map[string]any); return v }
func modelString(value any) string      { v, _ := value.(string); return v }
func modelArray(value any) []any        { v, _ := value.([]any); return v }

func (s *modelStreamState) consumeSSE(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16<<10), modelResponseMaxBytes)
	var data strings.Builder
	flush := func() (bool, error) {
		if data.Len() == 0 {
			return false, nil
		}
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "[DONE]" {
			s.terminal = true
			return true, nil
		}
		object, err := modelObject([]byte(payload))
		if err != nil {
			return false, err
		}
		s.consumeEvent(object)
		// Gemini may send usage in a trailing chunk after finishReason. Read to
		// EOF there; the terminal candidate is still required for success.
		return s.terminal && s.mode != "gemini", nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			stop, err := flush()
			if err != nil || stop {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			return errModelBodyLimit
		}
		return err
	}
	_, err := flush()
	return err
}

func (s *modelStreamState) consumeJSON(raw []byte) error {
	object, err := modelObject(raw)
	if err != nil {
		return err
	}
	switch s.mode {
	case "responses":
		s.consumeResponse(object, true)
	case "anthropic":
		for _, part := range modelArray(object["content"]) {
			block := modelMap(part)
			if modelString(block["type"]) == "text" {
				s.addText(modelString(block["text"]))
			}
		}
		s.setFinish(modelString(object["stop_reason"]))
		s.saveUsage(object["usage"])
		s.terminal = s.hasFinish
	case "gemini":
		s.consumeGemini(object)
	default:
		s.consumeChat(object, false)
		s.terminal = s.hasFinish
	}
	if object["error"] != nil {
		s.result.ErrorCode = "upstream_stream_error"
	}
	if model := modelString(object["model"]); model != "" {
		s.result.ResponseModel = model
	}
	return nil
}

func (s *modelStreamState) consumeEvent(object map[string]any) {
	if object["error"] != nil {
		s.result.ErrorCode = "upstream_stream_error"
		s.terminal = true
		return
	}
	if model := modelString(object["model"]); model != "" {
		s.result.ResponseModel = model
	}
	switch s.mode {
	case "responses":
		switch modelString(object["type"]) {
		case "response.output_text.delta":
			s.addText(modelString(object["delta"]))
		case "response.completed", "response.incomplete":
			s.consumeResponse(modelMap(object["response"]), true)
		case "response.failed", "error":
			s.result.ErrorCode = "upstream_stream_error"
			s.terminal = true
		}
	case "anthropic":
		switch modelString(object["type"]) {
		case "message_start":
			message := modelMap(object["message"])
			s.result.ResponseModel = modelString(message["model"])
			s.saveUsage(message["usage"])
		case "content_block_start":
			block := modelMap(object["content_block"])
			if modelString(block["type"]) == "text" {
				s.addText(modelString(block["text"]))
			}
		case "content_block_delta":
			delta := modelMap(object["delta"])
			if modelString(delta["type"]) == "text_delta" {
				s.addText(modelString(delta["text"]))
			}
		case "message_delta":
			s.setFinish(modelString(modelMap(object["delta"])["stop_reason"]))
			s.saveUsage(object["usage"])
		case "message_stop":
			s.terminal = true
		case "error":
			s.result.ErrorCode = "upstream_stream_error"
			s.terminal = true
		}
	case "gemini":
		s.consumeGemini(object)
	default:
		s.consumeChat(object, true)
	}
}

func (s *modelStreamState) consumeChat(object map[string]any, streaming bool) {
	for i, item := range modelArray(object["choices"]) {
		choice := modelMap(item)
		// Only the requested primary candidate contributes visible text and timing.
		if index, ok := choice["index"].(json.Number); ok {
			if index != "0" {
				continue
			}
		} else if i != 0 {
			continue
		}
		content := modelMap(choice["message"])
		if streaming {
			content = modelMap(choice["delta"])
		}
		s.addText(modelVisibleContent(content["content"]))
		s.setFinish(modelString(choice["finish_reason"]))
	}
	s.saveUsage(object["usage"])
}

func modelVisibleContent(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	var b strings.Builder
	for _, value := range modelArray(value) {
		part := modelMap(value)
		typ := modelString(part["type"])
		if typ == "text" || typ == "output_text" {
			b.WriteString(modelString(part["text"]))
		}
	}
	return b.String()
}

func (s *modelStreamState) consumeResponse(response map[string]any, terminal bool) {
	if response == nil {
		s.result.ErrorCode = "invalid_response"
		return
	}
	if s.text.Len() == 0 {
		for _, item := range modelArray(response["output"]) {
			message := modelMap(item)
			if modelString(message["type"]) == "message" {
				for _, part := range modelArray(message["content"]) {
					block := modelMap(part)
					if modelString(block["type"]) == "output_text" {
						s.addText(modelString(block["text"]))
					}
				}
			}
		}
	}
	if model := modelString(response["model"]); model != "" {
		s.result.ResponseModel = model
	}
	s.saveUsage(response["usage"])
	status := modelString(response["status"])
	if status == "incomplete" {
		reason := modelString(modelMap(response["incomplete_details"])["reason"])
		if reason == "" {
			reason = "incomplete"
		}
		s.setFinish(reason)
	} else if status == "completed" {
		s.setFinish("completed")
	} else if status == "failed" {
		s.result.ErrorCode = "upstream_stream_error"
	}
	if terminal {
		s.terminal = true
	}
}

func (s *modelStreamState) consumeGemini(object map[string]any) {
	if model := modelString(object["modelVersion"]); model != "" {
		s.result.ResponseModel = model
	}
	for i, item := range modelArray(object["candidates"]) {
		if i != 0 {
			continue
		}
		candidate := modelMap(item)
		for _, part := range modelArray(modelMap(candidate["content"])["parts"]) {
			block := modelMap(part)
			if thought, _ := block["thought"].(bool); !thought {
				s.addText(modelString(block["text"]))
			}
		}
		s.setFinish(modelString(candidate["finishReason"]))
	}
	s.saveUsage(object["usageMetadata"])
	if s.hasFinish {
		s.terminal = true
	}
	if modelString(modelMap(object["promptFeedback"])["blockReason"]) != "" {
		s.result.ErrorCode = "content_blocked"
		s.terminal = true
	}
}

func (s *modelStreamState) setFinish(reason string) {
	if reason == "" {
		return
	}
	s.result.FinishReason = reason
	s.hasFinish = true
	switch strings.ToLower(reason) {
	case "stop", "end_turn", "stop_sequence", "completed":
	case "length", "max_tokens", "max_output_tokens", "max_output_tokens_exceeded", "incomplete", "max_tokens_exceeded":
	default:
		s.result.ErrorCode = "non_text_completion"
	}
}

func (s *modelStreamState) saveUsage(value any) {
	u := modelMap(value)
	if u == nil {
		return
	}
	if raw, err := json.Marshal(u); err == nil {
		s.usages = append(s.usages, raw)
	}
	set := func(dst **int64, object map[string]any, field string) {
		value, ok := object[field]
		if !ok || value == nil {
			return
		}
		n, ok := value.(json.Number)
		parsed := int64(-1)
		if ok {
			if v, err := n.Int64(); err == nil {
				parsed = v
			}
		}
		*dst = &parsed
	}
	switch s.mode {
	case "chat_completions":
		set(&s.result.Usage.InputTokens, u, "prompt_tokens")
		set(&s.result.Usage.OutputTokens, u, "completion_tokens")
		set(&s.result.Usage.CachedTokens, modelMap(u["prompt_tokens_details"]), "cached_tokens")
		set(&s.result.Usage.ReasoningTokens, modelMap(u["completion_tokens_details"]), "reasoning_tokens")
	case "responses":
		set(&s.result.Usage.InputTokens, u, "input_tokens")
		set(&s.result.Usage.OutputTokens, u, "output_tokens")
		set(&s.result.Usage.CachedTokens, modelMap(u["input_tokens_details"]), "cached_tokens")
		set(&s.result.Usage.ReasoningTokens, modelMap(u["output_tokens_details"]), "reasoning_tokens")
	case "anthropic":
		set(&s.result.Usage.InputTokens, u, "input_tokens")
		set(&s.result.Usage.OutputTokens, u, "output_tokens")
		set(&s.result.Usage.CachedTokens, u, "cache_read_input_tokens")
		set(&s.result.Usage.CacheCreationTokens, u, "cache_creation_input_tokens")
	case "gemini":
		set(&s.result.Usage.InputTokens, u, "promptTokenCount")
		set(&s.result.Usage.OutputTokens, u, "candidatesTokenCount")
		set(&s.result.Usage.CachedTokens, u, "cachedContentTokenCount")
		set(&s.result.Usage.ReasoningTokens, u, "thoughtsTokenCount")
	}
}
