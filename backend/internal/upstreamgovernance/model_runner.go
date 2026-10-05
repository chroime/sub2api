package upstreamgovernance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const modelResponseMaxBytes = 8 << 20
const modelAdapterVersion = "governance-stream-v1"

type modelRequestConcurrencyKey struct{}

// ModelRequestConcurrency identifies model inference to the shared HTTP factory.
// Zero means an ordinary management request and retains its original pool.
func ModelRequestConcurrency(ctx context.Context) int {
	n, _ := ctx.Value(modelRequestConcurrencyKey{}).(int)
	return n
}

var (
	errModelFirstContent = errors.New("model first content timeout")
	errModelIdle         = errors.New("model stream idle timeout")
	errModelBodyLimit    = errors.New("model response size exceeded")
	errModelStream       = errors.New("model stream incomplete")
)

type modelWireRequest struct {
	path          string
	mode          string
	body          map[string]any
	headers       http.Header
	effortSupport string
	reasoning     bool
}

// RunModel deliberately bypasses the management connector's ten-second timeout.
// It retains the same injected public-host/proxy factory and never retries a
// potentially billable request, forwards login cookies, or enters gateway logs.
func (c *platformConnector) RunModel(ctx context.Context, site Site, key RemoteKey, request ModelRunRequest) (result ModelRunResult, runErr error) {
	var started time.Time
	result.TemplateVersion = modelTemplateVersion(request.Template)
	result.AdapterVersion = modelAdapterVersion
	var prompt modelPrompt
	var wire modelWireRequest
	defer func() {
		if !started.IsZero() {
			result.DurationMS = time.Since(started).Milliseconds()
		}
		if prompt.text != "" {
			result.Tokens = auditModelTokens(request.Config, wire.mode, prompt, result.ResponseText, result.Usage, wire.reasoning)
		}
		if request.Template == "candy" {
			result.CandyAnswer, result.CandyVerdict = modelCandyVerdict(result.ResponseText)
		}
		if request.Template == "pelican" {
			result.HTML = extractModelHTML(result.ResponseText)
		}
	}()
	if key.Key == "" || c.factory == nil || !validProbeModel(request.Config.Model) {
		result.ErrorCode = "invalid_request"
		return result, ErrInvalid
	}
	base, err := url.Parse(site.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		result.ErrorCode = "invalid_origin"
		return result, ErrInvalid
	}
	if site.Platform != "newapi" && site.Platform != "sub2api" {
		result.ErrorCode = "unsupported_platform"
		return result, ErrUnsupported
	}
	prompt, err = buildModelPrompt(request)
	if err != nil {
		result.ErrorCode = "invalid_template"
		return result, err
	}
	result.InputText = prompt.text
	wire, err = buildModelWire(site, key.Key, request, prompt.text)
	result.EffortSupport = wire.effortSupport
	if err != nil {
		result.ErrorCode = "effort_unsupported"
		if errors.Is(err, ErrInvalid) {
			result.ErrorCode = "invalid_request"
		}
		return result, err
	}
	result.SentParameters = map[string]any{"api_mode": wire.mode}
	for name, value := range wire.body {
		if name != "messages" && name != "contents" && name != "input" {
			result.SentParameters[name] = value
		}
	}
	result.RequestBody, err = json.Marshal(wire.body)
	if err != nil {
		result.ErrorCode = "invalid_request"
		return result, ErrInvalid
	}
	seconds := request.Config.TimeoutSeconds
	if seconds == 0 {
		seconds = 180
	}
	if seconds < 1 || seconds > 1800 || request.Config.FirstContentTimeoutSeconds < 0 || request.Config.IdleTimeoutSeconds < 0 || request.Config.FirstContentTimeoutSeconds > seconds || request.Config.IdleTimeoutSeconds > seconds {
		result.ErrorCode = "invalid_timeout"
		return result, ErrInvalid
	}
	// Proxy lookup/client preparation has its own bounded context. Neither the
	// first-content allowance nor channel latency includes local preparation.
	prepareCtx, prepareCancel := context.WithTimeout(ctx, 10*time.Second)
	prepareCtx = context.WithValue(prepareCtx, modelRequestConcurrencyKey{}, min(32, max(1, request.Config.Concurrency)))
	client, err := c.factory(prepareCtx, site)
	prepareCancel()
	if err != nil || client == nil {
		result.ErrorCode = "transport_unavailable"
		return result, errConnectorRemote
	}
	if concrete, ok := client.(*http.Client); ok {
		copy := *concrete
		copy.Jar = nil
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer timeoutCancel()
	runCtx, cancel := context.WithCancelCause(timeoutCtx)
	defer cancel(nil)
	runCtx = context.WithValue(runCtx, modelRequestConcurrencyKey{}, min(32, max(1, request.Config.Concurrency)))
	firstSeconds := request.Config.FirstContentTimeoutSeconds
	if firstSeconds == 0 {
		firstSeconds = min(60, seconds)
	}
	idleSeconds := request.Config.IdleTimeoutSeconds
	if idleSeconds == 0 {
		idleSeconds = min(30, seconds)
	}
	clock := &modelStreamClock{cancel: cancel, idleDelay: time.Duration(idleSeconds) * time.Second}
	defer clock.stop()
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, strings.TrimRight(site.BaseURL, "/")+wire.path, bytes.NewReader(result.RequestBody))
	if err != nil {
		result.ErrorCode = "invalid_request"
		return result, ErrInvalid
	}
	req.Header = wire.headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	started = time.Now()
	clock.first = time.AfterFunc(time.Duration(firstSeconds)*time.Second, func() { cancel(errModelFirstContent) })
	resp, err := client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		result.ErrorCode = modelNetworkError(runCtx)
		return result, errConnectorRemote
	}
	if resp == nil || resp.Body == nil {
		result.ErrorCode = "invalid_response"
		return result, errConnectorRemote
	}
	defer resp.Body.Close()
	stopClosing := context.AfterFunc(runCtx, func() { resp.Body.Close() })
	defer stopClosing()
	result.HTTPStatus = resp.StatusCode
	clock.activity()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		result.ErrorCode = modelHTTPError(resp.StatusCode)
		message := strings.ToLower(string(raw))
		if (resp.StatusCode == 400 || resp.StatusCode == 422) && (strings.Contains(message, "reasoning") || strings.Contains(message, "thinking") || strings.Contains(message, "effort")) {
			result.ErrorCode, result.EffortSupport = "effort_unsupported", "unsupported"
		}
		return result, errConnectorRemote
	}
	state := modelStreamState{result: &result, mode: wire.mode, started: started, clock: clock}
	reader := &modelBoundedReader{reader: resp.Body, clock: clock, max: modelResponseMaxBytes}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		raw, readErr := io.ReadAll(reader)
		if readErr == nil {
			readErr = state.consumeJSON(raw)
		}
		err = readErr
	} else {
		result.Streamed = true
		err = state.consumeSSE(reader)
	}
	state.finish()
	if reader.read > reader.max {
		err = errModelBodyLimit
	}
	if runCtx.Err() != nil {
		result.ErrorCode = modelNetworkError(runCtx)
		return result, errConnectorRemote
	}
	if err != nil {
		result.ErrorCode = "invalid_stream"
		if errors.Is(err, errModelBodyLimit) {
			result.ErrorCode = "response_too_large"
		}
		if runCtx.Err() != nil {
			result.ErrorCode = modelNetworkError(runCtx)
		}
		return result, errConnectorRemote
	}
	if result.ErrorCode != "" {
		return result, ErrUnsupported
	}
	if !result.Completed {
		result.ErrorCode = "incomplete_stream"
		return result, errModelStream
	}
	if modelTruncatedFinish(result.FinishReason) {
		result.ErrorCode = "output_truncated"
		return result, ErrUnsupported
	}
	if strings.TrimSpace(result.ResponseText) == "" {
		result.ErrorCode = "empty_response"
		return result, ErrUnsupported
	}
	if request.Template == "probe" && (len(prompt.markers) != 1 || !strings.Contains(result.ResponseText, prompt.markers[0].Expected)) {
		result.ErrorCode = "verification_failed"
		return result, ErrUnsupported
	}
	result.Success = true
	return result, nil
}

