package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/accountquality"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	qualityProbeMaxResponseBytes = 8 << 20
	qualityProbeMaxTextBytes     = 1 << 20
)

// SetQualityProbeConcurrencyService injects the same account slots used by business traffic.
func (s *AccountTestService) SetQualityProbeConcurrencyService(concurrency *ConcurrencyService) {
	if s != nil {
		s.qualityProbeConcurrency = concurrency
	}
}

// AccountQualityCapabilities lists only protocol adapters with diagnostic parameter support.
// Model-specific parameter validation still runs before every request.
func AccountQualityCapabilities() []accountquality.Capability {
	return []accountquality.Capability{
		{Platform: PlatformOpenAI, APIMode: "responses", AccountTypes: []string{AccountTypeAPIKey}, AutomaticControl: true, OutputLimitMode: "upstream"},
		{Platform: PlatformOpenAI, APIMode: "chat_completions", AccountTypes: []string{AccountTypeAPIKey}, AutomaticControl: true, OutputLimitMode: "upstream"},
		{Platform: PlatformOpenAI, APIMode: "responses", AccountTypes: []string{AccountTypeOAuth, AccountTypeSetupToken}, AutomaticControl: false, OutputLimitMode: "local", Reason: "Codex OAuth has no verified upstream token cap; diagnostics enforce a local text byte bound and timeout. Credential shadows and Agent Identity are not supported."},
		{Platform: PlatformAnthropic, APIMode: "anthropic", AccountTypes: []string{AccountTypeAPIKey, AccountTypeOAuth}, AutomaticControl: true, OutputLimitMode: "upstream"},
		{Platform: PlatformGemini, APIMode: "gemini", AccountTypes: []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeServiceAccount}, AutomaticControl: true, OutputLimitMode: "upstream"},
		{Platform: PlatformGrok, APIMode: "responses", AccountTypes: []string{AccountTypeAPIKey}, AutomaticControl: true, OutputLimitMode: "upstream"},
		{Platform: PlatformGrok, APIMode: "chat_completions", AccountTypes: []string{AccountTypeAPIKey}, AutomaticControl: true, OutputLimitMode: "upstream"},
	}
}

// AccountQualityIdentityVersion exposes an opaque execution-identity digest, never credentials.
func AccountQualityIdentityVersion(account *Account) string {
	if account == nil {
		return ""
	}
	var parentID int64
	if account.ParentAccountID != nil {
		parentID = *account.ParentAccountID
	}
	var proxy map[string]any
	if account.Proxy != nil {
		proxy = map[string]any{"id": account.Proxy.ID, "protocol": account.Proxy.Protocol, "host": account.Proxy.Host, "port": account.Proxy.Port, "username": account.Proxy.Username, "password": account.Proxy.Password, "status": account.Proxy.Status, "expires_at": account.Proxy.ExpiresAt}
	}
	return accountquality.Fingerprint(accountquality.IdentityInput{ID: account.ID, ParentAccountID: parentID, Platform: account.Platform, Type: account.Type, Credentials: account.Credentials, Extra: account.Extra, Proxy: proxy, ProxyID: account.ProxyID})
}

