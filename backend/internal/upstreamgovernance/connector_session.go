package upstreamgovernance

import (
	"context"
	"strings"
	"time"
	"unicode"
)

const defaultSessionUserAgent = "sub2api-upstream-governance/1.0"
const maxSessionTokenLength = 16384
const maxSessionLifetimeSeconds = int64(366 * 24 * 60 * 60)

type captchaError struct{ provider CaptchaProvider }

func (e *captchaError) Error() string { return "upstream captcha required" }
func (e *captchaError) Unwrap() error { return errConnectorCaptcha }

func captchaReason(reason string) error {
	var provider CaptchaProvider
	switch reason {
	case "TURNSTILE_VERIFICATION_FAILED", "TURNSTILE_REQUIRED":
		provider = CaptchaTurnstile
	case "TENCENT_CAPTCHA_VERIFICATION_FAILED", "TENCENT_CAPTCHA_REQUIRED":
		provider = CaptchaTencent
	case "ALIYUN_CAPTCHA_VERIFICATION_FAILED", "ALIYUN_CAPTCHA_REQUIRED":
		provider = CaptchaAliyun
	case "CAPTCHA_VERIFICATION_FAILED", "CAPTCHA_REQUIRED":
		provider = CaptchaUnknown
	default:
		return nil
	}
	return &captchaError{provider: provider}
}

func validHeaderValue(value string, maxLength int) bool {
	if len(value) > maxLength {
		return false
	}
	for _, c := range []byte(value) {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

func sessionCookieAllowed(name string) bool { return name == "session" || name == "cf_clearance" }

func hasSessionCredential(session Session) bool {
	return session.AccessToken != "" || session.Cookies["session"] != ""
}

func validSessionCookie(value string) bool {
	if !validHeaderValue(value, maxSessionTokenLength) {
		return false
	}
	return !strings.ContainsAny(value, "\";,\\")
}

func validateLoginInput(input LoginInput) error {
	if len(input.Username) > 320 || len(input.Password) > 4096 || strings.ContainsFunc(input.Username, unicode.IsControl) || strings.ContainsRune(input.Password, '\x00') {
		return ErrInvalid
	}
	if !validHeaderValue(input.SessionToken, maxSessionTokenLength) || !validHeaderValue(input.RefreshToken, maxSessionTokenLength) || !validHeaderValue(input.UserAgent, 1024) || len(input.OTP) > 32 || len(input.ChallengeToken) > maxSessionTokenLength || input.ExpiresIn < 0 || input.ExpiresIn > maxSessionLifetimeSeconds || input.UserID < 0 {
		return ErrInvalid
	}
	for _, proof := range []string{input.CaptchaToken, input.TurnstileToken, input.TencentCaptchaTicket, input.TencentCaptchaRandstr} {
		if len(proof) > maxSessionTokenLength || strings.ContainsRune(proof, '\x00') {
			return ErrInvalid
		}
	}
	if (input.TencentCaptchaTicket == "") != (input.TencentCaptchaRandstr == "") || input.CaptchaToken != "" && input.TurnstileToken != "" && input.CaptchaToken != input.TurnstileToken {
		return ErrInvalid
	}
	if input.SessionToken == "" && (input.RefreshToken != "" || input.ExpiresIn != 0) {
		return ErrInvalid
	}
	if input.RefreshToken != "" && input.ExpiresIn == 0 {
		return ErrInvalid
	}
	return nil
}

func sessionTiming(session *Session, expiresIn int64, now time.Time) error {
	if expiresIn < 0 || expiresIn > maxSessionLifetimeSeconds {
		return ErrUnsupported
	}
	issued := now.UTC()
	session.IssuedAt = &issued
	if expiresIn > 0 {
		expires := issued.Add(time.Duration(expiresIn) * time.Second)
		session.ExpiresAt = &expires
	}
	return nil
}

type sessionRefresher interface {
	Refresh(context.Context, Site, Session) (Session, error)
}

type sessionVerifier interface {
	VerifySession(context.Context, Site, Session) (Session, error)
}

func (c *platformConnector) VerifySession(ctx context.Context, site Site, session Session) (Session, error) {
	return c.identity(ctx, site, session)
}

// The caller must durably save this new pair before making another request.
func (c *platformConnector) Refresh(ctx context.Context, site Site, session Session) (Session, error) {
	if site.Platform != "sub2api" {
		return Session{}, ErrUnsupported
	}
	if session.RefreshToken == "" || !validHeaderValue(session.RefreshToken, maxSessionTokenLength) || !validHeaderValue(session.UserAgent, 1024) {
		return Session{}, ErrReauth
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	// The refresh endpoint authenticates the refresh token, not a possibly expired bearer.
	auth := Session{UserAgent: session.UserAgent, Cookies: session.Cookies}
	if err := c.data(ctx, site, auth, "POST", "/api/v1/auth/refresh", map[string]string{"refresh_token": session.RefreshToken}, &result); err != nil {
		return Session{}, err
	}
	if result.AccessToken == "" || result.RefreshToken == "" || result.ExpiresIn <= 0 || !validHeaderValue(result.AccessToken, maxSessionTokenLength) || !validHeaderValue(result.RefreshToken, maxSessionTokenLength) {
		return Session{}, ErrUnsupported
	}
	session.AccessToken, session.RefreshToken = result.AccessToken, result.RefreshToken
	if err := sessionTiming(&session, result.ExpiresIn, time.Now()); err != nil {
		return Session{}, err
	}
	return session, nil
}
