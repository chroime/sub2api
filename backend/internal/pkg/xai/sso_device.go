package xai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	SSOBuildScope        = "openid profile email offline_access grok-cli:access api:access conversations:read conversations:write"
	SSOAccountsURL       = "https://accounts.x.ai/"
	SSODeviceURL         = OAuthIssuer + "/oauth2/device/code"
	SSOVerifyURL         = OAuthIssuer + "/oauth2/device/verify"
	SSOApproveURL        = OAuthIssuer + "/oauth2/device/approve"
	SSOTokenURL          = OAuthIssuer + "/oauth2/token"
	SSOConversionTimeout = 180 * time.Second
	// SSOBrowserApprovalTimeout bounds the browser fallback: Chromium launch,
	// the device page's Castle/Cloudflare checks, the consent click, and the
	// concurrent token polling. It must fit inside SSOConversionTimeout after
	// the HTTP steps that precede the consent POST.
	SSOBrowserApprovalTimeout = 140 * time.Second

	ssoMaxAuthBody     = 2 << 20
	ssoMaxTokenLength  = 16 << 10
	ssoDefaultUA       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	ssoDefaultTokenTTL = 6 * time.Hour
)

var (
	ErrSSOUnauthorized        = errors.New("xai sso unauthorized")
	ErrSSOAuthorizationDenied = errors.New("xai device authorization denied")
	ErrSSOBrowserRequired     = errors.New("xai device approval requires browser verification")
	ErrSSOBrowserUnavailable  = errors.New("xai sso browser verifier unavailable")
	ErrSSOBrowserApproval     = errors.New("xai sso browser approval failed")
)

type SSOHTTPError struct{ Status int }

func (e SSOHTTPError) Error() string { return fmt.Sprintf("xAI OAuth HTTP %d", e.Status) }

type SSODeviceHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// SSOBrowserCookie is a cookie captured during the HTTP device flow. URL is
// kept with each cookie so the browser helper can inject it only on the
// trusted xAI origin that issued it.
type SSOBrowserCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	URL   string `json:"url"`
}

type SSODeviceOptions struct {
	HTTPClient SSODeviceHTTPClient
	UserAgent  string
	Sleep      func(context.Context, time.Duration) error
	// BrowserApprove is called only when xAI rejects the plain HTTP consent
	// POST with a browser verification challenge. The callback owns an
	// isolated browser session, injects the ephemeral device-flow cookies, and
	// returns only after the device approval page has completed.
	BrowserApprove func(context.Context, SSOBrowserApproval) error
}

// SSOBrowserApproval contains the minimum device-flow data required by the
// browser verifier. SSOToken and Cookies are ephemeral and must never be
// persisted or logged; they cross only the private backend-to-helper boundary.
// The browser resumes at the consent URL with the cookies captured by the
// HTTP device flow so Castle can validate the same authorization session.
type SSOBrowserApproval struct {
	SSOToken        string
	UserCode        string
	VerificationURL string
	ConsentURL      string
	Cookies         []SSOBrowserCookie
}

type ssoDeviceFlow struct {
	client              SSODeviceHTTPClient
	userAgent           string
	cookieJar           http.CookieJar
	sleep               func(context.Context, time.Duration) error
	ssoToken            string
	browserApprove      func(context.Context, SSOBrowserApproval) error
	lastResponseHeaders http.Header
}