// ProbeAccountText always uses the requested account. It deliberately does not call
// connectivity recovery, account selection, or failover to a different account.
func (s *AccountTestService) ProbeAccountText(ctx context.Context, input accountquality.ProbeRequest) (result accountquality.ProbeResult, err error) {
	started := time.Now()
	result = accountquality.ProbeResult{AccountID: input.AccountID, CredentialAccountID: input.AccountID, RequestedModel: input.Model, APIMode: input.APIMode, Effort: input.Effort, MaxOutputTokens: input.MaxOutputTokens, ExecutionStatus: "error"}
	defer func() { result.LatencyMS = time.Since(started).Milliseconds() }()
	if input.AccountID <= 0 || strings.TrimSpace(input.Prompt) == "" || len(input.Prompt) > 128<<10 || strings.TrimSpace(input.Model) == "" || input.MaxOutputTokens < 1 || input.MaxOutputTokens > 65536 || input.TimeoutSeconds < 1 || input.TimeoutSeconds > 600 {
		qualityProbeFailure(&result, "unsupported", "invalid_parameters", "The diagnostic prompt, model, output limit or timeout is invalid.")
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		qualityProbeFailure(&result, "error", "transport_unavailable", "The account diagnostic transport is unavailable.")
		return result, nil
	}
	account, loadErr := s.accountRepo.GetByID(ctx, input.AccountID)
	if loadErr != nil || account == nil || account.ID != input.AccountID {
		qualityProbeFailure(&result, "error", "account_unavailable", "The requested account is unavailable.")
		return result, nil
	}
	result.IdentityVersion = AccountQualityIdentityVersion(account)
	result.MappedModel = account.GetMappedModel(input.Model)
	if account.Platform == PlatformOpenAI {
		result.MappedModel = resolveOpenAIAccountUpstreamModelForRequest(account, input.Model, false)
	}
	if account.ParentAccountID != nil || account.IsOpenAIAgentIdentity() || account.IsSyntheticUITest() {
		qualityProbeFailure(&result, "unsupported", "unsupported_identity", "This account identity has no validated quality diagnostic adapter.")
		return result, nil
	}
	if !qualityProbeSupports(account, input.APIMode) || qualityProbeIsMediaModel(result.MappedModel) {
		qualityProbeFailure(&result, "unsupported", "unsupported_protocol", "This account, protocol or model has no validated text diagnostic adapter.")
		return result, nil
	}
	if !qualityProbeEffortSupported(account, input.APIMode, result.MappedModel, input.Effort) {
		qualityProbeFailure(&result, "unsupported", "unsupported_effort", "The selected model and protocol cannot honor this reasoning effort.")
		return result, nil
	}
	account = accountForQualityProbeAdmission(account, input.BindingID, input.Manual)
	if !account.IsSchedulableForModelWithContext(ctx, input.Model) {
		qualityProbeFailure(&result, "error", "admission_blocked", "The account is blocked by an independent scheduling condition.")
		return result, nil
	}
	proxyURL := ""
	if account.ProxyID != nil {
		if account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
			qualityProbeFailure(&result, "error", "proxy_unavailable", "The configured account proxy is unavailable.")
			return result, nil
		}
		proxyURL = account.Proxy.URL()
	}
	if s.qualityProbeConcurrency == nil {
		qualityProbeFailure(&result, "error", "concurrency_unavailable", "Account concurrency protection is unavailable.")
		return result, nil
	}
	slot, slotErr := s.qualityProbeConcurrency.AcquireAccountSlot(ctx, account.ID, max(1, account.Concurrency))
	if slotErr != nil || slot == nil || !slot.Acquired {
		if ctx.Err() != nil {
			qualityProbeTransportFailure(ctx, &result, ctx.Err())
		} else {
			qualityProbeFailure(&result, "error", "concurrency_unavailable", "No account concurrency slot is available.")
		}
		return result, nil
	}
	defer slot.ReleaseFunc()
	result.OutputLimitMode, result.AutoControlEligible = "upstream", true
	if account.Platform == PlatformOpenAI && account.IsOpenAIOAuthLike() {
		result.OutputLimitMode, result.AutoControlEligible = "local", false
	}
	req, buildErr := s.buildAccountQualityRequest(ctx, account, input, result.MappedModel)
	if buildErr != nil {
		if ctx.Err() != nil {
			qualityProbeTransportFailure(ctx, &result, ctx.Err())
		} else {
			qualityProbeFailure(&result, "error", "request_preparation", "The account credentials or upstream request could not be prepared.")
		}
		return result, nil
	}
	var resp *http.Response
	if account.Platform == PlatformOpenAI && input.APIMode == "responses" {
		resp, err = s.doOpenAIAccountTestUpstream(req, proxyURL, account, true)
	} else {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	}
	if err != nil {
		qualityProbeTransportFailure(ctx, &result, err)
		return result, nil
	}
	if resp == nil || resp.Body == nil {
		qualityProbeFailure(&result, "error", "invalid_response", "The upstream returned no response body.")
		return result, nil
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	result.UpstreamRequestID = resp.Header.Get("x-request-id")
	if result.UpstreamRequestID == "" {
		result.UpstreamRequestID = resp.Header.Get("request-id")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		class := "upstream_http"
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			class = "authentication"
		case http.StatusTooManyRequests:
			class = "rate_limit"
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			class = "timeout"
		}
		status := "error"
		if class == "timeout" {
			status = "timeout"
		}
		qualityProbeFailure(&result, status, class, fmt.Sprintf("The diagnostic upstream returned HTTP %d.", resp.StatusCode))
		return result, nil
	}
	parseErr := parseAccountQualityResponse(ctx, resp, &result, input.MaxOutputTokens)
	if parseErr != nil {
		if ctx.Err() != nil {
			qualityProbeTransportFailure(ctx, &result, ctx.Err())
		} else if errors.Is(parseErr, context.DeadlineExceeded) || errors.Is(parseErr, context.Canceled) {
			qualityProbeTransportFailure(ctx, &result, parseErr)
		} else {
			qualityProbeFailure(&result, "error", "invalid_response", "The upstream diagnostic response could not be decoded.")
		}
	}
	return result, nil
}

