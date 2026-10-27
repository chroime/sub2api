//go:build unit

package xai

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ssoDeviceFakeClient struct {
	t             *testing.T
	tokenCalls    int
	cookieHeaders []string
	approveStatus int
	approveBody   string
	consentBody   string
	approveCalls  int
	tokenPending  bool
}

func (c *ssoDeviceFakeClient) Do(req *http.Request) (*http.Response, error) {
	c.cookieHeaders = append(c.cookieHeaders, req.Header.Get("Cookie"))
	if req.URL.String() != SSOApproveURL && req.URL.String() != SSOVerifyURL {
		require.Empty(c.t, req.Header.Get("Origin"))
		require.Empty(c.t, req.Header.Get("Referer"))
	}
	switch req.URL.String() {
	case SSOAccountsURL:
		require.Equal(c.t, http.MethodGet, req.Method)
		return ssoDeviceResponse(http.StatusOK, http.Header{"Set-Cookie": {"session=web-session; Domain=x.ai; Path=/"}}, `{}`), nil
	case SSODeviceURL:
		require.Equal(c.t, http.MethodPost, req.Method)
		values := readSSODeviceForm(c.t, req)
		require.Equal(c.t, DefaultClientID, values.Get("client_id"))
		require.Equal(c.t, SSOBuildScope, values.Get("scope"))
		return ssoDeviceResponse(http.StatusOK, http.Header{"Set-Cookie": {"csrf=csrf-token; Path=/"}}, `{"device_code":"device-1","user_code":"USER-1","verification_uri_complete":"https://auth.x.ai/oauth2/device/complete","interval":1,"expires_in":60}`), nil
	case "https://auth.x.ai/oauth2/device/complete":
		require.Equal(c.t, http.MethodGet, req.Method)
		return ssoDeviceResponse(http.StatusOK, nil, `<html>ok</html>`), nil
	case SSOVerifyURL:
		require.Equal(c.t, http.MethodPost, req.Method)
		require.Equal(c.t, "https://auth.x.ai/oauth2/device/complete", req.Header.Get("Referer"))
		require.Equal(c.t, "https://auth.x.ai", req.Header.Get("Origin"))
		values := readSSODeviceForm(c.t, req)
		require.Equal(c.t, "USER-1", values.Get("user_code"))
		return ssoDeviceResponse(http.StatusFound, http.Header{"Location": {"/oauth2/device/consent"}}, ``), nil
	case "https://auth.x.ai/oauth2/device/consent":
		require.Equal(c.t, http.MethodGet, req.Method)
		body := `<html><form>
<input type="hidden" name="user_code" value="USER-1">
<input type="hidden" name="consent_token" value="consent&amp;token">
<input type="hidden" name="principal_type" value="User">
<input type="hidden" name="principal_id" value="user-123">
<input type="hidden" name="action" value="deny">
</form></html>`
		if c.consentBody != "" {
			body = c.consentBody
		}
		return ssoDeviceResponse(http.StatusOK, nil, body), nil
	case SSOApproveURL:
		c.approveCalls++
		require.Equal(c.t, http.MethodPost, req.Method)
		require.Equal(c.t, "https://auth.x.ai", req.Header.Get("Origin"))
		require.Equal(c.t, "https://auth.x.ai/oauth2/device/consent", req.Header.Get("Referer"))
		require.Equal(c.t, "navigate", req.Header.Get("Sec-Fetch-Mode"))
		require.Equal(c.t, "document", req.Header.Get("Sec-Fetch-Dest"))
		require.Equal(c.t, "?1", req.Header.Get("Sec-Fetch-User"))
		require.Contains(c.t, req.Header.Get("Sec-CH-UA"), `"Google Chrome";v="120"`)
		values := readSSODeviceForm(c.t, req)
		require.Equal(c.t, "USER-1", values.Get("user_code"))
		require.Equal(c.t, "allow", values.Get("action"))
		require.Equal(c.t, "User", values.Get("principal_type"))
		require.Equal(c.t, "user-123", values.Get("principal_id"))
		require.Equal(c.t, "consent&token", values.Get("consent_token"))
		if c.approveStatus != 0 {
			return ssoDeviceResponse(c.approveStatus, nil, c.approveBody), nil
		}
		return ssoDeviceResponse(http.StatusSeeOther, http.Header{"Location": {"/oauth2/device/done"}}, ``), nil
	case "https://auth.x.ai/oauth2/device/done":
		require.Equal(c.t, http.MethodGet, req.Method)
		return ssoDeviceResponse(http.StatusOK, nil, `<html>done</html>`), nil
	case SSOTokenURL:
		require.Equal(c.t, http.MethodPost, req.Method)
		c.tokenCalls++
		values := readSSODeviceForm(c.t, req)
		require.Equal(c.t, "urn:ietf:params:oauth:grant-type:device_code", values.Get("grant_type"))
		require.Equal(c.t, "device-1", values.Get("device_code"))
		if c.tokenPending {
			return ssoDeviceResponse(http.StatusBadRequest, nil, `{"error":"authorization_pending"}`), nil
		}
		return ssoDeviceResponse(http.StatusOK, nil, `{"access_token":"access-token","refresh_token":"refresh-token","id_token":"id-token","token_type":"Bearer","expires_in":3600,"scope":"`+SSOBuildScope+`"}`), nil
	default:
		c.t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		return nil, nil
	}
}

