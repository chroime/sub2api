package upstreamgovernance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type connectorDoer func(*http.Request) (*http.Response, error)

func (f connectorDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureConnector(t *testing.T, handler func(*http.Request) (int, string)) Connector {
	t.Helper()
	return NewConnector(func(context.Context, Site) (HTTPDoer, error) {
		return connectorDoer(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "upstream.example" {
				t.Fatalf("unsafe URL: %v", r.URL)
			}
			if _, ok := r.Context().Deadline(); !ok {
				t.Error("missing deadline")
			}
			status, body := handler(r)
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}), nil
	})
}
func TestConnectorSub2APIRateOverride(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":2}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{"7":0.8}}`
		case "/api/v1/channels/available":
			return 404, `{"message":"secret"}`
		}
		t.Fatalf("unexpected %s", r.URL)
		return 500, ""
	})
	cat, err := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake", UserID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Groups) != 1 || cat.Groups[0].ResolvedRateMultiplier == nil || *cat.Groups[0].ResolvedRateMultiplier != 0.8 {
		t.Fatalf("bad override: %+v", cat)
	}
	if len(cat.Warnings) == 0 {
		t.Fatal("missing unavailable pricing warning")
	}
}
func TestConnectorNewAPIKeyRecovery(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		case "/api/token/":
			if r.Method != "GET" {
				t.Fatal("existing key must not create")
			}
			return 200, `{"success":true,"data":{"page":1,"page_size":100,"total":1,"items":[{"id":8,"name":"governance-stable","group":"vip","key":"sk-***"}]}}`
		case "/api/token/8/key":
			if r.Method != "POST" {
				t.Fatal("key read must POST")
			}
			return 200, `{"success":true,"data":{"key":"sk-invented-full-key"}}`
		}
		t.Fatalf("unexpected %s", r.URL)
		return 500, ""
	})
	k, e := c.EnsureKey(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake", UserID: 42}, RemoteGroup{ID: "vip"}, "governance-stable")
	if e != nil || k.Key != "sk-invented-full-key" {
		t.Fatalf("key=%+v err=%v", k, e)
	}
}
func TestConnectorProbeRequiresText(t *testing.T) {
	for _, body := range []string{`{}`, `{"choices":[]}`, `{"choices":[{"message":{"content":""}}]}`} {
		c := fixtureConnector(t, func(*http.Request) (int, string) { return 200, body })
		result, e := c.Probe(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, RemoteKey{Key: "fake"}, "openai", "model")
		if e == nil && result.Success {
			t.Fatal("malformed probe succeeded")
		}
	}
}
func TestConnectorSanitizesRemoteFailures(t *testing.T) {
	c := fixtureConnector(t, func(*http.Request) (int, string) { return 401, `{"message":"secret-password"}` })
	_, _, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{SessionToken: "fake"})
	if !errors.Is(e, ErrReauth) || strings.Contains(e.Error(), "secret") {
		t.Fatalf("unsafe error: %v", e)
	}
}

func TestConnectorNewAPIDiscoverRatios(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		case "/api/user/self/groups":
			return 200, `{"success":true,"data":{"vip":{"ratio":0.8,"desc":"VIP"},"auto":{"ratio":"自动","desc":"Auto"}}}`
		case "/api/user/models":
			return 200, `{"success":true,"data":["text-model"]}`
		case "/api/pricing":
			return 200, `{"success":true,"data":[{"model_name":"text-model","model_ratio":1,"completion_ratio":3,"quota_type":0,"enable_groups":["vip"]}],"group_ratio":{"vip":0.8}}`
		}
		t.Fatalf("unexpected %s", r.URL)
		return 500, ""
	})
	cat, e := c.Discover(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake", UserID: 42})
	if e != nil {
		t.Fatal(e)
	}
	if len(cat.Groups) != 2 {
		t.Fatal(cat)
	}
	for _, g := range cat.Groups {
		if g.ID == "auto" && g.ResolvedRateMultiplier != nil {
			t.Fatal("auto ratio invented")
		}
		if g.ID == "vip" {
			if len(g.Prices) != 1 || g.Prices[0].Unit != "newapi_ratio" || *g.Prices[0].Input != 1 || *g.Prices[0].Output != 3 {
				t.Fatalf("lost ratio provenance %+v", g)
			}
		}
	}
}
func TestConnectorCreateThenReadNewAPIKey(t *testing.T) {
	created := false
	posts := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		case "/api/token/":
			if r.Method == "POST" {
				posts++
				created = true
				return 200, `{"success":true,"message":""}`
			}
			if created {
				return 200, `{"success":true,"data":{"total":1,"page":1,"items":[{"id":9,"name":"marker","group":"vip","key":"sk-***"}]}}`
			}
			return 200, `{"success":true,"data":{"total":0,"page":1,"items":[]}}`
		case "/api/token/9/key":
			return 200, `{"success":true,"data":{"key":"sk-real-fixture-key"}}`
		}
		t.Fatalf("unexpected %s", r.URL)
		return 500, ""
	})
	key, e := c.EnsureKey(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake", UserID: 42}, RemoteGroup{ID: "vip"}, "marker")
	if e != nil || key.ID != "9" || posts != 1 {
		t.Fatalf("key=%+v e=%v posts=%d", key, e, posts)
	}
}