// This copy bypasses the executing binding's quality gate and, for manual runs,
// the business scheduling toggle. Account status, expiry, cooldowns, quotas, model
// and other-owner conditions still apply, and the stored snapshot stays intact.
func accountForQualityProbeAdmission(account *Account, bindingID int64, manual bool) *Account {
	scoped := *account
	if manual {
		scoped.Schedulable = true
	}
	scoped.Extra = make(map[string]any, len(account.Extra))
	for key, value := range account.Extra {
		scoped.Extra[key] = value
	}
	if gates, ok := account.Extra[AccountQualityGatesExtraKey].(map[string]any); ok {
		owned := ""
		if bindingID > 0 {
			owned = strconv.FormatInt(bindingID, 10)
		}
		remaining := make(map[string]any, len(gates))
		for key, value := range gates {
			if key != owned {
				remaining[key] = value
			}
		}
		scoped.Extra[AccountQualityGatesExtraKey] = remaining
	}
	return &scoped
}

func qualityProbeSupports(a *Account, mode string) bool {
	for _, capability := range AccountQualityCapabilities() {
		if a.Platform != capability.Platform || mode != capability.APIMode {
			continue
		}
		for _, typ := range capability.AccountTypes {
			if a.Type == typ {
				return true
			}
		}
	}
	return false
}

func qualityProbeIsMediaModel(model string) bool {
	m := strings.ToLower(model)
	for _, kind := range []string{"image", "video", "tts", "embedding", "realtime", "audio", "live"} {
		if strings.Contains(m, kind) {
			return true
		}
	}
	return false
}

func qualityProbeEffortSupported(a *Account, mode, model, effort string) bool {
	if effort == "" {
		return true
	}
	switch a.Platform {
	case PlatformOpenAI:
		if !qualityProbeOpenAIReasoningModel(model) {
			return false
		}
		switch effort {
		case "none", "minimal", "low", "medium", "high", "xhigh":
			return true
		}
	case PlatformAnthropic:
		modern := strings.Contains(model, "sonnet-4-6") || strings.Contains(model, "opus-4-6") || isClaude55SignedThinkingModel(model)
		if !modern {
			return false
		}
		switch effort {
		case "low", "medium", "high":
			return true
		case "max":
			return strings.Contains(model, "opus")
		}
	case PlatformGemini:
		switch effort {
		case "minimal", "low", "medium", "high":
		default:
			return false
		}
		if geminiModelNameCarriesThinkingTier(model) {
			return false
		}
		switch geminiThinkingParamForModel(model) {
		case geminiThinkingParamLevel:
			return geminiThinkingLevelFor(model, claudeThinkingIntent{tier: effort}) == effort
		case geminiThinkingParamBudget:
			return true
		}
	case PlatformGrok:
		if !grokSupportsReasoningEffort(model) {
			return false
		}
		switch effort {
		case "none", "low", "medium", "high":
			return true
		case "xhigh":
			return GrokSupportsXHighReasoningEffort(model)
		}
	}
	return false
}

