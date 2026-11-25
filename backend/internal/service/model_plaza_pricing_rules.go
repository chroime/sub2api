package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// PlazaPricingRules describes one specific price card, not a vendor capability.
type PlazaPricingRules struct {
	Currency           string                 `json:"currency"`
	ContextStatus      string                 `json:"context_status"`
	ContextTierCount   int                    `json:"context_tier_count"`
	ContextBasis       ContextPricingBasis    `json:"context_basis"`
	TimeStatus         string                 `json:"time_status"`
	PriceBasis         string                 `json:"price_basis"`
	TimeRules          []PlazaPricingTimeRule `json:"time_rules,omitempty"`
	EvaluatedAt        time.Time              `json:"evaluated_at"`
	CurrentMultipliers map[string]float64     `json:"current_multipliers,omitempty"`
}

type PlazaPricingTimeRule struct {
	Source            string                   `json:"source"`
	Timezone          string                   `json:"timezone"`
	WeekdaysOnly      bool                     `json:"weekdays_only,omitempty"`
	BaseMultiplier    float64                  `json:"base_multiplier"`
	CurrentMultiplier float64                  `json:"current_multiplier"`
	Fields            []string                 `json:"fields"`
	Periods           []PlazaPricingTimePeriod `json:"periods"`
}

type PlazaPricingTimePeriod struct {
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	Multiplier float64 `json:"multiplier"`
}

func plazaRules(currency string, mode BillingMode, intervals []PricingInterval, present bool) *PlazaPricingRules {
	r := &PlazaPricingRules{Currency: currency, ContextStatus: "unknown", ContextBasis: ContextPricingBasisWholeRequest, TimeStatus: "unknown", PriceBasis: "standard"}
	if !present {
		return r
	}
	if mode != "" && mode != BillingModeToken {
		r.ContextStatus = "not_applicable"
		return r
	}
	r.ContextStatus, r.ContextTierCount = "flat", 1
	if count := len(filterValidIntervals(intervals)); count > 1 {
		r.ContextStatus, r.ContextTierCount = "tiered", count
	}
	return r
}

func (s *ModelPlazaService) fillPricingRules(_ context.Context, m *PlazaModel, g *Group, at time.Time) {
	var base *ModelPricing
	if s.billingService != nil {
		base, _ = s.billingService.GetModelPricing(m.Name)
	}
	official := plazaRules("", "", nil, false)
	if p := m.OfficialPricing; p != nil {
		official = plazaRules(p.Currency, p.BillingMode, p.Intervals, true)
		if rule := deepseekPlazaTimeRule(m.Name, base); rule != nil {
			official.TimeStatus = "peak_offpeak"
			official.TimeRules = []PlazaPricingTimeRule{*rule}
			if base.DeepSeekPeakPrice {
				official.PriceBasis = "peak"
			} else {
				official.PriceBasis = "off_peak"
			}
		}
	}
	m.OfficialPricingRules = official

	site := plazaRules("", "", nil, false)
	if p := m.Pricing; p != nil {
		currency := ""
		if base != nil {
			currency = base.Currency
			if currency == "" { // Billing's unmarked base cards are USD.
				currency = "USD"
			}
		} else if m.OfficialPricing != nil {
			currency = m.OfficialPricing.Currency
		}
		site = plazaRules(currency, p.BillingMode, p.Intervals, true)
		site.TimeStatus = "uniform"
		if m.LongContextBasis != "" {
			site.ContextBasis = m.LongContextBasis
		}
		if p.BillingMode == "" || p.BillingMode == BillingModeToken {
			if m.PricingSource == PlazaPricingSourceOfficial {
				if rule := deepseekPlazaTimeRule(m.Name, base); rule != nil {
					site.TimeStatus = "peak_offpeak"
					site.PriceBasis = "off_peak"
					if base.DeepSeekPeakPrice {
						site.PriceBasis = "peak"
					}
					site.TimeRules = append(site.TimeRules, *rule)
				}
			}
			if schedule := m.TimePricing; schedule != nil {
				rule := PlazaPricingTimeRule{Source: "channel", Timezone: schedule.Timezone, WeekdaysOnly: schedule.WeekdaysOnly, BaseMultiplier: 1, Fields: plazaAllTokenFields()}
				for _, p := range schedule.Periods {
					rule.Periods = append(rule.Periods, PlazaPricingTimePeriod{StartTime: p.StartTime, EndTime: p.EndTime, Multiplier: p.Multiplier})
				}
				if len(rule.Periods) > 0 {
					site.TimeRules = append(site.TimeRules, rule)
					site.TimeStatus = "scheduled"
				}
			}
			// Probe the same validated group policy used by token billing.
			if g != nil && g.PeakRateEnabled {
				if start, ok := parseMinutes(g.PeakStart); ok {
					at := time.Date(2026, 1, 5, start/60, start%60, 1, 0, timezone.Location())
					if multiplier := g.PeakMultiplierAt(at); multiplier != 1 {
						site.TimeRules = append(site.TimeRules, PlazaPricingTimeRule{Source: "group", Timezone: timezone.Location().String(), BaseMultiplier: 1, Fields: plazaAllTokenFields(), Periods: []PlazaPricingTimePeriod{{StartTime: g.PeakStart, EndTime: g.PeakEnd, Multiplier: multiplier}}})
						site.TimeStatus = "scheduled"
					}
				}
			}
		}
	}
	evaluatePlazaTimeRules(official, g, at)
	evaluatePlazaTimeRules(site, g, at)
	m.PricingRules = site
}