func TestConnectorMalformedOptionalCatalogFails(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{}}`
		default:
			return 200, `{"code":0,"data":{"wrong":"secret"}}`
		}
	})
	_, e := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"})
	if !errors.Is(e, ErrUnsupported) {
		t.Fatalf("incomplete snapshot accepted: %v", e)
	}
}
func TestConnectorSub2APIPriceUnit(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":2}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{}}`
		default:
			return 200, `{"code":0,"data":[{"name":"channel","platforms":[{"platform":"openai","groups":[{"id":7}],"supported_models":[{"name":"model","platform":"openai","pricing":{"billing_mode":"token","input_price":0.000002,"output_price":0.000004}}]}]}]}`
		}
	})
	cat, e := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	if cat.Groups[0].Prices[0].Unit != "usd_per_token" {
		t.Fatal("wrong pricing unit")
	}
}

func TestConnectorLoginChallengesAndVerification(t *testing.T) {
	for _, tc := range []struct {
		name, platform, path, body, kind string
		input                            LoginInput
	}{
		{"sub_totp", "sub2api", "/api/v1/auth/login", `{"code":0,"data":{"requires_2fa":true,"temp_token":"temporary"}}`, "totp", LoginInput{Username: "fixture@example.test", Password: "invented"}},
		{"new_totp", "newapi", "/api/user/login", `{"success":true,"data":{"require_verification":true,"flow_token":"temporary","methods":[{"method":"2fa","available":true}]}}`, "totp", LoginInput{Username: "fixture", Password: "invented"}},
		{"new_passkey", "newapi", "/api/user/login", `{"success":true,"data":{"require_verification":true,"flow_token":"temporary","methods":[{"method":"passkey","available":true}]}}`, "interactive_verification", LoginInput{Username: "fixture", Password: "invented"}},
		{"sub_complete", "sub2api", "/api/v1/auth/login/2fa", `{"code":0,"data":{"access_token":"dashboard","user":{"id":42}}}`, "", LoginInput{OTP: "123456", ChallengeToken: "temporary"}},
		{"new_complete", "newapi", "/api/user/login/2fa", `{"success":true,"data":{"access_token":"dashboard","user":{"id":42}}}`, "", LoginInput{OTP: "123456", ChallengeToken: "temporary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureConnector(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/api/status":
					return 200, `{"success":true,"data":{"turnstile_check":false}}`
				case "/api/user/login/encryption-key":
					return 200, `{"success":true,"data":{"enabled":false}}`
				case "/api/v1/user/profile":
					return 200, `{"code":0,"data":{"id":42}}`
				case "/api/user/self":
					return 200, `{"success":true,"data":{"id":42}}`
				}
				if r.URL.Path != tc.path {
					t.Fatalf("path %s", r.URL.Path)
				}
				return 200, tc.body
			})
			session, ch, e := c.Login(context.Background(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, tc.input)
			if tc.kind == "" {
				if e != nil || ch != nil || session.UserID != 42 || session.AccessToken != "dashboard" {
					t.Fatalf("session=%+v challenge=%+v err=%v", session, ch, e)
				}
			} else if ch == nil || ch.Kind != tc.kind || session.AccessToken != "" {
				t.Fatalf("challenge=%+v err=%v", ch, e)
			}
		})
	}
}
func TestConnectorNewAPIInvalidPublicKeyNeverSent(t *testing.T) {
	posts := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.Method == "POST" {
			posts++
		}
		switch r.URL.Path {
		case "/api/status":
			return 200, `{"success":true,"data":{}}`
		case "/api/user/login/encryption-key":
			return 200, `{"success":true,"data":{"enabled":true,"public_key":"fixture"}}`
		}
		return 500, `{}`
	})
	_, ch, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: "never-send"})
	if !errors.Is(e, ErrUnsupported) || ch != nil || posts != 0 {
		t.Fatalf("e=%v ch=%+v posts=%d", e, ch, posts)
	}
}
func TestConnectorBodyLimitAndRedirectRejected(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{302, `{"secret":"hidden"}`}, {200, `{"code":0,"data":"` + strings.Repeat("x", connectorMaxBody) + `"}`}} {
		c := fixtureConnector(t, func(*http.Request) (int, string) { return tc.status, tc.body })
		_, _, e := c.Login(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, LoginInput{SessionToken: "fake"})
		if e == nil || strings.Contains(e.Error(), "hidden") {
			t.Fatal("unsafe response accepted")
		}
	}
}
func TestConnectorIncompleteKeyPaginationPreventsCreate(t *testing.T) {
	posts := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.Method == "POST" {
			posts++
		}
		if r.URL.Path == "/api/user/self" {
			return 200, `{"success":true,"data":{"id":42}}`
		}
		return 200, `{"success":true,"data":{"total":1,"items":[]}}`
	})
	_, e := c.EnsureKey(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"}, RemoteGroup{ID: "vip"}, "marker")
	if e == nil || posts != 0 {
		t.Fatalf("incomplete list created key e=%v posts=%d", e, posts)
	}
}
func TestConnectorUncertainCreateNotRetried(t *testing.T) {
	posts := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/api/user/self" {
			return 200, `{"success":true,"data":{"id":42}}`
		}
		if r.Method == "POST" {
			posts++
			return 502, `{"message":"secret"}`
		}
		return 200, `{"success":true,"data":{"total":0,"items":[]}}`
	})
	_, e := c.EnsureKey(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"}, RemoteGroup{ID: "vip"}, "marker")
	if !errors.Is(e, errConnectorUncertain) || posts != 1 || strings.Contains(e.Error(), "secret") {
		t.Fatalf("e=%v posts=%d", e, posts)
	}
}
func TestConnectorProbeTransports(t *testing.T) {
	for _, tc := range []struct{ platform, path, header, body string }{
		{"openai", "/v1/chat/completions", "Authorization", `{"choices":[{"message":{"content":"OK"}}]}`},
		{"anthropic", "/v1/messages", "x-api-key", `{"content":[{"type":"text","text":"OK"}]}`},
		{"gemini", "/v1beta/models/model:generateContent", "x-goog-api-key", `{"candidates":[{"content":{"parts":[{"text":"OK"}]}}]}`},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			c := fixtureConnector(t, func(r *http.Request) (int, string) {
				if r.URL.Path != tc.path || r.Header.Get(tc.header) == "" || r.URL.RawQuery != "" {
					t.Fatalf("wrong probe request %v", r.URL)
				}
				return 200, tc.body
			})
			result, e := c.Probe(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, RemoteKey{Key: "fixture"}, tc.platform, "model")
			if e != nil || !result.Success {
				t.Fatalf("result=%+v e=%v", result, e)
			}
		})
	}
}