func qualityProbeOpenAIReasoningModel(model string) bool {
	if strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		return true
	}
	var major int
	_, err := fmt.Sscanf(model, "gpt-%d", &major)
	return err == nil && major >= 5
}

func (s *AccountTestService) buildAccountQualityRequest(ctx context.Context, a *Account, input accountquality.ProbeRequest, mappedModel string) (*http.Request, error) {
	model, apiURL, token := mappedModel, "", ""
	payload := map[string]any{"model": model, "stream": true}
	switch input.APIMode {
	case "responses":
		payload["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": input.Prompt}}}}
		if input.Effort != "" {
			payload["reasoning"] = map[string]any{"effort": input.Effort}
		}
		if a.Platform == PlatformOpenAI && a.IsOpenAIOAuthLike() {
			payload["model"], payload["store"], payload["instructions"] = model, false, openai.DefaultInstructions
			apiURL = chatgptCodexAPIURL
		} else {
			payload["max_output_tokens"] = input.MaxOutputTokens
			if a.Platform == PlatformGrok {
				var err error
				apiURL, err = buildGrokResponsesURL(a, s.cfg, s.settingService)
				if err != nil {
					return nil, err
				}
			} else {
				baseURL, err := s.validateUpstreamBaseURL(a.GetOpenAIBaseURL())
				if err != nil {
					return nil, err
				}
				apiURL = buildOpenAIResponsesURLForPlatform(a.Platform, baseURL)
				payload["instructions"] = openai.DefaultInstructions
			}
		}
	case "chat_completions":
		payload["messages"] = []any{map[string]any{"role": "user", "content": input.Prompt}}
		payload["stream_options"] = map[string]any{"include_usage": true}
		capField := "max_tokens"
		if a.Platform == PlatformOpenAI && qualityProbeOpenAIReasoningModel(model) {
			capField = "max_completion_tokens"
		}
		payload[capField] = input.MaxOutputTokens
		if input.Effort != "" {
			payload["reasoning_effort"] = input.Effort
		}
		if a.Platform == PlatformGrok {
			var err error
			apiURL, err = buildGrokChatCompletionsURL(a, s.cfg, s.settingService)
			if err != nil {
				return nil, err
			}
		} else {
			baseURL, err := s.validateUpstreamBaseURL(a.GetOpenAIBaseURL())
			if err != nil {
				return nil, err
			}
			apiURL = buildOpenAIChatCompletionsURL(baseURL)
		}
	case "anthropic":
		payload["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": input.Prompt}}}}
		payload["max_tokens"] = input.MaxOutputTokens
		if input.Effort != "" {
			payload["thinking"] = map[string]any{"type": "adaptive"}
			payload["output_config"] = map[string]any{"effort": input.Effort}
		}
		if a.IsOAuth() {
			if s.claudeTokenProvider == nil {
				return nil, errors.New("Claude token provider unavailable")
			}
			var err error
			token, err = s.claudeTokenProvider.GetAccessToken(ctx, a)
			if err != nil {
				return nil, err
			}
			apiURL = testClaudeAPIURL
			payload["system"] = []any{map[string]any{"type": "text", "text": claudeCodeSystemPrompt}}
			session, err := generateSessionString()
			if err != nil {
				return nil, err
			}
			payload["metadata"] = map[string]any{"user_id": session}
		} else {
			baseURL, err := s.validateUpstreamBaseURL(a.GetBaseURL())
			if err != nil {
				return nil, err
			}
			apiURL = strings.TrimSuffix(baseURL, "/") + "/v1/messages"
			token = a.GetCredential("api_key")
		}
	case "gemini":
		payload = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": input.Prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": input.MaxOutputTokens, "candidateCount": 1}}
		if input.Effort != "" {
			thinking := map[string]any{}
			if geminiThinkingParamForModel(model) == geminiThinkingParamLevel {
				thinking["thinkingLevel"] = input.Effort
			} else {
				budget, _ := geminiThinkingBudgetFor(model, claudeThinkingIntent{tier: input.Effort})
				thinking["thinkingBudget"] = budget
			}
			payload["generationConfig"].(map[string]any)["thinkingConfig"] = thinking
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		var req *http.Request
		switch a.Type {
		case AccountTypeAPIKey:
			req, err = s.buildGeminiAPIKeyRequest(ctx, a, model, body)
		case AccountTypeOAuth:
			req, err = s.buildGeminiOAuthRequest(ctx, a, model, body)
		case AccountTypeServiceAccount:
			req, err = s.buildGeminiServiceAccountRequest(ctx, a, model, body)
		}
		if err != nil {
			return nil, err
		}
		if req == nil {
			return nil, errors.New("unsupported Gemini credentials")
		}
		a.ApplyHeaderOverrides(req.Header)
		return req, nil
	default:
		return nil, errors.New("unsupported protocol")
	}
	if input.APIMode != "anthropic" {
		if a.Platform == PlatformOpenAI && a.IsOpenAIOAuthLike() {
			if s.openaiGatewayService != nil {
				var err error
				token, _, err = s.openaiGatewayService.GetAccessToken(ctx, a)
				if err != nil {
					return nil, err
				}
			} else {
				token = a.GetOpenAIAccessToken()
			}
		} else if a.Platform == PlatformGrok {
			token = a.GetCredential("api_key")
		} else {
			token = a.GetOpenAIProtocolAPIKey()
		}
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("missing upstream credential")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if input.APIMode == "anthropic" {
		for name, value := range claude.DefaultHeaders() {
			req.Header.Set(name, value)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		if a.IsOAuth() {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("anthropic-beta", claude.DefaultBetaHeader)
		} else {
			setAnthropicAPIKeyAuthHeader(req.Header, a, token, a.GetBaseURL())
		}
	} else {
		req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
		req.Header.Set("Authorization", "Bearer "+token)
		if a.Platform == PlatformOpenAI && a.IsOpenAIOAuthLike() {
			req.Host = "chatgpt.com"
			req.Header.Set("OpenAI-Beta", "responses=experimental")
			setOpenAIChatGPTAccountHeaders(req.Header, a)
			enforceCodexIdentityHeadersWithUA(req.Header, a.GetOpenAIUserAgent())
		} else if a.Platform == PlatformOpenAI {
			applyOpenAICodexProbeHeaders(req.Header)
		}
	}
	a.ApplyHeaderOverrides(req.Header)
	// Every diagnostic is an independent conversation, even with shared header overrides.
	session := uuid.NewString()
	req.Header.Set("session_id", isolateOpenAIUpstreamSessionID(0, a, session))
	req.Header.Set("conversation_id", isolateOpenAIUpstreamSessionID(0, a, uuid.NewString()))
	req.Header.Set("x-client-request-id", uuid.NewString())
	return req, nil
}

func qualityProbeFailure(result *accountquality.ProbeResult, status, class, message string) {
	result.ExecutionStatus, result.ErrorClass, result.ErrorMessage = status, class, message
	result.AutoControlEligible = false
}

func qualityProbeTransportFailure(ctx context.Context, result *accountquality.ProbeResult, err error) {
	var ctxErr error
	if ctx != nil {
		ctxErr = ctx.Err()
	}
	var netErr net.Error
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		qualityProbeFailure(result, "timeout", "timeout", "The diagnostic exceeded its timeout.")
	case errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled):
		qualityProbeFailure(result, "cancelled", "cancelled", "The diagnostic was cancelled.")
	case errors.As(err, &netErr) && netErr.Timeout():
		qualityProbeFailure(result, "error", "network_timeout", "The upstream diagnostic transport timed out.")
	default:
		qualityProbeFailure(result, "error", "network", "The upstream diagnostic request failed.")
	}
}

