package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestConnectorAccountAndPlazaCollection(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.Header.Get("Authorization") != "Bearer fixture-session" {
			t.Fatal("discovery must use authenticated session")
		}
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42,"email":"fixture@example.test","username":"fixture","balance":12.5,"frozen_balance":2.5,"password":"secret-canary"}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"Bindable","platform":"openai","rate_multiplier":2}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{"7":0.5}}`
		case "/api/v1/channels/available":
			return 200, `{"code":0,"data":[]}`
		case "/api/v1/model-plaza":
			return 200, `{"code":0,"data":{"groups":[{"id":7,"long_context_pricing_enabled":true,"models":[{"name":"fixture-model","platform":"openai","pricing":{"billing_mode":"token","input_price":0.000002,"output_price":0.000008},"official_pricing":{"input_price":99,"secret":"secret-canary"},"long_context_basis":"whole_request","time_pricing":{"timezone":"Asia/Shanghai","periods":[{"start_time":"09:00","end_time":"12:00","multiplier":1.5}]}}]},{"id":99,"name":"Storefront only","models":[{"name":"hidden-model","pricing":{"input_price":1}}]}]}}`
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return 500, ""
		}
	})
	cat, err := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture-session", UserID: 42})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cat)
	// Check serialized field names independently of the implementation's DTO.
	var values map[string]json.RawMessage
	_ = json.Unmarshal(raw, &values)
	if len(values["account"]) == 0 {
		t.Fatalf("account summary missing: %s", raw)
	}
	var account struct {
		Balance *float64 `json:"balance"`
		Frozen  *float64 `json:"frozen_balance"`
		Unit    string   `json:"unit"`
	}
	_ = json.Unmarshal(values["account"], &account)
	if account.Balance == nil || *account.Balance != 12.5 || account.Frozen == nil || *account.Frozen != 2.5 || account.Unit != "usd" {
		t.Fatalf("balance incorrectly converted or frozen subtracted twice: %s", values["account"])
	}
	if len(cat.Groups) != 1 || len(cat.Groups[0].Prices) != 1 || len(cat.Groups[0].Models) != 1 {
		t.Fatalf("plaza not merged or broadened visibility: %+v", cat)
	}
	p := cat.Groups[0].Prices[0]
	if p.Input == nil || *p.Input != 0.000002 || p.Unit != "usd_per_token" || *cat.Groups[0].ResolvedRateMultiplier != 0.5 {
		t.Fatal("base pricing must not be replaced by official price or pre-multiplied")
	}
	if p.Details["long_context_basis"] != "whole_request" || p.Details["time_pricing"] == nil {
		t.Fatal("lost pricing conditions")
	}
	if strings.Contains(string(raw), "secret-canary") || strings.Contains(string(raw), "hidden-model") {
		t.Fatal("non-allowlisted data escaped")
	}
}

func TestConnectorAccountMissingAndQuotaUnits(t *testing.T) {
	for _, tc := range []struct {
		name, platform, profile, unit string
		balance                       *float64
	}{
		{"missing", "sub2api", `{"id":42}`, "usd", nil},
		{"negative", "sub2api", `{"id":42,"balance":-1.25}`, "usd", func() *float64 { v := -1.25; return &v }()},
		{"newapi", "newapi", `{"id":42,"quota":250000,"used_quota":100000}`, "quota", func() *float64 { v := 250000.0; return &v }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureConnector(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/api/v1/user/profile":
					return 200, `{"code":0,"data":` + tc.profile + `}`
				case "/api/user/self":
					return 200, `{"success":true,"data":` + tc.profile + `}`
				case "/api/v1/groups/available", "/api/v1/channels/available":
					return 200, `{"code":0,"data":[]}`
				case "/api/v1/groups/rates":
					return 200, `{"code":0,"data":{}}`
				case "/api/user/self/groups":
					return 200, `{"success":true,"data":{}}`
				case "/api/pricing":
					return 200, `{"success":true,"data":[]}`
				default:
					t.Fatalf("unexpected %s", r.URL.Path)
					return 500, ""
				}
			})
			cat, e := c.Discover(context.Background(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42})
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(cat)
			var got struct {
				Account *struct {
					Balance *float64 `json:"balance"`
					Unit    string   `json:"unit"`
					Used    *float64 `json:"used_balance"`
				} `json:"account"`
			}
			if json.Unmarshal(raw, &got) != nil || got.Account == nil {
				t.Fatal("missing account")
			}
			if got.Account.Unit != tc.unit || (got.Account.Balance == nil) != (tc.balance == nil) {
				t.Fatal("unknown or native unit lost")
			}
			if tc.balance != nil && *got.Account.Balance != *tc.balance {
				t.Fatal("incorrect balance")
			}
			if tc.platform == "newapi" && (got.Account.Used == nil || *got.Account.Used != 100000) {
				t.Fatal("used quota lost")
			}
		})
	}
}

func TestConnectorPlazaCannotHideExpiredSession(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"VIP"}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{}}`
		case "/api/v1/channels/available":
			return 404, `{}`
		case "/api/v1/model-plaza":
			return 401, `{}`
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
			return 500, ""
		}
	})
	_, e := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture"})
	if !errors.Is(e, ErrReauth) {
		t.Fatalf("reauth not propagated: %v", e)
	}
}

func TestConnectorCompositePlazaKeepsSameNameAcrossPlatforms(t *testing.T) {
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case "/api/v1/groups/available":
			return 200, `{"code":0,"data":[{"id":7,"name":"Mixed","platform":"composite"}]}`
		case "/api/v1/groups/rates":
			return 200, `{"code":0,"data":{}}`
		case "/api/v1/channels/available":
			return 200, `{"code":0,"data":[{"name":"channel","platforms":[{"platform":"anthropic","groups":[{"id":7}],"supported_models":[{"name":"shared-model","platform":"anthropic","pricing":{"input_price":0.1}}]}]}]}`
		case "/api/v1/model-plaza":
			return 200, `{"code":0,"data":{"groups":[{"id":7,"models":[{"name":"shared-model","platform":"anthropic","pricing":{"input_price":0.2}},{"name":"shared-model","platform":"openai","pricing":{"input_price":0.3}}]}]}}`
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
			return 500, ""
		}
	})
	cat, e := c.Discover(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if len(cat.Groups) != 1 || len(cat.Groups[0].Prices) != 2 || len(cat.Groups[0].Models) != 1 {
		t.Fatal("model/transport identity lost")
	}
	for _, p := range cat.Groups[0].Prices {
		if p.Platform == "anthropic" && *p.Input != 0.2 || p.Platform == "openai" && *p.Input != 0.3 {
			t.Fatal("plaza price not keyed by transport")
		}
	}
}

func TestConnectorPlazaBoundsUniqueMergedModelNames(t *testing.T) {
	names := make([]string, 1001)
	models := make([]map[string]string, 1001)
	for i := range names {
		names[i] = "model-" + strconv.Itoa(i)
		models[i] = map[string]string{"name": names[i], "platform": "openai"}
	}
	payload, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"groups": []any{map[string]any{"id": 7, "models": models}}}})
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/api/v1/model-plaza" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return 200, string(payload)
	}).(*platformConnector)
	cat := Catalog{Groups: []RemoteGroup{{ID: "7", Models: names}}}
	e := c.supplementPlaza(context.Background(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture"}, &cat)
	if e != nil {
		t.Fatal(e)
	}
	if len(cat.Groups[0].Models) != 1001 {
		t.Fatal("merged model names duplicated")
	}
}
