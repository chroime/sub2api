package service

import (
	"context"
	"strings"
	"time"
)

// AccountQualityGatesExtraKey is reserved for the quality store's binding-owned
// admission projection. General account edits cannot create or remove gates.
const AccountQualityGatesExtraKey = "account_quality_gates"

// Selection carries its resolved model through post-slot account hydration.
// This value is internal and grants no bypass to downstream requests.
type accountQualitySelectionModelKey struct{}

func withAccountQualitySelectionModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, accountQualitySelectionModelKey{}, strings.TrimSpace(model))
}

// IsQualityBlocked reports account-wide isolation. Model-only isolation leaves
// the account available for its other models. A probe's cooldown never expires a
// gate; only an explicit release or an active manual override can permit traffic.
func (a *Account) IsQualityBlocked() bool {
	return a.isQualityBlockedForModel("", time.Now())
}

// IsQualityBlockedForModel includes account-wide isolation and the exact tested
// model. Aliases resolving to the same account mapping share that model's gate;
// unrelated model families and accounts do not inherit it.
func (a *Account) IsQualityBlockedForModel(model string) bool {
	return a.isQualityBlockedForModel(strings.TrimSpace(model), time.Now())
}

func (a *Account) isQualityBlockedForModel(model string, now time.Time) bool {
	if a == nil {
		return false
	}
	gates, _ := a.Extra[AccountQualityGatesExtraKey].(map[string]any)
	for _, raw := range gates {
		gate, ok := raw.(map[string]any)
		if !ok || gate["blocked"] != true {
			continue
		}
		if rawUntil, ok := gate["override_until"].(string); ok {
			if until, err := time.Parse(time.RFC3339, rawUntil); err == nil && now.Before(until) {
				continue
			}
		}
		scope, _ := gate["scope"].(string)
		if scope == "account" {
			return true
		}
		if scope != "model" || model == "" {
			continue
		}
		gateModel, _ := gate["model"].(string)
		gateModel = strings.TrimSpace(gateModel)
		if gateModel == "" {
			continue
		}
		if model == gateModel || a.qualityMappedModel(model) == a.qualityMappedModel(gateModel) {
			return true
		}
	}
	return false
}

func (a *Account) qualityMappedModel(model string) string {
	if a.IsOpenAI() {
		if mapped := strings.TrimSpace(resolveOpenAIAccountUpstreamModelForRequest(a, model, false)); mapped != "" {
			return mapped
		}
		return model
	}
	if a.Platform == PlatformAntigravity {
		if mapped := strings.TrimSpace(mapAntigravityModel(a, model)); mapped != "" {
			return mapped
		}
	}
	if mapped := strings.TrimSpace(a.GetMappedModel(model)); mapped != "" {
		return mapped
	}
	return model
}