type accountQualityParser struct {
	result    *accountquality.ProbeResult
	textLimit int
	terminal  bool
	stopped   bool
}

func parseAccountQualityResponse(ctx context.Context, resp *http.Response, result *accountquality.ProbeResult, maxTokens int) error {
	limit := min(qualityProbeMaxTextBytes, maxTokens*16)
	p := accountQualityParser{result: result, textLimit: limit}
	limited := &io.LimitedReader{R: resp.Body, N: qualityProbeMaxResponseBytes + 1}
	reader := bufio.NewReader(limited)
	for {
		b, err := reader.Peek(1)
		if err != nil {
			return err
		}
		if b[0] != ' ' && b[0] != '\r' && b[0] != '\n' && b[0] != '\t' {
			break
		}
		reader.ReadByte()
	}
	first, _ := reader.Peek(1)
	if first[0] == '{' || first[0] == '[' {
		body, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if limited.N <= 0 {
			qualityProbeFailure(result, "truncated", "response_limit", "The diagnostic response exceeded its storage bound.")
			return nil
		}
		if !gjson.ValidBytes(body) {
			return errors.New("invalid JSON")
		}
		data := gjson.ParseBytes(body)
		if data.IsArray() {
			for _, item := range data.Array() {
				p.consume(item, true)
				if p.stopped {
					break
				}
			}
		} else {
			p.consume(data, true)
		}
	} else {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 8192), qualityProbeMaxResponseBytes)
		var data strings.Builder
		consume := func() error {
			if data.Len() == 0 {
				return nil
			}
			raw := strings.TrimSpace(data.String())
			data.Reset()
			if raw == "[DONE]" {
				p.stopped = true
				return nil
			}
			if !gjson.Valid(raw) {
				return errors.New("invalid SSE JSON")
			}
			p.consume(gjson.Parse(raw), false)
			return nil
		}
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return err
			}
			line := scanner.Text()
			if line == "" {
				if err := consume(); err != nil {
					return err
				}
				if p.stopped {
					break
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if !p.stopped {
			if err := consume(); err != nil {
				return err
			}
		}
		if limited.N <= 0 && !p.stopped {
			qualityProbeFailure(result, "truncated", "response_limit", "The diagnostic response exceeded its storage bound.")
			return nil
		}
	}
	if !p.terminal && result.ErrorClass == "" {
		qualityProbeFailure(result, "error", "incomplete_stream", "The diagnostic ended without a valid terminal response.")
	}
	return nil
}