func buildModelWire(site Site, key string, req ModelRunRequest, prompt string) (modelWireRequest, error) {
	w := modelWireRequest{mode: req.Config.APIMode, headers: make(http.Header), effortSupport: "unverified_parameter", reasoning: true}
	if req.Effort != "low" && req.Effort != "medium" && req.Effort != "high" {
		w.effortSupport = "unsupported"
		return w, ErrInvalid
	}
	if w.mode == "" {
		w.mode = "chat_completions"
	}
	model := req.Config.Model
	lower := strings.ToLower(model)
	output := req.Config.MaxOutputTokens
	if output == 0 {
		output = 4096
	}
	if output < 1 || output > 65536 {
		return w, ErrInvalid
	}
	if req.Config.Platform != "" && !validSiteTransport(site.Platform, req.Config.Platform) {
		return w, ErrUnsupported
	}
	prefix := ""
	if req.Config.Platform == "antigravity" {
		prefix = "/antigravity"
	}
	switch w.mode {
	case "chat_completions", "responses":
		if strings.HasPrefix(lower, "gpt-3.5") || strings.HasPrefix(lower, "gpt-4o") || strings.HasPrefix(lower, "gpt-4.1") || lower == "gpt-4" || strings.HasPrefix(lower, "gpt-4-") {
			w.effortSupport = "unsupported"
			return w, ErrUnsupported
		}
		if strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") || strings.HasPrefix(lower, "gpt-5") || strings.HasPrefix(lower, "gpt-6") {
			w.effortSupport = "supported_parameter"
		}
		w.headers.Set("Authorization", "Bearer "+key)
		if w.mode == "responses" {
			w.path = prefix + "/v1/responses"
			w.body = map[string]any{"model": model, "input": prompt, "stream": true, "max_output_tokens": output, "reasoning": map[string]any{"effort": req.Effort}}
		} else {
			w.path = prefix + "/v1/chat/completions"
			w.body = map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": prompt}}, "stream": true, "stream_options": map[string]any{"include_usage": true}, "max_completion_tokens": output, "reasoning_effort": req.Effort}
		}
	case "anthropic":
		// Native effort is only mapped for model families that expose the effort
		// parameter. Older budget-only thinking APIs are not silently approximated.
		if !strings.Contains(lower, "claude-opus-4-6") && !strings.Contains(lower, "claude-sonnet-4-6") && !strings.Contains(lower, "claude-opus-4-5") {
			w.effortSupport = "unsupported"
			return w, ErrUnsupported
		}
		w.effortSupport = "supported_parameter"
		w.path = prefix + "/v1/messages"
		w.headers.Set("x-api-key", key)
		w.headers.Set("anthropic-version", "2023-06-01")
		w.body = map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": prompt}}, "stream": true, "max_tokens": output, "output_config": map[string]any{"effort": req.Effort}}
		if strings.Contains(lower, "4-6") {
			w.body["thinking"] = map[string]any{"type": "adaptive"}
		}
	case "gemini":
		if strings.ContainsAny(model, "/\\?#%") || model == "." || model == ".." {
			return w, ErrInvalid
		}
		if !strings.HasPrefix(lower, "gemini-3") || (req.Effort == "medium" && !strings.Contains(lower, "flash")) {
			w.effortSupport = "unsupported"
			return w, ErrUnsupported
		}
		w.effortSupport = "supported_parameter"
		w.path = prefix + "/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"
		w.headers.Set("x-goog-api-key", key)
		w.body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": output, "thinkingConfig": map[string]any{"thinkingLevel": strings.ToUpper(req.Effort)}}}
	default:
		return w, ErrUnsupported
	}
	return w, nil
}