func TestConnectorUnknownEncryptionContractDoesNotSendPassword(t *testing.T) {
	posts := 0
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.Method == "POST" {
			posts++
			return 500, `{}`
		}
		return 200, `{"success":true,"data":{}}`
	})
	_, _, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: "never-send"})
	if !errors.Is(e, ErrUnsupported) || posts != 0 {
		t.Fatalf("unknown encryption profile sent password: e=%v posts=%d", e, posts)
	}
}
func TestConnectorSub2APICaptchaSurfaced(t *testing.T) {
	c := fixtureConnector(t, func(*http.Request) (int, string) {
		return 400, `{"code":400,"reason":"TURNSTILE_VERIFICATION_FAILED","message":"secret"}`
	})
	_, ch, e := c.Login(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: "invented"})
	if e != nil || ch == nil || ch.Kind != "captcha" {
		t.Fatalf("captcha not surfaced ch=%+v e=%v", ch, e)
	}
}

func TestConnectorNewAPIPricingMetadataAndPlugin(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		case "/api/user/self/groups":
			return 200, `{"success":true,"data":{"vip":{"ratio":0.8}}}`
		case "/api/user/models":
			return 200, `{"success":true,"data":["standard","plugin"]}`
		default:
			return 200, `{"success":true,"group_ratio":{"vip":0.8},"data":[{"model_name":"standard","quota_type":0,"model_ratio":2,"completion_ratio":3,"cache_ratio":0.1,"enable_groups":["all"],"supported_endpoint_types":["openai"]},{"model_name":"plugin","quota_type":0,"model_ratio":0,"completion_ratio":0,"enable_groups":["vip"],"billing_plugin_variants":[{"billing_expr":"secret-expression"}]}]}`
		}
	})
	cat, e := c.Discover(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	if len(cat.Groups[0].Prices) != 2 {
		t.Fatal("all-group pricing omitted")
	}
	for _, p := range cat.Groups[0].Prices {
		if p.Model == "standard" && (p.Details["cache_ratio"] != 0.1 || p.Details["pricing_group_ratio"] != 0.8) {
			t.Fatalf("lost metadata %+v", p)
		}
		if p.Model == "plugin" && (p.Input != nil || p.Unit != "newapi_unknown") {
			t.Fatal("fabricated zero plugin pricing")
		}
	}
}

