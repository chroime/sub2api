package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

// Quality gates are written only by the quality store in the same transaction
// as its state and scheduler outbox event. Imports and general account edits
// must neither forge an owner nor erase another owner's isolation.
func stripAccountQualityRuntimeExtra(extra map[string]any) map[string]any {
	if _, exists := extra[service.AccountQualityGatesExtraKey]; !exists {
		return extra
	}
	clean := copyJSONMap(extra)
	delete(clean, service.AccountQualityGatesExtraKey)
	return clean
}

func mergeAccountQualityRuntimeExtra(extra, current map[string]any) {
	delete(extra, service.AccountQualityGatesExtraKey)
	if value, exists := current[service.AccountQualityGatesExtraKey]; exists {
		extra[service.AccountQualityGatesExtraKey] = value
	}
}
