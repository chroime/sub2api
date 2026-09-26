package upstreamgovernance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const connectorMaxBody = 2 << 20
const connectorMaxGroups = 100
const connectorMaxModels = 2000

var errConnectorUnavailable = errors.Join(ErrUnsupported, errors.New("upstream endpoint unavailable"))
var errConnectorRemote = errors.New("upstream request failed")
var errConnectorEnvelopeDenied = errors.New("upstream operation rejected")
var errConnectorDenied = errors.New("upstream capability denied")
var errConnectorCaptcha = errors.New("upstream captcha required")
var errConnectorUncertain = errors.New("upstream key creation outcome uncertain; reconcile on next explicit apply")

type platformConnector struct{ factory ClientFactory }

func NewConnector(factory ClientFactory) Connector { return &platformConnector{factory: factory} }

type connectorEnvelope struct {
	Code    *int            `json:"code"`
	Success *bool           `json:"success"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}
type connectorResponse struct {
	connectorEnvelope
	Header http.Header
}

// The factory must enforce the public-host and explicit-proxy policy. A concrete
// http.Client is copied to disable redirects even if its caller forgot to do so.
func (c *platformConnector) request(ctx context.Context, site Site, session Session, method, path string, body any, headers http.Header, envelope bool) (connectorResponse, []byte, error) {
	var out connectorResponse
	base, e := url.Parse(site.BaseURL)
	if e != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return out, nil, ErrInvalid
	}
	if site.Platform != "sub2api" && site.Platform != "newapi" {
		return out, nil, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var payload []byte
	if body != nil {
		payload, e = json.Marshal(body)
		if e != nil {
			return out, nil, ErrInvalid
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(site.BaseURL, "/")+path, bytes.NewReader(payload))
	if e != nil {
		return out, nil, ErrInvalid
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if session.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	}
	if session.AuthVariant == "newapi_legacy_cookie" || session.AuthVariant == "newapi_legacy_bearer" {
		if session.UserID <= 0 {
			return out, nil, ErrReauth
		}
		req.Header.Set("New-Api-User", strconv.FormatInt(session.UserID, 10))
	}
	for name, value := range session.Cookies {
		if name == "session" {
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
	}
	for key, values := range headers {
		req.Header[key] = values
	}
	if c.factory == nil {
		return out, nil, ErrInvalid
	}
	client, e := c.factory(ctx, site)
	if e != nil || client == nil {
		return out, nil, errConnectorRemote
	}
	if concrete, ok := client.(*http.Client); ok {
		copy := *concrete
		copy.Jar = nil // Session cookies are scoped explicitly; never inherit a shared jar.
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	resp, e := client.Do(req)
	if e != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return out, nil, errConnectorRemote
	}
	if resp == nil || resp.Body == nil {
		return out, nil, errConnectorRemote
	}
	defer resp.Body.Close()
	out.Header = resp.Header
	if site.Platform == "sub2api" && (resp.StatusCode == 400 || resp.StatusCode == 422) {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, connectorMaxBody+1))
		var failure struct {
			Reason string `json:"reason"`
		}
		if readErr == nil && len(raw) <= connectorMaxBody && json.Unmarshal(raw, &failure) == nil {
			switch failure.Reason {
			case "TURNSTILE_VERIFICATION_FAILED", "TENCENT_CAPTCHA_VERIFICATION_FAILED", "ALIYUN_CAPTCHA_VERIFICATION_FAILED":
				return out, nil, errConnectorCaptcha
			}
		}
		return out, nil, errConnectorRemote
	}
	if resp.StatusCode == 401 {
		return out, nil, ErrReauth
	}
	if resp.StatusCode == 403 {
		return out, nil, errConnectorDenied
	}
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return out, nil, errConnectorUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, nil, errConnectorRemote
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, connectorMaxBody+1))
	if e != nil || len(raw) > connectorMaxBody {
		return out, nil, ErrUnsupported
	}
	if !json.Valid(raw) {
		return out, nil, ErrUnsupported
	}
	if envelope {
		if json.Unmarshal(raw, &out.connectorEnvelope) != nil {
			return out, nil, ErrUnsupported
		}
		if site.Platform == "sub2api" {
			if out.Code == nil {
				return out, nil, ErrUnsupported
			}
			if *out.Code != 0 {
				return out, nil, errConnectorRemote
			}
		} else {
			if out.Success == nil {
				return out, nil, ErrUnsupported
			}
			if !*out.Success {
				return out, nil, errConnectorEnvelopeDenied
			}
		}
	}
	return out, raw, nil
}
func (c *platformConnector) data(ctx context.Context, s Site, session Session, method, path string, body any, dst any) error {
	r, _, e := c.request(ctx, s, session, method, path, body, nil, true)
	if e != nil {
		return e
	}
	if dst != nil && (len(r.Data) == 0 || string(r.Data) == "null" || json.Unmarshal(r.Data, dst) != nil) {
		return ErrUnsupported
	}
	return nil
}
func (c *platformConnector) identity(ctx context.Context, s Site, session Session) (Session, error) {
	path := "/api/v1/user/profile"
	if s.Platform == "newapi" {
		path = "/api/user/self"
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if e := c.data(ctx, s, session, "GET", path, nil, &user); e != nil {
		// Legacy New API middleware signals invalid management credentials with
		// HTTP 200 / success:false. Classify that only while verifying identity;
		// the same envelope on token creation is an operation failure, not reauth.
		if errors.Is(e, errConnectorEnvelopeDenied) {
			return Session{}, ErrReauth
		}
		return Session{}, e
	}
	if user.ID <= 0 || session.UserID > 0 && session.UserID != user.ID {
		return Session{}, ErrReauth
	}
	session.UserID = user.ID
	return session, nil
}
func (c *platformConnector) Login(ctx context.Context, s Site, input LoginInput) (Session, *Challenge, error) {
	session := Session{AccessToken: input.SessionToken}
	if input.SessionToken != "" {
		session.AuthVariant = "bearer"
		// A supplied ID explicitly selects the pinned legacy management-PAT profile.
		if s.Platform == "newapi" && input.UserID > 0 {
			session.AuthVariant = "newapi_legacy_bearer"
			session.UserID = input.UserID
		}
		verified, e := c.identity(ctx, s, session)
		return verified, nil, e
	}
	var result connectorResponse
	var e error
	if s.Platform == "sub2api" {
		path := "/api/v1/auth/login"
		body := map[string]any{"email": input.Username, "password": input.Password, "turnstile_token": input.CaptchaToken}
		if input.ChallengeToken != "" {
			if input.OTP == "" {
				return Session{}, &Challenge{Kind: "totp", Token: input.ChallengeToken}, nil
			}
			path += "/2fa"
			body = map[string]any{"temp_token": input.ChallengeToken, "totp_code": input.OTP}
		}
		result, _, e = c.request(ctx, s, Session{}, "POST", path, body, nil, true)
	} else if s.Platform == "newapi" {
		path := "/api/user/login"
		body := map[string]any{"username": input.Username, "password": input.Password}
		if input.ChallengeToken != "" {
			if input.OTP == "" {
				return Session{}, &Challenge{Kind: "totp", Token: input.ChallengeToken}, nil
			}
			path += "/2fa"
			body = map[string]any{"flow_token": input.ChallengeToken, "method": "2fa", "code": input.OTP}
		} else {
			var status map[string]json.RawMessage
			if e = c.data(ctx, s, Session{}, "GET", "/api/status", nil, &status); e != nil {
				return Session{}, nil, e
			}
			var captcha bool
			_ = json.Unmarshal(status["turnstile_check"], &captcha)
			if captcha && input.CaptchaToken == "" {
				return Session{}, &Challenge{Kind: "captcha"}, nil
			}
			var enc struct {
				Enabled   *bool  `json:"enabled"`
				KeyID     string `json:"kid"`
				PublicKey string `json:"public_key"`
			}
			e = c.data(ctx, s, Session{}, "GET", "/api/user/login/encryption-key", nil, &enc)
			if e != nil && !errors.Is(e, errConnectorUnavailable) {
				return Session{}, nil, e
			}
			if e == nil && enc.Enabled == nil {
				return Session{}, nil, ErrUnsupported
			}
			if e == nil && *enc.Enabled {
				encrypted, encryptionErr := connectorEncryptPassword(input.Password, enc.PublicKey, enc.KeyID)
				if encryptionErr != nil {
					return Session{}, nil, encryptionErr
				}
				body = map[string]any{"username": input.Username, "password_encrypted": encrypted, "encryption_key_id": enc.KeyID}
			}
			if input.CaptchaToken != "" {
				path += "?turnstile=" + url.QueryEscape(input.CaptchaToken)
			}
		}
		result, _, e = c.request(ctx, s, Session{}, "POST", path, body, nil, true)
	} else {
		return Session{}, nil, ErrUnsupported
	}
	if e != nil {
		if errors.Is(e, errConnectorCaptcha) {
			return Session{}, &Challenge{Kind: "captcha"}, nil
		}
		return Session{}, nil, e
	}
	var login struct {
		AccessToken string `json:"access_token"`
		ID          int64  `json:"id"`
		User        struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Requires2FA         bool   `json:"requires_2fa"`
		Require2FA          bool   `json:"require_2fa"`
		TempToken           string `json:"temp_token"`
		RequireVerification bool   `json:"require_verification"`
		FlowToken           string `json:"flow_token"`
		Methods             []struct {
			Method    string `json:"method"`
			Available bool   `json:"available"`
		} `json:"methods"`
	}
	if json.Unmarshal(result.Data, &login) != nil {
		return Session{}, nil, ErrUnsupported
	}
	if login.Requires2FA && login.TempToken != "" {
		return Session{}, &Challenge{Kind: "totp", Token: login.TempToken}, nil
	}
	if login.Require2FA {
		return Session{}, &Challenge{Kind: "interactive_legacy_totp"}, ErrUnsupported
	}
	if login.RequireVerification {
		for _, m := range login.Methods {
			if m.Method == "2fa" && m.Available && login.FlowToken != "" {
				return Session{}, &Challenge{Kind: "totp", Token: login.FlowToken}, nil
			}
		}
		return Session{}, &Challenge{Kind: "interactive_verification"}, ErrUnsupported
	}
	session.AccessToken = login.AccessToken
	session.AuthVariant = "bearer"
	session.UserID = login.User.ID
	if session.AccessToken == "" && s.Platform == "newapi" && login.ID > 0 {
		session.AuthVariant = "newapi_legacy_cookie"
		session.UserID = login.ID
		session.Cookies = map[string]string{}
		response := http.Response{Header: result.Header}
		for _, cookie := range response.Cookies() {
			if cookie.Name == "session" && cookie.Value != "" {
				session.Cookies[cookie.Name] = cookie.Value
			}
		}
		if len(session.Cookies) == 0 {
			return Session{}, nil, ErrUnsupported
		}
	} else if session.AccessToken == "" {
		return Session{}, nil, ErrUnsupported
	}
	verified, e := c.identity(ctx, s, session)
	return verified, nil, e
}