// Keep every source and price field on one response snapshot. The list quote
// retains its fixed basis; these factors describe the current time separately.
func evaluatePlazaTimeRules(rules *PlazaPricingRules, g *Group, at time.Time) {
	rules.EvaluatedAt = at
	if len(rules.TimeRules) == 0 {
		return
	}
	rules.CurrentMultipliers = make(map[string]float64)
	for _, field := range plazaAllTokenFields() {
		rules.CurrentMultipliers[field] = 1
	}
	for i := range rules.TimeRules {
		rule := &rules.TimeRules[i]
		switch rule.Source {
		case "official":
			rule.CurrentMultiplier = deepseekPeakMultiplierAt(at) * rule.BaseMultiplier
		case "channel":
			config := &ChannelTimePricing{Timezone: rule.Timezone, WeekdaysOnly: rule.WeekdaysOnly}
			for _, period := range rule.Periods {
				config.Periods = append(config.Periods, ChannelTimePricingPeriod{StartTime: period.StartTime, EndTime: period.EndTime, Multiplier: period.Multiplier})
			}
			rule.CurrentMultiplier = config.MultiplierAt(at)
		case "group":
			rule.CurrentMultiplier = g.PeakMultiplierAt(at)
		}
		for _, field := range rule.Fields {
			rules.CurrentMultipliers[field] *= rule.CurrentMultiplier
		}
	}
}

func plazaAllTokenFields() []string {
	return []string{"input_price", "output_price", "cache_read_price", "cache_write_price", "cache_write_1h_price", "cache_write_12h_price"}
}

func deepseekPlazaTimeRule(model string, base *ModelPricing) *PlazaPricingTimeRule {
	if base == nil || !isDeepSeekModel(model) {
		return nil
	}
	fields := []string{}
	if !base.priceOverrides.input {
		fields = append(fields, "input_price")
	}
	if !base.priceOverrides.output {
		fields = append(fields, "output_price")
	}
	if !base.priceOverrides.cacheRead {
		fields = append(fields, "cache_read_price")
	}
	if len(fields) == 0 {
		return nil
	}
	factor := 1.0
	if base.DeepSeekPeakPrice {
		factor = .5
	}
	rule := &PlazaPricingTimeRule{Source: "official", Timezone: "Asia/Shanghai", WeekdaysOnly: true, BaseMultiplier: factor, Fields: fields}
	for _, window := range deepseekPeakWindowsUTC {
		rule.Periods = append(rule.Periods, PlazaPricingTimePeriod{StartTime: fmt.Sprintf("%02d:00", window[0]+8), EndTime: fmt.Sprintf("%02d:00", window[1]+8), Multiplier: 2 * factor})
	}
	return rule
}
