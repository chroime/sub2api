package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type openAICodexTicketProbeFailure struct {
	Code       string
	HTTPStatus int
	RetryAt    time.Time
}

func (e *openAICodexTicketProbeFailure) Error() string { return e.Code }

func openAICodexTicketStreamFailure(raw json.RawMessage) *openAICodexTicketProbeFailure {
	var data struct {
		Code            string `json:"code"`
		ResetsInSeconds int64  `json:"resets_in_seconds"`
		ResetsAt        int64  `json:"resets_at"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &data) != nil {
		return nil
	}
	switch data.Code {
	case "usage_limit_reached", "rate_limit_exceeded", "insufficient_quota":
		body, _ := json.Marshal(map[string]any{"error": data})
		return &openAICodexTicketProbeFailure{Code: "rate_limited", HTTPStatus: 429, RetryAt: openAICodexTicketRetryAt(nil, body, time.Now())}
	case "invalid_api_key", "token_expired", "account_deactivated":
		return &openAICodexTicketProbeFailure{Code: "auth_failed", HTTPStatus: 401}
	default:
		return &openAICodexTicketProbeFailure{Code: "stream_invalid"}
	}
}

func (s *OpenAIGatewayService) openAICodexTicketRoutes(ctx context.Context, mode string, cfg config.OpenAICodexTicketConfig) []openAICodexTicketRoute {
	routes := make([]openAICodexTicketRoute, 0, 33)
	seen := make(map[string]bool)
	if proxy := s.openAICodexTicketHarvestProxyForMode(ctx, mode); proxy != "" && ValidateOpenAICodexTicketHarvestProxyURL(proxy) == nil {
		routes = append(routes, openAICodexTicketRoute{Key: "standalone", Name: "standalone", URL: proxy})
		seen[proxy] = true
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	repo := s.openaiCodexTicketProxyRepo
	s.openaiCodexTicketRuntimeMu.Unlock()
	if repo == nil || len(cfg.HarvestProxyIDs) == 0 {
		return routes
	}
	ids := make([]int64, 0, 32)
	idSet := make(map[int64]bool)
	for _, id := range cfg.HarvestProxyIDs {
		if id > 0 && !idSet[id] {
			ids = append(ids, id)
			idSet[id] = true
			if len(ids) == 32 {
				break
			}
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	proxies, err := repo.ListByIDs(readCtx, ids)
	if err != nil {
		return routes
	}
	byID := make(map[int64]Proxy, len(proxies))
	for _, proxy := range proxies {
		byID[proxy.ID] = proxy
	}
	for _, id := range ids {
		proxy, ok := byID[id]
		if !ok || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
			continue
		}
		url := proxy.URL()
		if seen[url] || ValidateOpenAICodexTicketHarvestProxyURL(url) != nil {
			continue
		}
		seen[url] = true
		routeID := id
		routes = append(routes, openAICodexTicketRoute{Key: fmt.Sprintf("proxy:%d", id), ID: &routeID, Name: proxy.Name, URL: url})
	}
	return routes
}

func chooseOpenAICodexTicketRoute(routes []openAICodexTicketRoute, state openAICodexTicketRuntime) *openAICodexTicketRoute {
	if len(routes) == 0 {
		return nil
	}
	for i := range routes {
		if routes[i].Key == state.PreferredRoute && routes[i].Key != state.FailedRoute {
			return &routes[i]
		}
	}
	for i := range routes {
		if routes[i].Key == state.FailedRoute {
			return &routes[(i+1)%len(routes)]
		}
	}
	return &routes[0]
}

func (s *OpenAIGatewayService) failOpenAICodexTicketProbe(ctx context.Context, account *Account, mode, model, phase string, route *openAICodexTicketRoute, state openAICodexTicketRuntime, status int, err error) {
	code := "network_error"
	next := time.Now().Add(20 * time.Second)
	var failure *openAICodexTicketProbeFailure
	if errors.As(err, &failure) {
		code = failure.Code
		if failure.HTTPStatus != 0 {
			status = failure.HTTPStatus
		}
		if failure.RetryAt.After(next) {
			next = failure.RetryAt
		}
	}
	if ctx.Err() != nil {
		code = "cancelled"
	}
	state.LastError = code
	if route != nil {
		state.FailedRoute = route.Key
		state.PreferredRoute = ""
	}
	state.NextAttemptAt = next
	outcome := "cooldown"
	if status == 429 {
		state.NextAttemptAt = next
		minNext := time.Now().Add(300 * time.Second)
		if minNext.After(state.NextAttemptAt) {
			state.NextAttemptAt = minNext
		}
		state.LastError = "rate_limited"
		phase = "cooldown"
	}
	if status == 401 || status == 403 {
		state.AuthBlocked = true
		state.LastError = "auth_failed"
		phase = "auth"
		outcome = "auth_blocked"
	}
	s.saveOpenAICodexTicketRuntime(ctx, account, mode, model, state)
	s.recordOpenAICodexTicketEvent(account, mode, model, phase, outcome, state.LastError, status, route, nil, state.NextAttemptAt)
}
