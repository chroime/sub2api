package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

// The plaza is a storefront. Its groups must never expand the set of bindable
// groups returned by the authenticated groups API.
func (c *platformConnector) supplementPlaza(ctx context.Context, site Site, session Session, catalog *Catalog) error {
	if len(catalog.Groups) == 0 {
		return nil
	}
	var plaza struct {
		Groups []struct {
			ID               int64    `json:"id"`
			ImageIndependent *bool    `json:"image_rate_independent"`
			ImageRate        *float64 `json:"image_rate_multiplier"`
			LongContext      *bool    `json:"long_context_pricing_enabled"`
			Models           []struct {
				Name        string                     `json:"name"`
				Platform    string                     `json:"platform"`
				Pricing     map[string]json.RawMessage `json:"pricing"`
				Official    map[string]json.RawMessage `json:"official_pricing"`
				Basis       string                     `json:"long_context_basis"`
				TimePricing json.RawMessage            `json:"time_pricing"`
			} `json:"models"`
		} `json:"groups"`
	}
	if e := c.data(ctx, site, session, "GET", "/api/v1/model-plaza", nil, &plaza); e != nil {
		if errors.Is(e, errConnectorUnavailable) || errors.Is(e, errConnectorDenied) {
			catalog.Warnings = append(catalog.Warnings, "sub2api_model_plaza_unavailable")
			return nil
		}
		return e
	}
	if plaza.Groups == nil || len(plaza.Groups) > 1000 {
		return ErrUnsupported
	}
	indexes := map[string]int{}
	for i, g := range catalog.Groups {
		indexes[g.ID] = i
	}
	seen := map[int64]bool{}
	foundPrices := false
	for _, pg := range plaza.Groups {
		if pg.ID <= 0 || seen[pg.ID] {
			return ErrUnsupported
		}
		seen[pg.ID] = true
		idx, visible := indexes[strconv.FormatInt(pg.ID, 10)]
		if !visible {
			continue
		}
		if len(pg.Models) > connectorMaxModels || !connectorValidRate(pg.ImageRate) {
			return ErrUnsupported
		}
		g := &catalog.Groups[idx]
		modelsSeen := map[string]bool{}
		namesSeen := map[string]bool{}
		for _, name := range g.Models {
			namesSeen[name] = true
		}
		for _, model := range pg.Models {
			modelID := model.Platform + "\x00" + model.Name
			if model.Name == "" || len(model.Name) > 256 || len(model.Platform) > 128 || modelsSeen[modelID] {
				return ErrUnsupported
			}
			modelsSeen[modelID] = true
			if !namesSeen[model.Name] {
				g.Models = append(g.Models, model.Name)
				namesSeen[model.Name] = true
			}
			if model.Pricing == nil {
				continue
			}
			price, complex, e := connectorPlazaPrice(model.Name, model.Platform, model.Pricing)
			if e != nil {
				return e
			}
			price.Details["source"] = "/api/v1/model-plaza"
			if model.Basis != "" {
				if model.Basis != "whole_request" && model.Basis != "marginal" {
					return ErrUnsupported
				}
				price.Details["long_context_basis"] = model.Basis
			}
			if pg.ImageIndependent != nil {
				price.Details["image_rate_independent"] = *pg.ImageIndependent
			}
			if pg.ImageRate != nil {
				price.Details["image_rate_multiplier"] = *pg.ImageRate
			}
			if pg.LongContext != nil {
				price.Details["long_context_pricing_enabled"] = *pg.LongContext
			}
			if model.Official != nil {
				official, e := connectorSubPricingDetails(model.Official)
				if e != nil {
					return e
				}
				price.Details["official_reference_pricing"] = official
			}
			if len(model.TimePricing) > 0 && string(model.TimePricing) != "null" {
				schedule, e := connectorPlazaSchedule(model.TimePricing)
				if e != nil {
					return e
				}
				price.Details["time_pricing"] = schedule
			}
			// Plaza resolves the billing chain for this group/model. Keep other
			// channel-only models, but do not show two conflicting base prices.
			kept := g.Prices[:0]
			for _, previous := range g.Prices {
				if previous.Model != model.Name || previous.Platform != model.Platform {
					kept = append(kept, previous)
				}
			}
			g.Prices = append(kept, price)
			foundPrices = true
			if complex {
				catalog.Warnings = appendUniqueWarning(catalog.Warnings, "sub2api_complex_pricing_not_flattened")
			}
		}
		var e error
		g.Models, e = connectorModels(g.Models)
		if e != nil {
			return e
		}
	}
	if foundPrices {
		for i, warning := range catalog.Warnings {
			if warning == "sub2api_channels_pricing_unavailable" {
				catalog.Warnings[i] = "sub2api_channels_unavailable"
			}
		}
	}
	return nil
}

func appendUniqueWarning(warnings []string, warning string) []string {
	for _, existing := range warnings {
		if existing == warning {
			return warnings
		}
	}
	return append(warnings, warning)
}

func connectorPlazaPrice(name, platform string, raw map[string]json.RawMessage) (RemotePrice, bool, error) {
	details, e := connectorSubPricingDetails(raw)
	if e != nil {
		return RemotePrice{}, false, e
	}
	var value struct {
		Mode      string            `json:"billing_mode"`
		Input     *float64          `json:"input_price"`
		Output    *float64          `json:"output_price"`
		Request   *float64          `json:"per_request_price"`
		Intervals []json.RawMessage `json:"intervals"`
	}
	encoded, _ := json.Marshal(raw)
	if json.Unmarshal(encoded, &value) != nil {
		return RemotePrice{}, false, ErrUnsupported
	}
	p := RemotePrice{Model: name, Platform: platform, Unit: "usd_per_token", Input: value.Input, Output: value.Output, PerRequest: value.Request, Details: details}
	p.Details["per_request_unit"] = "usd_per_request"
	if value.Mode == "per_request" {
		p.Unit = "usd_per_request"
		p.Input = nil
		p.Output = nil
	}
	complex := len(value.Intervals) > 0 || value.Mode != "" && value.Mode != "token" && value.Mode != "per_request"
	if complex {
		p.Unit = "sub2api_complex_pricing"
		p.Input = nil
		p.Output = nil
		p.PerRequest = nil
	}
	return p, complex, nil
}

func connectorPlazaSchedule(raw json.RawMessage) (any, error) {
	var schedule struct {
		Timezone     string `json:"timezone"`
		WeekdaysOnly bool   `json:"weekdays_only"`
		Periods      []struct {
			Start      string   `json:"start_time"`
			End        string   `json:"end_time"`
			Multiplier *float64 `json:"multiplier"`
		} `json:"periods"`
	}
	if json.Unmarshal(raw, &schedule) != nil || len(schedule.Timezone) > 128 || len(schedule.Periods) > 100 {
		return nil, ErrUnsupported
	}
	for _, p := range schedule.Periods {
		if len(p.Start) > 8 || len(p.End) > 8 || p.Multiplier == nil || !connectorValidRate(p.Multiplier) {
			return nil, ErrUnsupported
		}
	}
	return schedule, nil
}