type connectorRoundTripper func(*http.Request) (*http.Response, error)

func (f connectorRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConnectorDisablesConcreteClientRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: connectorRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/steal"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	c := NewConnector(func(context.Context, Site) (HTTPDoer, error) { return client, nil })
	_, _, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{SessionToken: "secret"})
	if e == nil || calls != 1 {
		t.Fatalf("redirect followed calls=%d e=%v", calls, e)
	}
}
func TestConnectorLegacyCookieLoginValidatesReturnedIdentity(t *testing.T) {
	c := NewConnector(func(context.Context, Site) (HTTPDoer, error) {
		return connectorDoer(func(r *http.Request) (*http.Response, error) {
			header := make(http.Header)
			body := `{"success":true,"data":{}}`
			status := 200
			switch r.URL.Path {
			case "/api/status":
			case "/api/user/login/encryption-key":
				status = 404
			case "/api/user/login":
				body = `{"success":true,"data":{"id":42,"username":"fixture"}}`
				header.Set("Set-Cookie", "session=invented-cookie; Path=/; HttpOnly")
			case "/api/user/self":
				if r.Header.Get("New-Api-User") != "42" || r.Header.Get("Cookie") != "session=invented-cookie" || r.Header.Get("Authorization") != "" {
					t.Fatalf("wrong legacy auth headers")
				}
				body = `{"success":true,"data":{"id":42}}`
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}), nil
	})
	session, ch, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: "invented", UserID: 999})
	if e != nil || ch != nil || session.UserID != 42 || session.AuthVariant != "newapi_legacy_cookie" {
		t.Fatalf("session=%+v ch=%+v e=%v", session, ch, e)
	}
}

