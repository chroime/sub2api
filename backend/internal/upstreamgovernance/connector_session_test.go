package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConnectorSessionCapturesPairAndKeepsUserAgent(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		require.Equal(t, "FixtureBrowser/1.0", r.UserAgent())
		if r.URL.Path == "/api/v1/auth/login" {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "ticket", body["tencent_captcha_ticket"])
			require.Equal(t, "nonce", body["tencent_captcha_randstr"])
			require.Equal(t, "proof", body["turnstile_token"])
			return 200, `{"code":0,"data":{"access_token":"fixture-access","refresh_token":"fixture-refresh","expires_in":3600,"user":{"id":42}}}`
		}
		require.Equal(t, "/api/v1/user/profile", r.URL.Path)
		require.Equal(t, "Bearer fixture-access", r.Header.Get("Authorization"))
		return 200, `{"code":0,"data":{"id":42}}`
	})
	var input LoginInput
	require.NoError(t, json.Unmarshal([]byte(`{"username":"fixture@example.com","password":"fixture-password","turnstile_token":"proof","tencent_captcha_ticket":"ticket","tencent_captcha_randstr":"nonce","user_agent":"FixtureBrowser/1.0"}`), &input))
	before := time.Now().UTC()
	session, challenge, err := c.Login(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, input)
	require.NoError(t, err)
	require.Nil(t, challenge)
	var actual map[string]any
	require.NoError(t, json.Unmarshal([]byte(canonical(session)), &actual))
	require.Equal(t, "fixture-refresh", actual["refresh_token"])
	require.Equal(t, "FixtureBrowser/1.0", actual["user_agent"])
	issued, err := time.Parse(time.RFC3339Nano, actual["issued_at"].(string))
	require.NoError(t, err)
	expires, err := time.Parse(time.RFC3339Nano, actual["expires_at"].(string))
	require.NoError(t, err)
	require.False(t, issued.Before(before))
	require.Equal(t, time.Hour, expires.Sub(issued))
}

func TestConnectorSessionCaptchaProviders(t *testing.T) {
	for reason, provider := range map[string]string{"TURNSTILE_VERIFICATION_FAILED": "turnstile", "TENCENT_CAPTCHA_VERIFICATION_FAILED": "tencent", "ALIYUN_CAPTCHA_VERIFICATION_FAILED": "aliyun", "CAPTCHA_VERIFICATION_FAILED": "unknown"} {
		t.Run(provider, func(t *testing.T) {
			c := fixtureConnector(t, func(*http.Request) (int, string) { return 400, `{"reason":"` + reason + `"}` })
			_, challenge, err := c.Login(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture@example.com", Password: "fixture-password"})
			require.NoError(t, err)
			require.NotNil(t, challenge)
			var actual map[string]any
			require.NoError(t, json.Unmarshal([]byte(canonical(challenge)), &actual))
			require.Equal(t, provider, actual["provider"])
		})
	}
}

func TestConnectorSessionDetectionProvider(t *testing.T) {
	for _, provider := range []string{"tencent", "aliyun"} {
		t.Run(provider, func(t *testing.T) {
			c := fixtureConnector(t, func(*http.Request) (int, string) {
				return 200, `{"code":0,"data":{"site_name":"Fixture","` + provider + `_captcha_enabled":true,"turnstile_site_key":"irrelevant"}}`
			})
			got, err := c.(siteDetector).Detect(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"})
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, json.Unmarshal([]byte(canonical(got)), &actual))
			require.Equal(t, provider, actual["captcha_provider"])
			require.Empty(t, got.CaptchaSiteKey)
		})
	}
}

func TestConnectorSessionRefreshContract(t *testing.T) {
	calls := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		calls++
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/api/v1/auth/refresh", r.URL.Path)
		require.Equal(t, "FixtureBrowser/1.0", r.UserAgent())
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, map[string]any{"refresh_token": "fixture-refresh"}, body)
		return 200, `{"code":0,"data":{"access_token":"rotated-access","refresh_token":"rotated-refresh","expires_in":3600}}`
	})
	refresher, ok := c.(interface {
		Refresh(context.Context, Site, Session) (Session, error)
	})
	require.True(t, ok, "Sub2API connector must expose refresh capability")
	var session Session
	require.NoError(t, json.Unmarshal([]byte(`{"access_token":"old","refresh_token":"fixture-refresh","user_id":42,"user_agent":"FixtureBrowser/1.0","auth_variant":"bearer"}`), &session))
	got, err := refresher.Refresh(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, session)
	require.NoError(t, err)
	require.Equal(t, "rotated-access", got.AccessToken)
	require.Equal(t, int64(42), got.UserID)
	require.Contains(t, canonical(got), `"refresh_token":"rotated-refresh"`)
	require.Equal(t, 1, calls, "rotation must not use the new pair before the service persists it")
	_, err = refresher.Refresh(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, session)
	require.True(t, errors.Is(err, ErrUnsupported))
	require.Equal(t, 1, calls)
}

