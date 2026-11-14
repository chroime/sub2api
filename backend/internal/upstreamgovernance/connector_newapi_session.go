package upstreamgovernance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const newAPIRefreshPath = "/api/user/auth/refresh"
const newAPIRefreshCookieName = "new_api_refresh"
const newAPISessionVariant = "newapi_session"

func validNewAPISessionID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// The identifier is a routing/binding hint, not proof of identity. The upstream
// validates the secret and the service verifies the returned user before use.
func newAPIRefreshSessionID(value string) (string, bool) {
	sid, secret, ok := strings.Cut(value, ".")
	if !ok || !validNewAPISessionID(sid) || secret == "" || !validSessionCookie(value) || strings.ContainsAny(secret, " .\t") {
		return "", false
	}
	return sid, true
}

func hasSessionRefreshCredential(site Site, session Session) bool {
	if site.Platform == "sub2api" {
		return session.RefreshToken != ""
	}
	if site.Platform != "newapi" || session.AuthVariant != newAPISessionVariant || session.SessionID == "" {
		return false
	}
	sid, ok := newAPIRefreshSessionID(session.RefreshToken)
	return ok && sid == session.SessionID
}

func (c *platformConnector) refreshNewAPISession(ctx context.Context, site Site, session Session) (Session, error) {
	if session.AuthVariant != newAPISessionVariant {
		return Session{}, ErrUnsupported
	}
	if !hasSessionRefreshCredential(site, session) || !validHeaderValue(session.UserAgent, 1024) {
		return Session{}, ErrReauth
	}
	headers := make(http.Header)
	headers.Set("Origin", strings.TrimRight(site.BaseURL, "/"))
	headers.Set("X-Auth-Session", session.SessionID)
	// Refresh is cookie-authenticated. Never send the expired access bearer.
	auth := Session{RefreshToken: session.RefreshToken, SessionID: session.SessionID, AuthVariant: newAPISessionVariant, UserAgent: session.UserAgent, Cookies: session.Cookies}
	response, _, err := c.request(ctx, site, auth, "POST", newAPIRefreshPath, nil, headers, true)
	if err != nil {
		return Session{}, err
	}
	return newAPISessionFromResponse(site, session, response, true)
}

func newAPISessionFromResponse(site Site, session Session, response connectorResponse, requireRefresh bool) (Session, error) {
	var data struct {
		AccessToken     string `json:"access_token"`
		TokenType       string `json:"token_type"`
		AccessExpiresAt int64  `json:"access_expires_at"`
		User            struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Session struct {
			SID string `json:"sid"`
		} `json:"session"`
	}
	if json.Unmarshal(response.Data, &data) != nil {
		return Session{}, ErrUnsupported
	}
	// Older NewAPI bearer responses have no browser-session metadata.
	if !requireRefresh && data.AccessExpiresAt == 0 && data.Session.SID == "" {
		return session, nil
	}
	now := time.Now().UTC()
	expires := time.Unix(data.AccessExpiresAt, 0).UTC()
	if data.AccessToken == "" || !validHeaderValue(data.AccessToken, maxSessionTokenLength) || !strings.EqualFold(data.TokenType, "Bearer") || !validNewAPISessionID(data.Session.SID) || !expires.After(now) || expires.After(now.Add(time.Duration(maxSessionLifetimeSeconds)*time.Second)) || data.User.ID <= 0 {
		return Session{}, ErrUnsupported
	}
	if session.UserID > 0 && data.User.ID != session.UserID || session.SessionID != "" && data.Session.SID != session.SessionID {
		return Session{}, ErrReauth
	}
	refresh, err := newAPIRefreshCookie(site, response.Header, data.Session.SID, now)
	if err != nil {
		return Session{}, err
	}
	if requireRefresh && refresh == "" {
		return Session{}, ErrUnsupported
	}
	session.AccessToken, session.RefreshToken = data.AccessToken, refresh
	session.UserID, session.SessionID = data.User.ID, data.Session.SID
	session.AuthVariant = newAPISessionVariant
	session.IssuedAt, session.ExpiresAt = &now, &expires
	return session, nil
}

func newAPIRefreshCookie(site Site, header http.Header, expectedSID string, now time.Time) (string, error) {
	base, err := url.Parse(site.BaseURL)
	if err != nil || base.Hostname() == "" {
		return "", ErrInvalid
	}
	response := http.Response{Header: header}
	value, found := "", false
	for _, cookie := range response.Cookies() {
		if cookie.Name != newAPIRefreshCookieName {
			continue
		}
		if found || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(now) || cookie.Path != "" && cookie.Path != "/" && cookie.Path != "/api/user/auth" || cookie.Domain != "" && !strings.EqualFold(strings.TrimPrefix(cookie.Domain, "."), base.Hostname()) {
			return "", ErrUnsupported
		}
		found = true
		sid, ok := newAPIRefreshSessionID(cookie.Value)
		if !ok {
			return "", ErrUnsupported
		}
		if sid != expectedSID {
			return "", ErrReauth
		}
		value = cookie.Value
	}
	return value, nil
}
