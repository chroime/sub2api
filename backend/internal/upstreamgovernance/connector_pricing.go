package upstreamgovernance

import (
	"encoding/json"
	"math"
	"sort"
)

// Pricing metadata uses an explicit field allowlist, including nested objects.
// It is observed data only: expressions are never evaluated and URLs never fetched.
func connectorPriceFields(raw map[string]json.RawMessage, numbers, texts []string) (map[string]any, error) {
	out := map[string]any{}
	for _, field := range numbers {
		if value, ok := raw[field]; ok {
			if string(value) == "null" {
				out[field] = nil
				continue
			}
			var n float64
			if json.Unmarshal(value, &n) != nil || !connectorValidRate(&n) {
				return nil, ErrUnsupported
			}
			out[field] = n
		}
	}
	for _, field := range texts {
		if value, ok := raw[field]; ok {
			var text string
			if json.Unmarshal(value, &text) != nil || len(text) > 65536 {
				return nil, ErrUnsupported
			}
			out[field] = text
		}
	}
	return out, nil
}
func connectorMetadataObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil || len(out) > 200 {
		return nil, ErrUnsupported
	}
	return out, nil
}
func connectorMetadataArray(raw json.RawMessage) ([]json.RawMessage, error) {
	var out []json.RawMessage
	if json.Unmarshal(raw, &out) != nil || len(out) > 100 {
		return nil, ErrUnsupported
	}
	return out, nil
}
func connectorSortMetadata(values []any) {
	sort.Slice(values, func(i, j int) bool {
		a, _ := json.Marshal(values[i])
		b, _ := json.Marshal(values[j])
		return string(a) < string(b)
	})
}
func connectorStringSet(raw json.RawMessage) ([]string, error) {
	var values []string
	if json.Unmarshal(raw, &values) != nil || len(values) > 100 {
		return nil, ErrUnsupported
	}
	for _, value := range values {
		if len(value) > 256 {
			return nil, ErrUnsupported
		}
	}
	sort.Strings(values)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out, nil
}
func connectorSubPricingDetails(raw map[string]json.RawMessage) (map[string]any, error) {
	numeric := []string{"input_price", "output_price", "cache_write_price", "cache_write_1h_price", "cache_read_price", "image_input_price", "image_output_price", "per_request_price"}
	out, e := connectorPriceFields(raw, numeric, []string{"billing_mode"})
	if e != nil {
		return nil, e
	}
	if value, ok := raw["reasoning_effort_multipliers"]; ok && string(value) != "null" {
		var rates map[string]float64
		if json.Unmarshal(value, &rates) != nil || len(rates) > 100 {
			return nil, ErrUnsupported
		}
		for name, rate := range rates {
			if len(name) > 128 || !connectorValidRate(&rate) {
				return nil, ErrUnsupported
			}
		}
		out["reasoning_effort_multipliers"] = rates
	}
	if value, ok := raw["intervals"]; ok {
		rows, e := connectorMetadataArray(value)
		if e != nil {
			return nil, e
		}
		intervals := make([]any, 0, len(rows))
		for _, row := range rows {
			fields, e := connectorMetadataObject(row)
			if e != nil {
				return nil, e
			}
			interval, e := connectorPriceFields(fields, []string{"min_tokens", "max_tokens", "input_price", "output_price", "cache_write_price", "cache_write_1h_price", "cache_read_price", "input_multiplier", "output_multiplier", "cache_write_multiplier", "cache_read_multiplier", "per_request_price"}, []string{"tier_label"})
			if e != nil {
				return nil, e
			}
			for _, bound := range []string{"min_tokens", "max_tokens"} {
				if n, ok := interval[bound].(float64); ok && math.Trunc(n) != n {
					return nil, ErrUnsupported
				}
			}
			intervals = append(intervals, interval)
		}
		// Interval order can encode precedence; preserve it rather than sorting tiers.
		out["intervals"] = intervals
	}
	return out, nil
}
func connectorLocalized(raw json.RawMessage) (any, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if len(text) > 4096 {
			return nil, ErrUnsupported
		}
		return text, nil
	}
	var translations map[string]string
	if json.Unmarshal(raw, &translations) != nil || translations == nil || len(translations) > 100 {
		return nil, ErrUnsupported
	}
	for key, value := range translations {
		if len(key) > 128 || len(value) > 4096 {
			return nil, ErrUnsupported
		}
	}
	return translations, nil
}
func connectorUsageSchema(raw json.RawMessage) (map[string]any, error) {
	fields, e := connectorMetadataObject(raw)
	if e != nil {
		return nil, e
	}
	out := map[string]any{}
	for name, rawField := range fields {
		if len(name) > 128 {
			return nil, ErrUnsupported
		}
		field, e := connectorMetadataObject(rawField)
		if e != nil {
			return nil, e
		}
		item, e := connectorPriceFields(field, nil, []string{"type", "unit"})
		if e != nil {
			return nil, e
		}
		for _, key := range []string{"unitLabel", "description"} {
			if value, ok := field[key]; ok {
				local, e := connectorLocalized(value)
				if e != nil {
					return nil, e
				}
				item[key] = local
			}
		}
		if value, ok := field["enum"]; ok {
			values, e := connectorStringSet(value)
			if e != nil {
				return nil, e
			}
			item["enum"] = values
		}
		if value, ok := field["enumLabels"]; ok {
			labels, e := connectorMetadataObject(value)
			if e != nil {
				return nil, e
			}
			localized := map[string]any{}
			for key, label := range labels {
				if len(key) > 128 {
					return nil, ErrUnsupported
				}
				local, e := connectorLocalized(label)
				if e != nil {
					return nil, e
				}
				localized[key] = local
			}
			item["enumLabels"] = localized
		}
		out[name] = item
	}
	return out, nil
}
func connectorUsageExamples(raw json.RawMessage) ([]any, error) {
	rows, e := connectorMetadataArray(raw)
	if e != nil {
		return nil, e
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		fields, e := connectorMetadataObject(row)
		if e != nil {
			return nil, e
		}
		example, e := connectorPriceFields(fields, nil, []string{"label"})
		if e != nil {
			return nil, e
		}
		if rawFacts, ok := fields["facts"]; ok {
			facts, e := connectorMetadataObject(rawFacts)
			if e != nil {
				return nil, e
			}
			values := map[string]any{}
			for name, rawValue := range facts {
				if len(name) > 128 {
					return nil, ErrUnsupported
				}
				var value any
				if json.Unmarshal(rawValue, &value) != nil {
					return nil, ErrUnsupported
				}
				switch v := value.(type) {
				case string:
					if len(v) > 4096 {
						return nil, ErrUnsupported
					}
				case float64:
					if math.IsNaN(v) || math.IsInf(v, 0) {
						return nil, ErrUnsupported
					}
				default:
					return nil, ErrUnsupported
				}
				values[name] = value
			}
			example["facts"] = values
		}
		out = append(out, example)
	}
	connectorSortMetadata(out)
	return out, nil
}
func connectorBillingMetadata(raw map[string]json.RawMessage) (map[string]any, error) {
	out, e := connectorPriceFields(raw, nil, []string{"billing_mode", "billing_expr", "pricing_version"})
	if e != nil {
		return nil, e
	}
	if value, ok := raw["billing_usage_schema"]; ok && string(value) != "null" {
		schema, e := connectorUsageSchema(value)
		if e != nil {
			return nil, e
		}
		out["billing_usage_schema"] = schema
	}
	if value, ok := raw["billing_usage_examples"]; ok {
		examples, e := connectorUsageExamples(value)
		if e != nil {
			return nil, e
		}
		out["billing_usage_examples"] = examples
	}
	return out, nil
}
func connectorNewPricingDetails(raw map[string]json.RawMessage) (map[string]any, error) {
	out, e := connectorBillingMetadata(raw)
	if e != nil {
		return nil, e
	}
	if value, ok := raw["billing_plugin_variants"]; ok {
		rows, e := connectorMetadataArray(value)
		if e != nil {
			return nil, e
		}
		plugins := make([]any, 0, len(rows))
		for _, row := range rows {
			fields, e := connectorMetadataObject(row)
			if e != nil {
				return nil, e
			}
			plugin, e := connectorBillingMetadata(fields)
			if e != nil {
				return nil, e
			}
			names, e := connectorPriceFields(fields, nil, []string{"plugin_key", "plugin_name", "icon"})
			if e != nil {
				return nil, e
			}
			for k, v := range names {
				plugin[k] = v
			}
			plugins = append(plugins, plugin)
		}
		connectorSortMetadata(plugins)
		out["billing_plugin_variants"] = plugins
	}
	return out, nil
}