func ConvertSSOToBuild(ctx context.Context, ssoToken string, opts *SSODeviceOptions) (*TokenResponse, error) {
	ssoToken = NormalizeSSOToken(ssoToken)
	if ssoToken == "" {
		return nil, ErrSSOUnauthorized
	}
	if opts == nil {
		opts = &SSODeviceOptions{}
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: SSOConversionTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = ssoDefaultUA
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	seedSSOCookies(jar, ssoToken)

	flow := &ssoDeviceFlow{
		client:         client,
		userAgent:      userAgent,
		cookieJar:      jar,
		sleep:          sleep,
		ssoToken:       ssoToken,
		browserApprove: opts.BrowserApprove,
	}
	return flow.convert(ctx)
}

func (f *ssoDeviceFlow) convert(ctx context.Context) (*TokenResponse, error) {
	status, finalURL, _, err := f.do(ctx, http.MethodGet, SSOAccountsURL, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || strings.Contains(finalURL, "sign-in") || strings.Contains(finalURL, "sign-up") {
		return nil, ErrSSOUnauthorized
	}
	if status < 200 || status >= 400 {
		return nil, fmt.Errorf("validate Grok Web SSO: %w", SSOHTTPError{Status: status})
	}

	status, _, body, err := f.do(ctx, http.MethodPost, SSODeviceURL, url.Values{
		"client_id": {DefaultClientID},
		"scope":     {SSOBuildScope},
	})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("start xAI device flow: %w", SSOHTTPError{Status: status})
	}
	var device struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		ExpiresIn               int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &device); err != nil {
		return nil, fmt.Errorf("parse xAI device flow response: %w", err)
	}
	if device.DeviceCode == "" || device.UserCode == "" || !safeXAIAuthURL(device.VerificationURIComplete) {
		return nil, errors.New("xAI device flow response is incomplete")
	}
	if device.Interval <= 0 {
		device.Interval = 5
	}
	if device.ExpiresIn <= 0 {
		device.ExpiresIn = 1800
	}

	status, verificationURL, _, err := f.do(ctx, http.MethodGet, device.VerificationURIComplete, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 400 {
		return nil, fmt.Errorf("open xAI device verification page: %w", SSOHTTPError{Status: status})
	}

	verification, err := url.Parse(verificationURL)
	if err != nil {
		return nil, errors.New("xAI device verification URL is invalid")
	}
	status, finalURL, body, err = f.do(ctx, http.MethodPost, SSOVerifyURL, url.Values{"user_code": {device.UserCode}}, http.Header{
		"Origin":  {verification.Scheme + "://" + verification.Host},
		"Referer": {verificationURL},
	})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 400 {
		return nil, fmt.Errorf("verify xAI device code: %w", SSOHTTPError{Status: status})
	}
	if !strings.Contains(finalURL, "consent") {
		return nil, errors.New("xAI device verification did not reach consent page")
	}

	approval, err := parseSSOConsentForm(body, device.UserCode)
	if err != nil {
		return nil, err
	}
	consentPageURL := finalURL
	consentURL, _ := url.Parse(consentPageURL) // f.do only returns validated xAI URLs.
	browserApproval := func() (*TokenResponse, error) {
		if f.browserApprove == nil {
			return nil, fmt.Errorf("approve xAI device code: %w", ErrSSOBrowserRequired)
		}
		return f.approveInBrowser(ctx, SSOBrowserApproval{
			SSOToken:        f.ssoToken,
			UserCode:        device.UserCode,
			VerificationURL: device.VerificationURIComplete,
			ConsentURL:      consentPageURL,
			Cookies:         f.browserCookies(),
		}, device.DeviceCode, time.Duration(device.Interval)*time.Second, time.Duration(device.ExpiresIn)*time.Second)
	}
	if _, hasCastleToken := approval["castle_request_token"]; hasCastleToken && approval.Get("castle_request_token") == "" {
		return browserApproval()
	}
	status, finalURL, approvalBody, err := f.do(ctx, http.MethodPost, SSOApproveURL, approval, http.Header{
		"Origin":  {consentURL.Scheme + "://" + consentURL.Host},
		"Referer": {consentPageURL},
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden && ssoApprovalNeedsBrowser(approvalBody, f.lastResponseHeaders) {
		return browserApproval()
	}
	if status < 200 || status >= 400 {
		return nil, fmt.Errorf("approve xAI device code: %w", SSOHTTPError{Status: status})
	}
	if !strings.Contains(finalURL, "done") {
		return nil, errors.New("xAI device approval did not reach done page")
	}

	return f.pollToken(ctx, device.DeviceCode, time.Duration(device.Interval)*time.Second, time.Duration(device.ExpiresIn)*time.Second)
}

// xAI's web authorization page now enables Castle. A plain HTTP client can
// complete the device flow up to the consent page, but the final approval is
// rejected with 403 when the browser-minted request token is missing. Keep
// this classification separate from an invalid SSO cookie so callers can
// offer a browser authorization path instead of retrying the same request.
func ssoApprovalNeedsBrowser(body []byte, headers http.Header) bool {
	if strings.TrimSpace(headers.Get("cf-mitigated")) != "" ||
		(strings.EqualFold(headers.Get("server"), "cloudflare") && strings.TrimSpace(headers.Get("cf-ray")) != "") {
		return true
	}
	lower := strings.ToLower(string(body))
	for _, marker := range []string{
		"castle",
		"request token required",
		"couldn't verify this request",
		"could not verify this request",
		"cf-mitigated",
		"cf-chl-",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// parseSSOConsentForm preserves the authorization server's CSRF token and
// principal selection. Never replace a missing token with a fabricated value
// or include response HTML/token values in errors: the page contains
// credentials.
func parseSSOConsentForm(body []byte, userCode string) (url.Values, error) {
	form := url.Values{}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		typeOfToken := tokenizer.Next()
		if typeOfToken == html.ErrorToken {
			if err := tokenizer.Err(); err != nil && err != io.EOF {
				return nil, errors.New("cannot parse xAI device consent form")
			}
			break
		}
		if typeOfToken != html.StartTagToken && typeOfToken != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if !strings.EqualFold(token.Data, "input") {
			continue
		}
		attributes := make(map[string]string, len(token.Attr))
		for _, attr := range token.Attr {
			attributes[strings.ToLower(attr.Key)] = attr.Val
		}
		if !strings.EqualFold(attributes["type"], "hidden") {
			continue
		}
		name := attributes["name"]
		switch name {
		case "user_code", "consent_token", "principal_type", "principal_id", "castle_request_token":
			if _, exists := form[name]; exists {
				return nil, errors.New("xAI device consent form contains duplicate authorization fields")
			}
			form.Set(name, attributes["value"])
		}
	}
	if userCode == "" || form.Get("user_code") != userCode {
		return nil, errors.New("xAI device consent form does not match the requested user code")
	}
	if strings.TrimSpace(form.Get("consent_token")) == "" {
		return nil, errors.New("xAI device consent form is missing consent_token")
	}
	form.Set("action", "allow")
	return form, nil
}

// xAI binds device approval to a hidden token on the consent page. Keep this
// compatibility helper for callers/tests that only need to extract that
// single field from legacy consent markup.
func ssoConsentToken(body []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "input" {
				continue
			}
			var name, value, inputType string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "name":
					name = attr.Val
				case "value":
					value = attr.Val
				case "type":
					inputType = attr.Val
				}
			}
			if name == "consent_token" && strings.EqualFold(inputType, "hidden") {
				return value
			}
		}
	}
}