func TestConnectorSessionForwardsOnlyApprovedBrowserCookies(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		require.Equal(t, "FixtureBrowser/1", r.UserAgent())
		cookie, err := r.Cookie("cf_clearance")
		require.NoError(t, err)
		require.Equal(t, "fixture-clearance", cookie.Value)
		_, err = r.Cookie("other")
		require.Error(t, err)
		return 200, `{"code":0,"data":{"id":42}}`
	})
	_, err := c.(sessionVerifier).VerifySession(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42, UserAgent: "FixtureBrowser/1", Cookies: map[string]string{"cf_clearance": "fixture-clearance", "other": "ignored"}})
	require.NoError(t, err)
}

func TestConnectorSessionManualImportAndTOTPKeepRefreshMetadata(t *testing.T) {
	for _, mode := range []string{"import", "totp"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			c := fixtureConnector(t, func(r *http.Request) (int, string) {
				require.Equal(t, "FixtureBrowser/2", r.UserAgent())
				if r.Method == "POST" {
					posts++
					require.Equal(t, "/api/v1/auth/login/2fa", r.URL.Path)
					var body map[string]string
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, map[string]string{"temp_token": "fixture-challenge", "totp_code": "123456"}, body)
					return 200, `{"code":0,"data":{"access_token":"fixture-access","refresh_token":"fixture-refresh","expires_in":600,"user":{"id":42}}}`
				}
				require.Equal(t, "/api/v1/user/profile", r.URL.Path)
				return 200, `{"code":0,"data":{"id":42}}`
			})
			input := LoginInput{UserAgent: "FixtureBrowser/2"}
			if mode == "import" {
				input.SessionToken, input.RefreshToken, input.ExpiresIn = "fixture-access", "fixture-refresh", 600
			} else {
				input.ChallengeToken, input.OTP = "fixture-challenge", "123456"
			}
			session, challenge, err := c.Login(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, input)
			require.NoError(t, err)
			require.Nil(t, challenge)
			require.Equal(t, "fixture-refresh", session.RefreshToken)
			require.Equal(t, 10*time.Minute, session.ExpiresAt.Sub(*session.IssuedAt))
			if mode == "import" {
				require.Zero(t, posts)
			} else {
				require.Equal(t, 1, posts)
			}
		})
	}
}

func TestConnectorSessionRejectsInvalidInputsBeforeHTTP(t *testing.T) {
	calls := 0
	c := fixtureConnector(t, func(*http.Request) (int, string) { calls++; return 200, `{}` })
	inputs := []LoginInput{
		{SessionToken: "fixture\r\nInjected: yes"},
		{SessionToken: strings.Repeat("x", maxSessionTokenLength+1)},
		{SessionToken: "fixture", RefreshToken: "rotation\x00"},
		{SessionToken: "fixture", RefreshToken: "rotation"},
		{SessionToken: "fixture", UserAgent: "Browser\nInjected: yes"},
		{SessionToken: "fixture", UserAgent: strings.Repeat("x", 1025)},
		{SessionToken: "fixture", ExpiresIn: -1},
		{SessionToken: "fixture", ExpiresIn: maxSessionLifetimeSeconds + 1},
		{RefreshToken: "fixture"},
		{ExpiresIn: 600},
		{TencentCaptchaTicket: "ticket"},
		{TencentCaptchaRandstr: "randstr"},
		{CaptchaToken: "legacy", TurnstileToken: "different"},
	}
	for _, input := range inputs {
		_, _, err := c.Login(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, input)
		require.ErrorIs(t, err, ErrInvalid)
	}
	require.Zero(t, calls)
}

func TestConnectorSessionDetectionConflictingProvidersIsUnknown(t *testing.T) {
	c := fixtureConnector(t, func(*http.Request) (int, string) {
		return 200, `{"code":0,"data":{"site_name":"Fixture","turnstile_enabled":true,"tencent_captcha_enabled":true,"turnstile_site_key":"irrelevant"}}`
	})
	got, err := c.(siteDetector).Detect(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"})
	require.NoError(t, err)
	require.True(t, got.CaptchaRequired)
	require.Equal(t, CaptchaUnknown, got.CaptchaProvider)
	require.Empty(t, got.CaptchaSiteKey)
}
