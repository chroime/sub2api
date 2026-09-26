package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

func connectorValidRate(value *float64) bool {
	return value == nil || !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0
}
func connectorModels(models []string) ([]string, error) {
	if len(models) > connectorMaxModels {
		return nil, ErrUnsupported
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model) == "" || len(model) > 256 {
			return nil, ErrUnsupported
		}
		if !seen[model] {
			seen[model] = true
			out = append(out, model)
		}
	}
	sort.Strings(out)
	return out, nil
}
func (c *platformConnector) Discover(ctx context.Context, s Site, session Session) (Catalog, error) {
	session, e := c.identity(ctx, s, session)
	if e != nil {
		return Catalog{}, e
	}
	if s.Platform == "sub2api" {
		return c.subCatalog(ctx, s, session)
	}
	if s.Platform == "newapi" {
		return c.newCatalog(ctx, s, session)
	}
	return Catalog{}, ErrUnsupported
}
func (c *platformConnector) subCatalog(ctx context.Context, s Site, session Session) (Catalog, error) {
	catalog := Catalog{Groups: []RemoteGroup{}, Channels: []RemoteChannel{}, Warnings: []string{}}
	var groups []struct {
		ID          int64    `json:"id"`
		Name        string   `json:"name"`
		Platform    string   `json:"platform"`
		Rate        *float64 `json:"rate_multiplier"`
		PeakEnabled bool     `json:"peak_rate_enabled"`
		PeakStart   string   `json:"peak_start"`
		PeakEnd     string   `json:"peak_end"`
		PeakRate    *float64 `json:"peak_rate_multiplier"`
	}
	if e := c.data(ctx, s, session, "GET", "/api/v1/groups/available", nil, &groups); e != nil {
		return Catalog{}, e
	}
	if len(groups) > connectorMaxGroups {
		return Catalog{}, ErrUnsupported
	}
	rates := map[string]float64{}
	if e := c.data(ctx, s, session, "GET", "/api/v1/groups/rates", nil, &rates); e != nil {
		return Catalog{}, e
	}
	indexes := map[string]int{}
	for _, g := range groups {
		id := strconv.FormatInt(g.ID, 10)
		if g.ID <= 0 || g.Name == "" || !connectorValidRate(g.Rate) || !connectorValidRate(g.PeakRate) {
			return Catalog{}, ErrUnsupported
		}
		if _, ok := indexes[id]; ok {
			return Catalog{}, ErrUnsupported
		}
		r := RemoteGroup{ID: id, Name: g.Name, Platform: g.Platform, RateMultiplier: g.Rate, ResolvedRateMultiplier: g.Rate, PeakRateEnabled: g.PeakEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd, PeakRateMultiplier: g.PeakRate, Models: []string{}, Prices: []RemotePrice{}, Source: "sub2api:user-visible-groups"}
		if override, ok := rates[id]; ok {
			if !connectorValidRate(&override) {
				return Catalog{}, ErrUnsupported
			}
			r.UserRateMultiplier = &override
			r.ResolvedRateMultiplier = &override
		}
		indexes[id] = len(catalog.Groups)
		catalog.Groups = append(catalog.Groups, r)
	}
	var channels []struct {
		Name      string `json:"name"`
		Platforms []struct {
			Platform string `json:"platform"`
			Groups   []struct {
				ID int64 `json:"id"`
			} `json:"groups"`
			Models []struct {
				Name     string                     `json:"name"`
				Platform string                     `json:"platform"`
				Pricing  map[string]json.RawMessage `json:"pricing"`
			} `json:"supported_models"`
		} `json:"platforms"`
	}
	e := c.data(ctx, s, session, "GET", "/api/v1/channels/available", nil, &channels)
	if errors.Is(e, errConnectorUnavailable) || errors.Is(e, errConnectorDenied) {
		catalog.Warnings = append(catalog.Warnings, "sub2api_channels_pricing_unavailable")
		return catalog, nil
	}
	if e != nil {
		return Catalog{}, e
	}
	if len(channels) > 200 {
		return Catalog{}, ErrUnsupported
	}
	if len(channels) == 0 {
		catalog.Warnings = append(catalog.Warnings, "sub2api_channels_pricing_unavailable")
	}
	for _, ch := range channels {
		if ch.Name == "" || len(ch.Platforms) > 20 {
			return Catalog{}, ErrUnsupported
		}
		remote := RemoteChannel{Name: ch.Name, GroupIDs: []string{}, Models: []string{}}
		for _, section := range ch.Platforms {
			if len(section.Groups) > connectorMaxGroups || len(section.Models) > connectorMaxModels {
				return Catalog{}, ErrUnsupported
			}
			for _, ref := range section.Groups {
				id := strconv.FormatInt(ref.ID, 10)
				idx, ok := indexes[id]
				if !ok {
					continue
				}
				remote.GroupIDs = append(remote.GroupIDs, id)
				for _, model := range section.Models {
					if model.Name == "" {
						return Catalog{}, ErrUnsupported
					}
					catalog.Groups[idx].Models = append(catalog.Groups[idx].Models, model.Name)
					remote.Models = append(remote.Models, model.Name)
					if model.Pricing != nil {
						var p struct {
							Mode       string            `json:"billing_mode"`
							Input      *float64          `json:"input_price"`
							Output     *float64          `json:"output_price"`
							PerRequest *float64          `json:"per_request_price"`
							Intervals  []json.RawMessage `json:"intervals"`
						}
						encoded, _ := json.Marshal(model.Pricing)
						if json.Unmarshal(encoded, &p) != nil {
							return Catalog{}, ErrUnsupported
						}
						details, e := connectorSubPricingDetails(model.Pricing)
						if e != nil {
							return Catalog{}, e
						}
						details["channel"] = ch.Name
						details["per_request_unit"] = "usd_per_request"
						if !connectorValidRate(p.Input) || !connectorValidRate(p.Output) || !connectorValidRate(p.PerRequest) {
							return Catalog{}, ErrUnsupported
						}
						price := RemotePrice{Model: model.Name, Platform: model.Platform, Unit: "usd_per_token", Input: p.Input, Output: p.Output, PerRequest: p.PerRequest, Details: details}
						if p.Mode == "per_request" {
							price.Unit = "usd_per_request"
							price.Input = nil
							price.Output = nil
						}
						if len(p.Intervals) > 0 || p.Mode != "" && p.Mode != "token" && p.Mode != "per_request" {
							catalog.Warnings = append(catalog.Warnings, "sub2api_complex_pricing_not_flattened")
							price.Unit = "sub2api_complex_pricing"
							price.Input = nil
							price.Output = nil
							price.PerRequest = nil
						}
						catalog.Groups[idx].Prices = append(catalog.Groups[idx].Prices, price)
					}
				}
			}
		}
		remote.Models, e = connectorModels(remote.Models)
		if e != nil {
			return Catalog{}, e
		}
		sort.Strings(remote.GroupIDs)
		catalog.Channels = append(catalog.Channels, remote)
	}
	for i := range catalog.Groups {
		catalog.Groups[i].Models, e = connectorModels(catalog.Groups[i].Models)
		if e != nil {
			return Catalog{}, e
		}
	}
	return catalog, nil
}
func (c *platformConnector) newCatalog(ctx context.Context, s Site, session Session) (Catalog, error) {
	catalog := Catalog{Groups: []RemoteGroup{}, Channels: []RemoteChannel{}, Warnings: []string{"newapi_channels_not_exposed"}}
	var groups map[string]struct {
		Ratio json.RawMessage `json:"ratio"`
		Desc  string          `json:"desc"`
	}
	if e := c.data(ctx, s, session, "GET", "/api/user/self/groups", nil, &groups); e != nil {
		return Catalog{}, e
	}
	if len(groups) > connectorMaxGroups {
		return Catalog{}, ErrUnsupported
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g := groups[id]
		if id == "" || len(id) > 128 {
			return Catalog{}, ErrUnsupported
		}
		var rate *float64
		if len(g.Ratio) > 0 && string(g.Ratio) != "null" {
			if json.Unmarshal(g.Ratio, &rate) != nil {
				if id != "auto" {
					return Catalog{}, ErrUnsupported
				}
				var label string
				if json.Unmarshal(g.Ratio, &label) != nil {
					return Catalog{}, ErrUnsupported
				}
				rate = nil
			}
		}
		if !connectorValidRate(rate) {
			return Catalog{}, ErrUnsupported
		}
		var models []string
		if e := c.data(ctx, s, session, "GET", "/api/user/models?group="+urlQuery(id), nil, &models); e != nil {
			return Catalog{}, e
		}
		models, e := connectorModels(models)
		if e != nil {
			return Catalog{}, e
		}
		name := g.Desc
		if name == "" {
			name = id
		}
		catalog.Groups = append(catalog.Groups, RemoteGroup{ID: id, Name: name, Platform: "unknown", ResolvedRateMultiplier: rate, UserRateMultiplier: rate, Models: models, Prices: []RemotePrice{}, Source: "newapi:user-self-groups"})
	}
	var prices []map[string]json.RawMessage
	pricingResponse, pricingRaw, e := c.request(ctx, s, session, "GET", "/api/pricing", nil, nil, true)
	if errors.Is(e, errConnectorUnavailable) || errors.Is(e, errConnectorDenied) {
		catalog.Warnings = append(catalog.Warnings, "newapi_pricing_unavailable")
		return catalog, nil
	}
	if e != nil {
		return Catalog{}, e
	}
	if len(pricingResponse.Data) == 0 || string(pricingResponse.Data) == "null" || json.Unmarshal(pricingResponse.Data, &prices) != nil {
		return Catalog{}, ErrUnsupported
	}
	var pricingContext struct {
		GroupRatio map[string]float64 `json:"group_ratio"`
	}
	if json.Unmarshal(pricingRaw, &pricingContext) != nil {
		return Catalog{}, ErrUnsupported
	}
	if len(prices) > connectorMaxModels {
		return Catalog{}, ErrUnsupported
	}
	for _, raw := range prices {
		var p struct {
			Model      string            `json:"model_name"`
			Quota      *int              `json:"quota_type"`
			Ratio      *float64          `json:"model_ratio"`
			Completion *float64          `json:"completion_ratio"`
			Price      *float64          `json:"model_price"`
			Groups     []string          `json:"enable_groups"`
			Mode       string            `json:"billing_mode"`
			Expr       string            `json:"billing_expr"`
			Plugins    []json.RawMessage `json:"billing_plugin_variants"`
		}
		encoded, _ := json.Marshal(raw)
		if json.Unmarshal(encoded, &p) != nil || p.Model == "" || !connectorValidRate(p.Ratio) || !connectorValidRate(p.Completion) || !connectorValidRate(p.Price) || len(p.Groups) > connectorMaxGroups {
			return Catalog{}, ErrUnsupported
		}
		details, e := connectorNewPricingDetails(raw)
		if e != nil {
			return Catalog{}, e
		}
		price := RemotePrice{Model: p.Model, Platform: "unknown", Unit: "newapi_unknown", Details: details}
		for _, field := range []string{"quota_type", "model_ratio", "completion_ratio", "model_price", "cache_ratio", "create_cache_ratio", "image_ratio", "audio_ratio", "audio_completion_ratio"} {
			if value, ok := raw[field]; ok && string(value) != "null" {
				var number float64
				if json.Unmarshal(value, &number) != nil || !connectorValidRate(&number) {
					return Catalog{}, ErrUnsupported
				}
				price.Details[field] = number
			}
		}
		if value, ok := raw["supported_endpoint_types"]; ok {
			var endpoints []string
			if json.Unmarshal(value, &endpoints) != nil || len(endpoints) > 20 {
				return Catalog{}, ErrUnsupported
			}
			for _, endpoint := range endpoints {
				if len(endpoint) > 100 {
					return Catalog{}, ErrUnsupported
				}
			}
			sort.Strings(endpoints)
			price.Details["supported_endpoint_types"] = endpoints
		}
		if p.Quota != nil && p.Expr == "" && len(p.Plugins) == 0 && (p.Mode == "" || p.Mode == "token" || p.Mode == "per_request") {
			if *p.Quota == 0 {
				price.Unit = "newapi_ratio"
				price.Input = p.Ratio
				price.Output = p.Completion
			} else if *p.Quota == 1 {
				price.Unit = "usd_per_request"
				price.PerRequest = p.Price
			}
		}
		if price.Unit == "newapi_unknown" {
			catalog.Warnings = append(catalog.Warnings, "newapi_complex_pricing_not_flattened")
		}
		for i, g := range catalog.Groups {
			if (containsConnectorString(p.Groups, g.ID) || containsConnectorString(p.Groups, "all")) && containsConnectorString(g.Models, p.Model) {
				groupPrice := price
				groupPrice.Details = map[string]any{}
				for name, value := range price.Details {
					groupPrice.Details[name] = value
				}
				if ratio, ok := pricingContext.GroupRatio[g.ID]; ok {
					if !connectorValidRate(&ratio) {
						return Catalog{}, ErrUnsupported
					}
					groupPrice.Details["pricing_group_ratio"] = ratio
				}
				catalog.Groups[i].Prices = append(catalog.Groups[i].Prices, groupPrice)
			}
		}
	}
	return catalog, nil
}
func containsConnectorString(values []string, value string) bool {
	for _, s := range values {
		if s == value {
			return true
		}
	}
	return false
}
