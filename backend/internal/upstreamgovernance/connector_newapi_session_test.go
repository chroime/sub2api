package upstreamgovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newAPISessionFixtureConnector(t *testing.T, handler func(*http.Request) (int, http.Header, string)) Connector {
	t.Helper()
	return NewConnector(func(context.Context, Site) (HTTPDoer, error) {
		return connectorDoer(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "https", r.URL.Scheme)
			require.Equal(t, "upstream.example", r.URL.Host)
			status, headers, body := handler(r)
			return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}), nil
	})
}

func newAPISessionFixture(t *testing.T) Session {
	t.Helper()
	var session Session
	require.NoError(t, json.Unmarshal([]byte(`{"access_token":"fixture-old-access","refresh_token":"fixture-session.fixture-old-secret","session_id":"fixture-session","user_id":42,"auth_variant":"newapi_session","user_agent":"Fixture Browser/2","cookies":{"cf_clearance":"fixture-clearance"}}`), &session))
	return session
}

func newAPISessionResponse(expires int64) string {
	return fmt.Sprintf(`{"success":true,"data":{"access_token":"fixture-new-access","access_expires_at":%d,"token_type":"Bearer","user":{"id":42},"session":{"sid":"fixture-session","expires_at":%d}}}`, expires, time.Now().Add(30*24*time.Hour).Unix())
}

func newAPIRefreshHeaders() http.Header {
	return http.Header{"Set-Cookie": {"new_api_refresh=fixture-session.fixture-new-secret; Path=/api/user/auth; HttpOnly; Secure; SameSite=Strict"}}
}

func TestNewAPISessionRefreshRotatesCookieOnlyOnRefreshEndpoint(t *testing.T) {
	refreshes, reads := 0, 0
	expires := time.Now().Add(15 * time.Minute).Unix()
	c := newAPISessionFixtureConnector(t, func(r *http.Request) (int, http.Header, string) {
		require.Equal(t, "Fixture Browser/2", r.UserAgent())
		clearance, err := r.Cookie("cf_clearance")
		require.NoError(t, err)
		require.Equal(t, "fixture-clearance", clearance.Value)
		if r.URL.Path == "/api/user/auth/refresh" {
			refreshes++
			require.Equal(t, "POST", r.Method)
			require.Empty(t, r.Header.Get("Authorization"))
			require.Equal(t, "https://upstream.example", r.Header.Get("Origin"))
			require.Equal(t, "fixture-session", r.Header.Get("X-Auth-Session"))
			cookie, err := r.Cookie("new_api_refresh")
			require.NoError(t, err)
			require.Equal(t, "fixture-session.fixture-old-secret", cookie.Value)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Empty(t, body)
			return 200, newAPIRefreshHeaders(), newAPISessionResponse(expires)
		}
		reads++
		require.Equal(t, "/api/user/self", r.URL.Path)
		require.Equal(t, "Bearer fixture-new-access", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("New-Api-User"))
		_, err = r.Cookie("new_api_refresh")
		require.ErrorIs(t, err, http.ErrNoCookie, "refresh credentials must not accompany normal management requests")
		return 200, nil, `{"success":true,"data":{"id":42}}`
	})
	site := Site{Platform: "newapi", BaseURL: "https://upstream.example/"}
	got, err := c.(sessionRefresher).Refresh(t.Context(), site, newAPISessionFixture(t))
	require.NoError(t, err)
	require.Equal(t, "fixture-new-access", got.AccessToken)
	require.Equal(t, "fixture-session.fixture-new-secret", got.RefreshToken)
	require.Equal(t, "fixture-session", got.SessionID)
	require.EqualValues(t, 42, got.UserID)
	require.Equal(t, expires, got.ExpiresAt.Unix())
	require.Equal(t, 1, refreshes)
	require.Zero(t, reads, "new credentials must be returned for persistence before identity verification")
	_, err = c.(sessionVerifier).VerifySession(t.Context(), site, got)
	require.NoError(t, err)
	require.Equal(t, 1, reads)
}

func TestNewAPISessionImportPinsModernIdentity(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		require.Equal(t, "/api/user/self", r.URL.Path)
		require.Empty(t, r.Header.Get("New-Api-User"), "modern dashboard sessions must not select legacy PAT authentication")
		_, err := r.Cookie("new_api_refresh")
		require.ErrorIs(t, err, http.ErrNoCookie)
		return 200, `{"success":true,"data":{"id":42}}`
	})
	var input LoginInput
	require.NoError(t, json.Unmarshal([]byte(`{"session_token":"fixture-access","refresh_token":"fixture-session.fixture-old-secret","session_id":"fixture-session","user_id":42,"expires_in":900,"user_agent":"Fixture Browser/2"}`), &input))
	got, challenge, err := c.Login(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, input)
	require.NoError(t, err)
	require.Nil(t, challenge)
	require.Equal(t, "newapi_session", got.AuthVariant)
	require.Equal(t, "fixture-session", got.SessionID)
	require.Equal(t, input.RefreshToken, got.RefreshToken)
}