func (p *accountQualityParser) appendText(text string) {
	if text == "" || p.stopped {
		return
	}
	remaining := p.textLimit - len(p.result.Text)
	if len(text) > remaining {
		if remaining > 0 {
			text = text[:remaining]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
			p.result.Text += text
		}
		qualityProbeFailure(p.result, "truncated", "output_limit", "The diagnostic reached its local text storage bound.")
		p.stopped, p.terminal = true, true
		return
	}
	p.result.Text += text
}

func (p *accountQualityParser) usage(data gjson.Result) {
	r := p.result
	if v := data.Get("input_tokens"); v.Exists() {
		r.InputTokens = v.Int()
	}
	if v := data.Get("prompt_tokens"); v.Exists() {
		r.InputTokens = v.Int()
	}
	if v := data.Get("output_tokens"); v.Exists() {
		r.OutputTokens = v.Int()
	}
	if v := data.Get("completion_tokens"); v.Exists() {
		r.OutputTokens = v.Int()
	}
	for _, path := range []string{"output_tokens_details.reasoning_tokens", "completion_tokens_details.reasoning_tokens"} {
		if v := data.Get(path); v.Exists() {
			r.ReasoningTokens = v.Int()
		}
	}
}

func (p *accountQualityParser) finish(reason string) {
	if p.stopped {
		return
	}
	p.result.FinishReason, p.terminal = reason, true
	switch reason {
	case "completed", "stop", "end_turn", "stop_sequence", "STOP":
		p.result.ExecutionStatus = "completed"
	case "length", "max_tokens", "max_output_tokens", "MAX_TOKENS":
		qualityProbeFailure(p.result, "truncated", "output_limit", "The upstream stopped at its output limit.")
	case "content_filter", "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		qualityProbeFailure(p.result, "error", "content_filter", "The upstream did not complete because of a content filter.")
	default:
		qualityProbeFailure(p.result, "error", "upstream_terminal", "The upstream ended without a completed text answer.")
	}
}