func TestConvertSSOToBuildCompletesDeviceFlow(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{t: t}
	token, err := ConvertSSOToBuild(context.Background(), "sso=sso-token; ignored=1", &SSODeviceOptions{
		HTTPClient: client,
		Sleep: func(context.Context, time.Duration) error {
			return nil
		},
	})

	require.NoError(t, err)
	require.Equal(t, "access-token", token.AccessToken)
	require.Equal(t, "refresh-token", token.RefreshToken)
	require.Equal(t, "id-token", token.IDToken)
	require.Equal(t, SSOBuildScope, token.Scope)
	require.Equal(t, 1, client.tokenCalls)
	require.Contains(t, client.cookieHeaders[0], "sso=sso-token")
	require.Contains(t, client.cookieHeaders[0], "sso-rw=sso-token")
	require.Contains(t, client.cookieHeaders[len(client.cookieHeaders)-1], "session=web-session")
	require.Contains(t, client.cookieHeaders[len(client.cookieHeaders)-1], "csrf=csrf-token")
	for _, cookie := range client.cookieHeaders {
		require.NotContains(t, cookie, "consent&token")
	}
}

func TestConvertSSOToBuildClassifiesCastleApprovalChallenge(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{
		t:             t,
		approveStatus: http.StatusForbidden,
		approveBody:   `<html><title>Castle request token required</title><p>We couldn't verify this request.</p></html>`,
	}
	_, err := ConvertSSOToBuild(context.Background(), "sso=sso-token", &SSODeviceOptions{
		HTTPClient: client,
		Sleep: func(context.Context, time.Duration) error {
			return nil
		},
	})
	require.ErrorIs(t, err, ErrSSOBrowserRequired)
}