func TestConnectorWrongGroupMarkerAndMaskedKeyRejected(t *testing.T) {
	for _, tc := range []struct {
		group, key string
		want       error
	}{{"other", "sk-full", ErrConflict}, {"vip", "sk-****", ErrUnsupported}} {
		c := fixtureConnector(t, func(r *http.Request) (int, string) {
			switch r.URL.Path {
			case "/api/user/self":
				return 200, `{"success":true,"data":{"id":42}}`
			case "/api/token/":
				return 200, `{"success":true,"data":{"total":1,"items":[{"id":9,"name":"marker","group":"` + tc.group + `"}]}}`
			case "/api/token/9/key":
				return 200, `{"success":true,"data":{"key":"` + tc.key + `"}}`
			}
			t.Fatalf("unexpected %s", r.URL)
			return 500, `{}`
		})
		_, e := c.EnsureKey(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"}, RemoteGroup{ID: "vip"}, "marker")
		if !errors.Is(e, tc.want) {
			t.Fatalf("e=%v want=%v", e, tc.want)
		}
	}
}
func TestConnectorNewAPIIdentityMismatchRejected(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.Header.Get("New-Api-User") != "42" {
			t.Fatal("missing legacy identity header")
		}
		return 200, `{"success":true,"data":{"id":99}}`
	})
	_, _, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{SessionToken: "management-pat", UserID: 42})
	if !errors.Is(e, ErrReauth) {
		t.Fatalf("identity mismatch e=%v", e)
	}
}
func TestConnectorNewAPICaptchaUsesQuery(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/status":
			return 200, `{"success":true,"data":{"turnstile_check":true}}`
		case "/api/user/login/encryption-key":
			return 200, `{"success":true,"data":{"enabled":false}}`
		case "/api/user/login":
			if r.URL.Query().Get("turnstile") != "proof+value" {
				t.Fatal("captcha query missing")
			}
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "proof") {
				t.Fatal("captcha in body")
			}
			return 200, `{"success":true,"data":{"access_token":"dashboard","user":{"id":42}}}`
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		}
		t.Fatalf("unexpected %s", r.URL)
		return 500, `{}`
	})
	_, ch, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: "invented"})
	if e != nil || ch == nil || ch.Kind != "captcha" {
		t.Fatalf("ch=%+v e=%v", ch, e)
	}
	session, ch, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: "invented", CaptchaToken: "proof+value"})
	if e != nil || ch != nil || session.UserID != 42 {
		t.Fatalf("session=%+v ch=%+v e=%v", session, ch, e)
	}
}
func TestConnectorUnsafeOriginRejectedBeforeHTTP(t *testing.T) {
	c := fixtureConnector(t, func(*http.Request) (int, string) { t.Fatal("unsafe origin reached HTTP"); return 500, `{}` })
	for _, origin := range []string{"http://upstream.example", "https://user:password@upstream.example", "https://upstream.example/path", "https://upstream.example?key=secret", "https://upstream.example#fragment"} {
		_, _, e := c.Login(context.Background(), Site{Platform: "sub2api", BaseURL: origin}, LoginInput{SessionToken: "fake"})
		if !errors.Is(e, ErrInvalid) {
			t.Fatalf("origin %s e=%v", origin, e)
		}
	}
}

func TestConnectorSub2APIPerRequestPriceUnit(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":2}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{}}`
		default:
			return 200, `{"code":0,"data":[{"name":"channel","platforms":[{"platform":"openai","groups":[{"id":7}],"supported_models":[{"name":"model","platform":"openai","pricing":{"billing_mode":"per_request","per_request_price":0.01}}]}]}]}`
		}
	})
	cat, e := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	p := cat.Groups[0].Prices[0]
	if p.Unit != "usd_per_request" || p.Input != nil || p.Details["channel"] != "channel" {
		t.Fatalf("wrong price metadata %+v", p)
	}
}

func TestConnectorNewAPILegacyEnvelopeDenialRequiresReauth(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/api/user/self" {
			t.Fatalf("unexpected %s", r.URL)
		}
		return 200, `{"success":false,"message":"invalid management token fixture-secret"}`
	})
	_, _, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{SessionToken: "invented-management-PAT", UserID: 42})
	if !errors.Is(e, ErrReauth) || strings.Contains(e.Error(), "fixture-secret") {
		t.Fatalf("management refusal not reauth: %v", e)
	}
	_, e = c.Discover(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "invented-management-PAT", UserID: 42, AuthVariant: "newapi_legacy_bearer"})
	if !errors.Is(e, ErrReauth) {
		t.Fatalf("discovery should require reauth: %v", e)
	}
}
func TestConnectorNewAPIKeyEnvelopeFailureIsNotReauth(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/api/user/self" {
			return 200, `{"success":true,"data":{"id":42}}`
		}
		if r.Method == "GET" {
			return 200, `{"success":true,"data":{"total":0,"items":[]}}`
		}
		return 200, `{"success":false,"message":"quota limit"}`
	})
	_, e := c.EnsureKey(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"}, RemoteGroup{ID: "vip"}, "marker")
	if !errors.Is(e, errConnectorUncertain) || errors.Is(e, ErrReauth) {
		t.Fatalf("key failure misclassified: %v", e)
	}
}