func modelTruncatedFinish(reason string) bool {
	switch strings.ToLower(reason) {
	case "length", "max_tokens", "max_output_tokens", "max_output_tokens_exceeded", "incomplete", "max_tokens_exceeded":
		return true
	}
	return false
}

func modelHTTPError(status int) string {
	switch status {
	case 401:
		return "authentication_failed"
	case 403:
		return "upstream_forbidden"
	case 404, 405:
		return "protocol_unsupported"
	case 408, 504:
		return "timeout"
	case 429:
		return "rate_limited"
	}
	if status >= 500 {
		return "upstream_5xx"
	}
	return "upstream_rejected"
}

func modelNetworkError(ctx context.Context) string {
	switch {
	case errors.Is(context.Cause(ctx), errModelFirstContent):
		return "first_content_timeout"
	case errors.Is(context.Cause(ctx), errModelIdle):
		return "stream_idle_timeout"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "timeout"
	case ctx.Err() != nil:
		return "cancelled"
	default:
		return "transport_error"
	}
}

type modelStreamClock struct {
	mu          sync.Mutex
	first, idle *time.Timer
	idleDelay   time.Duration
	cancel      context.CancelCauseFunc
	stopped     bool
}

func (c *modelStreamClock) activity() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	if c.idle == nil {
		c.idle = time.AfterFunc(c.idleDelay, func() { c.cancel(errModelIdle) })
	} else {
		c.idle.Reset(c.idleDelay)
	}
}
func (c *modelStreamClock) content() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.first != nil {
		c.first.Stop()
	}
}
func (c *modelStreamClock) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	if c.first != nil {
		c.first.Stop()
	}
	if c.idle != nil {
		c.idle.Stop()
	}
}

type modelBoundedReader struct {
	reader    io.Reader
	clock     *modelStreamClock
	read, max int
}

func (r *modelBoundedReader) Read(p []byte) (int, error) {
	if r.read > r.max {
		return 0, errModelBodyLimit
	}
	if len(p) > r.max+1-r.read {
		p = p[:r.max+1-r.read]
	}
	n, err := r.reader.Read(p)
	r.read += n
	if n > 0 && r.clock != nil {
		r.clock.activity()
	}
	if r.read > r.max {
		return n, errModelBodyLimit
	}
	return n, err
}
