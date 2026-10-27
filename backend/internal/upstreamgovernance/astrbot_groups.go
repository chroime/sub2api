package upstreamgovernance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type AstrBotGroupsQuery struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key,omitempty"`
	TimeoutSeconds int64  `json:"timeout_seconds"`
}

type AstrBotGroup struct {
	UMO       string `json:"umo"`
	BotID     string `json:"bot_id"`
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
}

type AstrBotGroupsResult struct {
	Groups   []AstrBotGroup `json:"groups"`
	Complete bool           `json:"complete"`
}

// Query errors carry a stable public code, never an upstream body or secret.
type AstrBotGroupsError struct{ Code string }

func (e *AstrBotGroupsError) Error() string { return e.Code }

// ListAstrBotGroups queries a live OneBot group-list bridge in AstrBot. It does
// not read historical sessions and does not save the caller's settings draft.
func (s *Service) ListAstrBotGroups(ctx context.Context, query AstrBotGroupsQuery) (AstrBotGroupsResult, error) {
	fail := func(code string) (AstrBotGroupsResult, error) {
		return AstrBotGroupsResult{}, &AstrBotGroupsError{Code: code}
	}
	base := strings.TrimSpace(query.BaseURL)
	u, err := url.ParseRequestURI(base)
	if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || query.TimeoutSeconds < 1 || query.TimeoutSeconds > 600 {
		return fail("astrbot_not_configured")
	}
	key := query.APIKey
	if strings.TrimSpace(key) == "" {
		settings, readErr := s.GetRuntimeGlobalSettings(ctx)
		if readErr != nil {
			return AstrBotGroupsResult{}, readErr
		}
		// A stored credential belongs to its saved address. An unsaved change
		// must supply its own key, even if a key exists for the original host.
		if base != strings.TrimSpace(settings.AstrBot.BaseURL) {
			return fail("astrbot_not_configured")
		}
		key = settings.AstrBot.APIKey
	}
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
		return fail("astrbot_not_configured")
	}
	timeout := min(query.TimeoutSeconds, int64(30))
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(base, "/")
	if !strings.HasSuffix(endpoint, "/api/v1") {
		endpoint += "/api/v1"
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint+"/im/groups", nil)
	if err != nil {
		return fail("astrbot_not_configured")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fail("astrbot_unavailable")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fail("astrbot_auth_failed")
	case http.StatusForbidden:
		return fail("astrbot_scope_required")
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return fail("astrbot_groups_unsupported")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail("astrbot_unavailable")
	}
	const maxGroupResponse = 2 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGroupResponse+1))
	if err != nil || len(data) > maxGroupResponse {
		return fail("astrbot_invalid_response")
	}
	var envelope struct {
		Status string              `json:"status"`
		Data   AstrBotGroupsResult `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return fail("astrbot_invalid_response")
	}
	if envelope.Status != "ok" {
		return fail("astrbot_unavailable")
	}
	if !envelope.Data.Complete {
		return fail("astrbot_groups_incomplete")
	}
	result := envelope.Data
	if result.Groups == nil {
		return fail("astrbot_invalid_response")
	}
	seen := make(map[string]bool, len(result.Groups))
	for _, group := range result.Groups {
		if group.BotID == "" || strings.ContainsAny(group.BotID, ":\r\n") || group.GroupID == "" || group.GroupID == "0" || group.GroupName == "" || group.UMO != group.BotID+":GroupMessage:"+group.GroupID || seen[group.UMO] {
			return fail("astrbot_invalid_response")
		}
		for _, digit := range group.GroupID {
			if digit < '0' || digit > '9' {
				return fail("astrbot_invalid_response")
			}
		}
		seen[group.UMO] = true
	}
	sort.SliceStable(result.Groups, func(i, j int) bool {
		left, right := result.Groups[i], result.Groups[j]
		if left.GroupName != right.GroupName {
			return left.GroupName < right.GroupName
		}
		return left.UMO < right.UMO
	})
	return result, nil
}
