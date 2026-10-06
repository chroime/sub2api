package service

import "math"

// TokenUsageAdjustment controls the customer-facing token quantity used by
// billing. Cache read/write buckets are deliberately excluded: they remain
// independently priced and must not be multiplied twice.
type TokenUsageAdjustment struct {
	InputMultiplier  float64
	OutputMultiplier float64
	MinInputTokens   int
}

type AdjustedTokenUsage struct {
	InputTokens  int
	OutputTokens int
}

func (a TokenUsageAdjustment) normalized() TokenUsageAdjustment {
	if a.InputMultiplier <= 0 || math.IsNaN(a.InputMultiplier) || math.IsInf(a.InputMultiplier, 0) {
		a.InputMultiplier = 1
	}
	if a.OutputMultiplier <= 0 || math.IsNaN(a.OutputMultiplier) || math.IsInf(a.OutputMultiplier, 0) {
		a.OutputMultiplier = 1
	}
	return a
}

func (a TokenUsageAdjustment) Apply(inputTokens, outputTokens int) AdjustedTokenUsage {
	a = a.normalized()
	return AdjustedTokenUsage{
		InputTokens:  adjustedTokenCount(inputTokens, a.InputMultiplier),
		OutputTokens: adjustedTokenCount(outputTokens, a.OutputMultiplier),
	}
}

// ApplyForRequest applies the configured adjustment only when the request has
// reached the minimum input size. A zero threshold means every normal request
// is eligible; internal probes do not enter this billing path.
func (a TokenUsageAdjustment) ApplyForRequest(inputTokens, outputTokens int) AdjustedTokenUsage {
	if a.MinInputTokens > 0 && inputTokens < a.MinInputTokens {
		return AdjustedTokenUsage{InputTokens: max(inputTokens, 0), OutputTokens: max(outputTokens, 0)}
	}
	return a.Apply(inputTokens, outputTokens)
}

func adjustedTokenCount(tokens int, multiplier float64) int {
	if tokens <= 0 {
		return 0
	}
	value := math.Round(float64(tokens) * multiplier)
	if value <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if value >= float64(maxInt) {
		return maxInt
	}
	return int(value)
}

func tokenUsageAdjustmentForGroup(group *Group) TokenUsageAdjustment {
	if group == nil {
		return TokenUsageAdjustment{InputMultiplier: 1, OutputMultiplier: 1}
	}
	return TokenUsageAdjustment{
		InputMultiplier:  group.BillingInputTokenMultiplier,
		OutputMultiplier: group.BillingOutputTokenMultiplier,
		MinInputTokens:   group.BillingTokenAdjustmentMinInputTokens,
	}.normalized()
}