func TestConvertSSOToBuildUsesBrowserApprovalThenPollsToken(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{
		t:             t,
		approveStatus: http.StatusForbidden,
		approveBody:   `<html><title>Castle request token required</title></html>`,
	}
	approvals := make(chan SSOBrowserApproval, 1)
	token, err := ConvertSSOToBuild(context.Background(), "sso=sso-token", &SSODeviceOptions{
		HTTPClient: client,
		Sleep:      func(context.Context, time.Duration) error { return nil },
		BrowserApprove: func(_ context.Context, approval SSOBrowserApproval) error {
			approvals <- approval
			return nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, "access-token", token.AccessToken)
	require.Equal(t, "refresh-token", token.RefreshToken)
	// The browser runs concurrently with token polling; wait for it to record
	// its input instead of racing the goroutine.
	var browserApproval SSOBrowserApproval
	select {
	case browserApproval = <-approvals:
	case <-time.After(2 * time.Second):
		t.Fatal("browser approval was never started")
	}
	require.Equal(t, "sso-token", browserApproval.SSOToken)
	require.Equal(t, "USER-1", browserApproval.UserCode)
	require.Equal(t, "https://auth.x.ai/oauth2/device/complete", browserApproval.VerificationURL)
	require.Equal(t, "https://auth.x.ai/oauth2/device/consent", browserApproval.ConsentURL)
	require.Contains(t, browserApproval.Cookies, SSOBrowserCookie{Name: "session", Value: "web-session", URL: SSOAccountsURL})
	require.Contains(t, browserApproval.Cookies, SSOBrowserCookie{Name: "csrf", Value: "csrf-token", URL: OAuthIssuer + "/"})
	require.Contains(t, browserApproval.Cookies, SSOBrowserCookie{Name: "sso", Value: "sso-token", URL: SSOAccountsURL})
}

func TestConvertSSOToBuildUsesBrowserBeforeCastleApproval(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{
		t: t,
		consentBody: `<html><form>
<input type="hidden" name="user_code" value="USER-1">
<input type="hidden" name="consent_token" value="consent&amp;token">
<input type="hidden" name="principal_type" value="User">
<input type="hidden" name="principal_id" value="user-123">
<input type="hidden" name="castle_request_token" value="">
</form></html>`,
	}
	browserStarted := make(chan struct{})
	token, err := ConvertSSOToBuild(context.Background(), "sso=sso-token", &SSODeviceOptions{
		HTTPClient: client,
		Sleep:      func(context.Context, time.Duration) error { return nil },
		BrowserApprove: func(context.Context, SSOBrowserApproval) error {
			close(browserStarted)
			return nil
		},
	})
	require.NoError(t, err)
	select {
	case <-browserStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("browser approval was never started")
	}
	require.Zero(t, client.approveCalls)
	require.Equal(t, "access-token", token.AccessToken)
}

func TestConvertSSOToBuildTokenWinsWhileBrowserStillRunning(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{
		t:             t,
		approveStatus: http.StatusForbidden,
		approveBody:   `<html><title>Castle request token required</title></html>`,
	}
	browserCanceled := make(chan struct{})
	token, err := ConvertSSOToBuild(context.Background(), "sso=sso-token", &SSODeviceOptions{
		HTTPClient: client,
		Sleep:      func(context.Context, time.Duration) error { return nil },
		BrowserApprove: func(ctx context.Context, _ SSOBrowserApproval) error {
			// Page never reports success; the token endpoint is authoritative.
			<-ctx.Done()
			close(browserCanceled)
			return ctx.Err()
		},
	})
	require.NoError(t, err)
	require.Equal(t, "access-token", token.AccessToken)
	select {
	case <-browserCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("browser approval was not canceled after token issuance")
	}
}

func TestConvertSSOToBuildReportsBrowserFailureAfterFinalPoll(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{
		t:             t,
		approveStatus: http.StatusForbidden,
		approveBody:   `<html><title>Castle request token required</title></html>`,
		tokenPending:  true,
	}
	browserReturned := make(chan struct{})
	_, err := ConvertSSOToBuild(context.Background(), "sso=sso-token", &SSODeviceOptions{
		HTTPClient: client,
		Sleep: func(context.Context, time.Duration) error {
			<-browserReturned
			return nil
		},
		BrowserApprove: func(context.Context, SSOBrowserApproval) error {
			defer close(browserReturned)
			return ErrSSOBrowserApproval
		},
	})
	require.ErrorIs(t, err, ErrSSOBrowserApproval)
	require.GreaterOrEqual(t, client.tokenCalls, 1, "a final token poll must run before reporting browser failure")
}

func TestConvertSSOToBuildKeepsGenericApprovalForbidden(t *testing.T) {
	t.Setenv(EnvClientID, "")
	client := &ssoDeviceFakeClient{
		t:             t,
		approveStatus: http.StatusForbidden,
		approveBody:   `<html><title>Forbidden</title></html>`,
	}
	_, err := ConvertSSOToBuild(context.Background(), "sso=sso-token", &SSODeviceOptions{
		HTTPClient: client,
		Sleep: func(context.Context, time.Duration) error {
			return nil
		},
	})
	var statusErr SSOHTTPError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusForbidden, statusErr.Status)
	require.NotErrorIs(t, err, ErrSSOBrowserRequired)
}

func TestSSOConsentToken(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"legacy page", `<html>consent</html>`, ""},
		{"encoded hidden value", `<input value='a&amp;b&#43;c' name='consent_token' type='hidden'>`, "a&b+c"},
		{"case insensitive HTML", `<INPUT TYPE="HIDDEN" NAME="consent_token" VALUE="token"/>`, "token"},
		{"ignore script and visible fields", `<script>"<input type='hidden' name='consent_token' value='fake'>"</script><input name='consent_token' value='visible'><input type='hidden' name='user_code' value='user'><input type='hidden' name='consent_token' value='real'>`, "real"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ssoConsentToken([]byte(tc.body)))
		})
	}
}