func (p *accountQualityParser) consume(data gjson.Result, unary bool) {
	r := p.result
	if upstreamError := data.Get("error"); (upstreamError.Exists() && upstreamError.Type != gjson.Null) || data.Get("type").String() == "error" {
		qualityProbeFailure(r, "error", "upstream_error", "The diagnostic upstream returned an error.")
		p.terminal, p.stopped = true, true
		return
	}
	switch r.APIMode {
	case "responses":
		typ := data.Get("type").String()
		if typ == "response.output_text.delta" {
			p.appendText(data.Get("delta").String())
			return
		}
		response := data.Get("response")
		if !response.Exists() {
			response = data
		}
		if model := response.Get("model").String(); model != "" {
			r.ReturnedModel = model
		}
		p.usage(response.Get("usage"))
		if typ == "response.completed" || typ == "response.done" || typ == "response.incomplete" || typ == "response.failed" || unary {
			if r.Text == "" {
				for _, item := range response.Get("output").Array() {
					for _, content := range item.Get("content").Array() {
						if content.Get("type").String() == "output_text" {
							p.appendText(content.Get("text").String())
						}
					}
				}
			}
			status := response.Get("status").String()
			if status == "completed" {
				p.finish("completed")
			} else if status == "incomplete" || typ == "response.incomplete" {
				p.finish(response.Get("incomplete_details.reason").String())
			} else {
				p.finish(status)
			}
			p.stopped = true
		}
	case "chat_completions":
		if model := data.Get("model").String(); model != "" {
			r.ReturnedModel = model
		}
		p.usage(data.Get("usage"))
		choices := data.Get("choices").Array()
		if len(choices) == 0 {
			return
		}
		choice := choices[0]
		if unary {
			p.appendText(choice.Get("message.content").String())
		} else {
			p.appendText(choice.Get("delta.content").String())
		}
		if reason := choice.Get("finish_reason").String(); reason != "" {
			p.finish(reason)
		}
	case "anthropic":
		switch data.Get("type").String() {
		case "message_start":
			r.ReturnedModel = data.Get("message.model").String()
			p.usage(data.Get("message.usage"))
		case "content_block_start":
			if data.Get("content_block.type").String() == "text" {
				p.appendText(data.Get("content_block.text").String())
			}
		case "content_block_delta":
			if data.Get("delta.type").String() == "text_delta" {
				p.appendText(data.Get("delta.text").String())
			}
		case "message_delta":
			p.usage(data.Get("usage"))
			r.FinishReason = data.Get("delta.stop_reason").String()
		case "message_stop":
			p.finish(r.FinishReason)
			p.stopped = true
		default:
			if unary {
				r.ReturnedModel = data.Get("model").String()
				p.usage(data.Get("usage"))
				for _, content := range data.Get("content").Array() {
					if content.Get("type").String() == "text" {
						p.appendText(content.Get("text").String())
					}
				}
				p.finish(data.Get("stop_reason").String())
			}
		}
	case "gemini":
		if response := data.Get("response"); response.Exists() {
			data = response
		}
		if model := data.Get("modelVersion").String(); model != "" {
			r.ReturnedModel = model
		}
		if usage := data.Get("usageMetadata"); usage.Exists() {
			r.InputTokens = usage.Get("promptTokenCount").Int()
			r.ReasoningTokens = usage.Get("thoughtsTokenCount").Int()
			r.OutputTokens = usage.Get("candidatesTokenCount").Int() + r.ReasoningTokens
		}
		if data.Get("promptFeedback.blockReason").String() != "" {
			p.finish("content_filter")
			p.stopped = true
			return
		}
		candidates := data.Get("candidates").Array()
		if len(candidates) == 0 {
			return
		}
		candidate := candidates[0]
		for _, part := range candidate.Get("content.parts").Array() {
			if !part.Get("thought").Bool() {
				p.appendText(part.Get("text").String())
			}
		}
		if reason := candidate.Get("finishReason").String(); reason != "" {
			p.finish(reason)
			p.stopped = true
		}
	}
}