// approveInBrowser runs the browser verifier and token polling concurrently.
// The token endpoint is the only authoritative approval signal: page text and
// URLs are heuristics, so the browser is kept alive until a token arrives, the
// browser reports a terminal failure, or the approval budget is exhausted.
func (f *ssoDeviceFlow) approveInBrowser(ctx context.Context, approval SSOBrowserApproval, deviceCode string, interval, expiresIn time.Duration) (*TokenResponse, error) {
	browserCtx, cancelBrowser := context.WithCancel(ctx)
	browserDone := make(chan error, 1)
	go func() { browserDone <- f.browserApprove(browserCtx, approval) }()
	token, err := f.pollTokenWithBrowser(ctx, deviceCode, interval, minDuration(expiresIn, SSOBrowserApprovalTimeout), browserDone)
	cancelBrowser()
	if token != nil {
		return token, nil
	}
	return nil, err
}

func (f *ssoDeviceFlow) pollToken(ctx context.Context, deviceCode string, interval, expiresIn time.Duration) (*TokenResponse, error) {
	return f.pollTokenWithBrowser(ctx, deviceCode, interval, minDuration(expiresIn, 75*time.Second), nil)
}

func (f *ssoDeviceFlow) pollTokenWithBrowser(ctx context.Context, deviceCode string, interval, budget time.Duration, browserDone <-chan error) (*TokenResponse, error) {
	if interval < time.Second {
		interval = time.Second
	}
	var browserErr error
	browserFailed := false
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if err := f.sleep(ctx, interval); err != nil {
			return nil, err
		}
		if browserDone != nil {
			select {
			case browserErr = <-browserDone:
				// A browser failure still gets one final poll: the approval
				// may have landed just before the page reported an error.
				browserDone, browserFailed = nil, browserErr != nil
			default:
			}
		}
		status, _, body, err := f.do(ctx, http.MethodPost, SSOTokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {DefaultClientID},
			"device_code": {deviceCode},
		})
		if err != nil {
			return nil, err
		}
		var payload struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			IDToken          string `json:"id_token"`
			TokenType        string `json:"token_type"`
			ExpiresIn        int64  `json:"expires_in"`
			Scope            string `json:"scope"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("parse xAI token response: %w", err)
		}
		if status >= 200 && status < 300 && payload.AccessToken != "" {
			if payload.ExpiresIn <= 0 {
				payload.ExpiresIn = int64(ssoDefaultTokenTTL.Seconds())
			}
			if payload.TokenType == "" {
				payload.TokenType = "Bearer"
			}
			return &TokenResponse{
				AccessToken:  payload.AccessToken,
				RefreshToken: payload.RefreshToken,
				IDToken:      payload.IDToken,
				TokenType:    payload.TokenType,
				ExpiresIn:    payload.ExpiresIn,
				Scope:        payload.Scope,
			}, nil
		}
		switch payload.Error {
		case "authorization_pending", "slow_down":
			if payload.Error == "slow_down" {
				interval += 5 * time.Second
			}
			if browserFailed {
				return nil, fmt.Errorf("approve xAI device code in browser: %w", browserErr)
			}
			continue
		case "access_denied", "expired_token":
			return nil, ErrSSOAuthorizationDenied
		default:
			if status >= 400 {
				return nil, fmt.Errorf("xAI token polling failed (%s): %w", firstNonEmpty(payload.ErrorDescription, payload.Error), SSOHTTPError{Status: status})
			}
			return nil, fmt.Errorf("xAI token polling failed: %s", firstNonEmpty(payload.ErrorDescription, payload.Error, strconv.Itoa(status)))
		}
	}
	if browserErr != nil {
		return nil, fmt.Errorf("approve xAI device code in browser: %w", browserErr)
	}
	if browserDone != nil {
		return nil, fmt.Errorf("approve xAI device code in browser: %w", ErrSSOBrowserApproval)
	}
	return nil, errors.New("xAI device flow token polling timed out")
}

func (f *ssoDeviceFlow) do(ctx context.Context, method, endpoint string, form url.Values, headers ...http.Header) (int, string, []byte, error) {
	if !safeXAIAuthURL(endpoint) {
		return 0, "", nil, errors.New("xAI OAuth URL is not trusted")
	}
	currentURL := endpoint
	currentMethod := method
	currentForm := form
	for redirects := 0; redirects <= 8; redirects++ {
		var body io.Reader
		if currentForm != nil {
			body = strings.NewReader(currentForm.Encode())
		}
		request, err := http.NewRequestWithContext(ctx, currentMethod, currentURL, body)
		if err != nil {
			return 0, currentURL, nil, err
		}
		request.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
		request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		request.Header.Set("User-Agent", f.userAgent)
		if isSSODevicePageRequest(request.URL) {
			request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
			request.Header.Set("Cache-Control", "no-cache")
			request.Header.Set("Pragma", "no-cache")
			request.Header.Set("Upgrade-Insecure-Requests", "1")
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			request.Header.Set("Sec-Fetch-Mode", "navigate")
			request.Header.Set("Sec-Fetch-User", "?1")
			request.Header.Set("Sec-Fetch-Dest", "document")
			request.Header.Set("Sec-CH-UA", `"Not_A Brand";v="99", "Chromium";v="120", "Google Chrome";v="120"`)
			request.Header.Set("Sec-CH-UA-Mobile", "?0")
			request.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
		}
		if redirects == 0 {
			for _, header := range headers {
				for name, values := range header {
					for _, value := range values {
						request.Header.Add(name, value)
					}
				}
			}
		}
		if cookie := f.cookieHeader(request.URL); cookie != "" {
			request.Header.Set("Cookie", cookie)
		}
		if currentForm != nil {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}

		response, err := f.client.Do(request)
		if err != nil {
			return 0, currentURL, nil, err
		}
		f.lastResponseHeaders = response.Header.Clone()
		f.captureCookies(request.URL, response)
		data, readErr := io.ReadAll(io.LimitReader(response.Body, ssoMaxAuthBody+1))
		_ = response.Body.Close()
		if readErr != nil {
			return response.StatusCode, currentURL, nil, readErr
		}
		if len(data) > ssoMaxAuthBody {
			return response.StatusCode, currentURL, nil, errors.New("xAI OAuth response exceeds 2 MiB")
		}
		if response.StatusCode < 300 || response.StatusCode > 399 {
			return response.StatusCode, currentURL, data, nil
		}

		location := strings.TrimSpace(response.Header.Get("Location"))
		if location == "" {
			return response.StatusCode, currentURL, data, errors.New("xAI OAuth redirect missing Location")
		}
		base, _ := url.Parse(currentURL)
		next, err := url.Parse(location)
		if err != nil {
			return response.StatusCode, currentURL, data, err
		}
		currentURL = base.ResolveReference(next).String()
		if !safeXAIAuthURL(currentURL) {
			return response.StatusCode, currentURL, data, errors.New("xAI OAuth redirected to untrusted host")
		}
		if response.StatusCode == http.StatusSeeOther || ((response.StatusCode == http.StatusMovedPermanently || response.StatusCode == http.StatusFound) && currentMethod != http.MethodGet && currentMethod != http.MethodHead) {
			currentMethod = http.MethodGet
			currentForm = nil
		}
	}
	return 0, currentURL, nil, errors.New("xAI OAuth redirected too many times")
}

func isSSODevicePageRequest(target *url.URL) bool {
	if target == nil {
		return false
	}
	host := strings.ToLower(target.Hostname())
	if host != "auth.x.ai" && host != "accounts.x.ai" {
		return false
	}
	path := strings.TrimRight(target.Path, "/")
	return strings.HasPrefix(path, "/oauth2/device/") &&
		path != "/oauth2/device/code" && path != "/oauth2/token"
}

func seedSSOCookies(jar http.CookieJar, token string) {
	if jar == nil {
		return
	}
	for _, rawURL := range []string{SSOAccountsURL, OAuthIssuer + "/"} {
		target, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		jar.SetCookies(target, []*http.Cookie{
			{Name: "sso", Value: token, Path: "/", Secure: true, HttpOnly: true},
			{Name: "sso-rw", Value: token, Path: "/", Secure: true, HttpOnly: true},
		})
	}
}

func (f *ssoDeviceFlow) browserCookies() []SSOBrowserCookie {
	if f == nil || f.cookieJar == nil {
		return nil
	}
	origins := []string{SSOAccountsURL, OAuthIssuer + "/"}
	result := make([]SSOBrowserCookie, 0, 8)
	for _, rawURL := range origins {
		target, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		for _, cookie := range f.cookieJar.Cookies(target) {
			name := strings.TrimSpace(cookie.Name)
			value := strings.TrimSpace(cookie.Value)
			if name == "" || len(name) > 128 || len(value) > ssoMaxTokenLength || strings.ContainsAny(name+value, "\r\n\x00") {
				continue
			}
			result = append(result, SSOBrowserCookie{Name: name, Value: value, URL: rawURL})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].URL != result[j].URL {
			return result[i].URL < result[j].URL
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Value < result[j].Value
	})
	return result
}

func (f *ssoDeviceFlow) captureCookies(requestURL *url.URL, response *http.Response) {
	if f == nil || f.cookieJar == nil || requestURL == nil || response == nil {
		return
	}
	cookies := make([]*http.Cookie, 0)
	for _, cookie := range response.Cookies() {
		name := strings.TrimSpace(cookie.Name)
		value := strings.TrimSpace(cookie.Value)
		if name == "" || len(name) > 128 || len(value) > 16384 || strings.ContainsAny(name+value, "\r\n\x00") {
			continue
		}
		cookie.Name = name
		cookie.Value = value
		cookies = append(cookies, cookie)
	}
	f.cookieJar.SetCookies(requestURL, cookies)
}

func (f *ssoDeviceFlow) cookieHeader(requestURL *url.URL) string {
	if f == nil || f.cookieJar == nil || requestURL == nil {
		return ""
	}
	cookies := f.cookieJar.Cookies(requestURL)
	sort.Slice(cookies, func(i, j int) bool { return cookies[i].Name < cookies[j].Name })
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func safeXAIAuthURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	if AllowUnsafeURLOverrides() {
		return parsed.Scheme != "" && parsed.Host != ""
	}
	if parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "x.ai" || strings.HasSuffix(host, ".x.ai")
}

func NormalizeSSOToken(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "cookie:") {
		value = strings.TrimSpace(value[len("cookie:"):])
	}
	for _, part := range strings.Split(value, ";") {
		name, token, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "sso", "sso-rw":
			return sanitizeSSOToken(token)
		}
	}
	if token, _, found := strings.Cut(value, ";"); found {
		value = strings.TrimSpace(token)
	}
	return sanitizeSSOToken(value)
}

func sanitizeSSOToken(value string) string {
	value = strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(strings.TrimSpace(value))
	if len(value) > ssoMaxTokenLength {
		return ""
	}
	return value
}

func DecodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func JWTClaimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