func TestParseSSOConsentFormPreservesAuthorizationFields(t *testing.T) {
	form, err := parseSSOConsentForm([]byte(`<FORM>
<INPUT VALUE='USER-1' NAME=user_code TYPE=HIDDEN>
<input value='abc&amp;def&#43;ghi' type=hidden name=consent_token />
<input type=hidden name=principal_type value=User>
<input type=hidden name=principal_id value=selected-user>
<input type=hidden name=action value=deny>
</FORM>`), "USER-1")
	require.NoError(t, err)
	require.Equal(t, url.Values{
		"user_code": {"USER-1"}, "consent_token": {"abc&def+ghi"},
		"principal_type": {"User"}, "principal_id": {"selected-user"}, "action": {"allow"},
	}, form)
}

func TestNormalizeSSOTokenAcceptsCookieHeader(t *testing.T) {
	require.Equal(t, "token-1", NormalizeSSOToken("Cookie: foo=bar; sso=token-1; sso-rw=token-2"))
	require.Equal(t, "token-2", NormalizeSSOToken("sso-rw=token-2; foo=bar"))
	require.Equal(t, "raw-token", NormalizeSSOToken(" raw-token ; ignored=1"))
	require.Empty(t, NormalizeSSOToken(strings.Repeat("x", ssoMaxTokenLength+1)))
}

func TestSSODeviceCookieJarHonorsDomainAndPath(t *testing.T) {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	flow := &ssoDeviceFlow{cookieJar: jar}
	accountsURL, err := url.Parse("https://accounts.x.ai/")
	require.NoError(t, err)
	authURL, err := url.Parse("https://auth.x.ai/oauth2/device/verify")
	require.NoError(t, err)

	flow.captureCookies(accountsURL, ssoDeviceResponse(http.StatusOK, http.Header{"Set-Cookie": {
		"host-only=accounts; Path=/",
		"shared=all-xai; Domain=x.ai; Path=/",
		"narrow=oauth-only; Domain=x.ai; Path=/oauth2",
	}}, ""))

	authCookies := flow.cookieHeader(authURL)
	require.NotContains(t, authCookies, "host-only=accounts")
	require.Contains(t, authCookies, "shared=all-xai")
	require.Contains(t, authCookies, "narrow=oauth-only")

	accountsCookies := flow.cookieHeader(accountsURL)
	require.Contains(t, accountsCookies, "host-only=accounts")
	require.Contains(t, accountsCookies, "shared=all-xai")
	require.NotContains(t, accountsCookies, "narrow=oauth-only")
}

func ssoDeviceResponse(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func readSSODeviceForm(t *testing.T, req *http.Request) url.Values {
	t.Helper()
	data, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	values, err := url.ParseQuery(string(data))
	require.NoError(t, err)
	return values
}
