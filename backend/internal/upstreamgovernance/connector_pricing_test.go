package upstreamgovernance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func connectorPricingCatalog(t *testing.T, platform, pricing string) Catalog {
	t.Helper()
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		if platform == "sub2api" {
			switch r.URL.Path {
			case "/api/v1/user/profile":
				return 200, `{"code":0,"data":{"id":42}}`
			case "/api/v1/groups/available":
				return 200, `{"code":0,"data":[{"id":7,"name":"VIP","platform":"openai","rate_multiplier":1}]}`
			case "/api/v1/groups/rates":
				return 200, `{"code":0,"data":{}}`
			default:
				return 200, `{"code":0,"data":[{"name":"channel","platforms":[{"platform":"openai","groups":[{"id":7}],"supported_models":[{"name":"model","platform":"openai","pricing":` + pricing + `}]}]}]}`
			}
		}
		switch r.URL.Path {
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		case "/api/user/self/groups":
			return 200, `{"success":true,"data":{"vip":{"ratio":1}}}`
		case "/api/user/models":
			return 200, `{"success":true,"data":["model"]}`
		default:
			return 200, `{"success":true,"data":[` + pricing + `]}`
		}
	})
	cat, e := c.Discover(context.Background(), Site{Platform: platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	return cat
}
func TestConnectorComplexPricingChangesProduceEvents(t *testing.T) {
	for _, tc := range []struct{ name, platform, before, after string }{
		{"cache_write", "sub2api", `{"billing_mode":"token","cache_write_price":1}`, `{"billing_mode":"token","cache_write_price":2}`},
		{"cache_1h", "sub2api", `{"billing_mode":"token","cache_write_1h_price":1}`, `{"billing_mode":"token","cache_write_1h_price":2}`},
		{"cache_read", "sub2api", `{"billing_mode":"token","cache_read_price":1}`, `{"billing_mode":"token","cache_read_price":2}`},
		{"reasoning", "sub2api", `{"billing_mode":"token","reasoning_effort_multipliers":{"high":1}}`, `{"billing_mode":"token","reasoning_effort_multipliers":{"high":2}}`},
		{"interval", "sub2api", `{"billing_mode":"token","intervals":[{"min_tokens":0,"max_tokens":1000,"cache_read_multiplier":1}]}`, `{"billing_mode":"token","intervals":[{"min_tokens":0,"max_tokens":1000,"cache_read_multiplier":2}]}`},
		{"image", "sub2api", `{"billing_mode":"image","image_input_price":1,"image_output_price":3}`, `{"billing_mode":"image","image_input_price":2,"image_output_price":3}`},
		{"video", "sub2api", `{"billing_mode":"video","per_request_price":1}`, `{"billing_mode":"video","per_request_price":2}`},
		{"expression", "newapi", `{"billing_mode":"tiered_expr","billing_expr":"input * 1"}`, `{"billing_mode":"tiered_expr","billing_expr":"input * 2"}`},
		{"plugin", "newapi", `{"billing_plugin_variants":[{"plugin_key":"video","billing_expr":"seconds * 1"}]}`, `{"billing_plugin_variants":[{"plugin_key":"video","billing_expr":"seconds * 2"}]}`},
		{"schema", "newapi", `{"billing_usage_schema":{"seconds":{"unit":"second"}}}`, `{"billing_usage_schema":{"seconds":{"unit":"credit"}}}`},
		{"example", "newapi", `{"billing_usage_examples":[{"label":"small","facts":{"seconds":1}}]}`, `{"billing_usage_examples":[{"label":"small","facts":{"seconds":2}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.platform == "newapi" {
				tc.before = strings.TrimSuffix(tc.before, "}") + `,"model_name":"model","enable_groups":["vip"]}`
				tc.after = strings.TrimSuffix(tc.after, "}") + `,"model_name":"model","enable_groups":["vip"]}`
			}
			before := connectorPricingCatalog(t, tc.platform, tc.before)
			after := connectorPricingCatalog(t, tc.platform, tc.after)
			events := DiffCatalog(1, before, after)
			if len(events) != 1 || events[0].Kind != "price_changed" {
				t.Fatalf("pricing change lost: %+v", events)
			}
			if len(after.Groups[0].Prices) != 1 {
				t.Fatal("complex price omitted")
			}
			if tc.name == "interval" || tc.name == "image" || tc.name == "video" || tc.name == "expression" || tc.name == "plugin" {
				p := after.Groups[0].Prices[0]
				if p.Input != nil || p.Output != nil || p.PerRequest != nil {
					t.Fatal("complex price flattened")
				}
			}
		})
	}
}
func TestConnectorPricingMetadataCanonicalOrder(t *testing.T) {
	before := `{"model_name":"model","enable_groups":["vip"],"supported_endpoint_types":["openai","anthropic"],"billing_plugin_variants":[{"plugin_key":"a","billing_expr":"x","billing_usage_schema":{"x":{"enum":["a","b"]}}},{"plugin_key":"b","billing_expr":"y"}],"billing_usage_examples":[{"label":"a","facts":{"x":1}},{"label":"b","facts":{"x":2}}]}`
	after := `{"model_name":"model","enable_groups":["vip"],"supported_endpoint_types":["anthropic","openai"],"billing_plugin_variants":[{"plugin_key":"b","billing_expr":"y"},{"plugin_key":"a","billing_expr":"x","billing_usage_schema":{"x":{"enum":["b","a"]}}}],"billing_usage_examples":[{"label":"b","facts":{"x":2}},{"label":"a","facts":{"x":1}}]}`
	if events := DiffCatalog(1, connectorPricingCatalog(t, "newapi", before), connectorPricingCatalog(t, "newapi", after)); len(events) != 0 {
		t.Fatalf("unordered metadata generated events: %+v", events)
	}
	before = `{"billing_mode":"token","intervals":[{"min_tokens":0,"input_price":1},{"min_tokens":1000,"input_price":2}]}`
	after = `{"billing_mode":"token","intervals":[{"min_tokens":1000,"input_price":2},{"min_tokens":0,"input_price":1}]}`
	if events := DiffCatalog(1, connectorPricingCatalog(t, "sub2api", before), connectorPricingCatalog(t, "sub2api", after)); len(events) != 1 || events[0].Kind != "price_changed" {
		t.Fatal("semantically ordered tiers lost")
	}
}
func TestConnectorPricingMetadataWhitelist(t *testing.T) {
	for _, tc := range []struct{ platform, pricing string }{{"sub2api", `{"billing_mode":"token","dashboard_secret":"never-copy","intervals":[{"min_tokens":0,"internal_id":"never-copy"}]}`}, {"newapi", `{"model_name":"model","enable_groups":["vip"],"dashboard_secret":"never-copy","billing_plugin_variants":[{"plugin_key":"a","password":"never-copy","billing_expr":"x"}]}`}} {
		cat := connectorPricingCatalog(t, tc.platform, tc.pricing)
		raw, _ := json.Marshal(cat)
		if strings.Contains(string(raw), "never-copy") {
			t.Fatal("non-whitelisted secret copied")
		}
	}
}