func TestNewAPISessionPasswordAnd2FALoginCaptureRefreshCookie(t *testing.T) {
	for _, secondFactor := range []bool{false, true} {
		t.Run(fmt.Sprint(secondFactor), func(t *testing.T) {
			expires := time.Now().Add(15 * time.Minute).Unix()
			c := newAPISessionFixtureConnector(t, func(r *http.Request) (int, http.Header, string) {
				switch r.URL.Path {
				case "/api/status":
					return 200, nil, `{"success":true,"data":{"turnstile_check":false}}`
				case "/api/user/login/encryption-key":
					return 404, nil, `{}`
				case "/api/user/login", "/api/user/login/2fa":
					if secondFactor {
						require.Equal(t, "/api/user/login/2fa", r.URL.Path)
					}
					return 200, newAPIRefreshHeaders(), newAPISessionResponse(expires)
				case "/api/user/self":
					return 200, nil, `{"success":true,"data":{"id":42}}`
				}
				t.Fatalf("unexpected request %s", r.URL.Path)
				return 500, nil, `{}`
			})
			input := LoginInput{Username: "fixture-user", Password: "fixture-password"}
			if secondFactor {
				input.ChallengeToken, input.OTP = "fixture-flow", "123456"
			}
			got, challenge, err := c.Login(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, input)
			require.NoError(t, err)
			require.Nil(t, challenge)
			require.Equal(t, "fixture-session.fixture-new-secret", got.RefreshToken)
			require.Equal(t, "fixture-session", got.SessionID)
			require.Equal(t, "newapi_session", got.AuthVariant)
			require.NotNil(t, got.ExpiresAt)
			require.Equal(t, expires, got.ExpiresAt.Unix())
		})
	}
}

func TestNewAPISessionRefreshRejectsInvalidRotation(t *testing.T) {
	valid := newAPISessionResponse(time.Now().Add(15 * time.Minute).Unix())
	for _, tc := range []struct {
		name   string
		header http.Header
		body   string
	}{
		{"missing rotated cookie", nil, valid},
		{"unrelated cookie only", http.Header{"Set-Cookie": {"new_api_has_session=1; Path=/"}}, valid},
		{"deleted cookie", http.Header{"Set-Cookie": {"new_api_refresh=; Max-Age=0; Path=/api/user/auth"}}, valid},
		{"expired cookie", http.Header{"Set-Cookie": {"new_api_refresh=fixture-session.fixture-secret; Expires=Thu, 01 Jan 1970 00:00:01 GMT; Path=/api/user/auth"}}, valid},
		{"foreign domain", http.Header{"Set-Cookie": {"new_api_refresh=fixture-session.fixture-secret; Domain=other.example; Path=/api/user/auth"}}, valid},
		{"wrong cookie path", http.Header{"Set-Cookie": {"new_api_refresh=fixture-session.fixture-secret; Path=/other"}}, valid},
		{"cookie session mismatch", http.Header{"Set-Cookie": {"new_api_refresh=other-session.fixture-secret; Path=/api/user/auth"}}, valid},
		{"ambiguous duplicate cookie", http.Header{"Set-Cookie": {"new_api_refresh=fixture-session.first; Path=/api/user/auth", "new_api_refresh=fixture-session.second; Path=/api/user/auth"}}, valid},
		{"user mismatch", newAPIRefreshHeaders(), strings.Replace(valid, `"id":42`, `"id":43`, 1)},
		{"session mismatch", newAPIRefreshHeaders(), strings.Replace(valid, `"sid":"fixture-session"`, `"sid":"other-session"`, 1)},
		{"expired access", newAPIRefreshHeaders(), newAPISessionResponse(time.Now().Add(-time.Minute).Unix())},
		{"missing access expiry", newAPIRefreshHeaders(), strings.Replace(valid, `"access_expires_at":`, `"ignored_expiry":`, 1)},
		{"wrong token type", newAPIRefreshHeaders(), strings.Replace(valid, `"Bearer"`, `"Basic"`, 1)},
		{"unsuccessful response", newAPIRefreshHeaders(), strings.Replace(valid, `"success":true`, `"success":false`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := newAPISessionFixtureConnector(t, func(*http.Request) (int, http.Header, string) {
				calls++
				return 200, tc.header, tc.body
			})
			got, err := c.(sessionRefresher).Refresh(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, newAPISessionFixture(t))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "fixture-secret")
			require.Empty(t, got.AccessToken)
			require.Equal(t, 1, calls)
		})
	}
}

func TestNewAPISessionRefreshRequiresCredentialAndNeverForwardsItToOtherRoutes(t *testing.T) {
	calls := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		calls++
		_, err := r.Cookie("new_api_refresh")
		require.ErrorIs(t, err, http.ErrNoCookie)
		if r.URL.Path == "/api/user/self" {
			return 200, `{"success":true,"data":{"id":42}}`
		}
		return 200, `{"code":0,"data":{"id":42}}`
	})
	site := Site{Platform: "newapi", BaseURL: "https://upstream.example"}
	for _, credential := range []string{"", "not-a-refresh-cookie", "other-session.fixture-secret", "fixture-session.fixture\r\nsecret"} {
		session := newAPISessionFixture(t)
		session.RefreshToken = credential
		_, err := c.(sessionRefresher).Refresh(t.Context(), site, session)
		require.Error(t, err)
	}
	require.Zero(t, calls)
	session := newAPISessionFixture(t)
	session.Cookies["new_api_refresh"] = "fixture-forbidden-general-cookie"
	_, err := c.(sessionVerifier).VerifySession(t.Context(), site, session)
	require.NoError(t, err)
	_, err = c.(sessionVerifier).VerifySession(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, session)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestNewAPISessionImportRejectsMismatchedRefreshIdentityBeforeHTTP(t *testing.T) {
	c := fixtureConnector(t, func(*http.Request) (int, string) {
		t.Fatal("invalid import must not reach upstream")
		return 200, `{}`
	})
	for _, raw := range []string{
		`{"session_token":"fixture-access","session_id":"fixture-session","refresh_token":"other-session.fixture-secret","expires_in":900}`,
		`{"session_token":"fixture-access","session_id":"fixture\r\nsecret"}`,
		`{"session_id":"fixture-session"}`,
	} {
		var input LoginInput
		require.NoError(t, json.Unmarshal([]byte(raw), &input))
		_, _, err := c.Login(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, input)
		require.ErrorIs(t, err, ErrInvalid)
	}
}
