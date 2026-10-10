package service

import "math"

// TokenUsageAdjustment controls the customer-facing token quantity used by
// billing. When a non-1 adjustment is enabled, cache read/write buckets are
// folded into the logical customer input so cache hits cannot reduce the
// customer charge; raw cache buckets remain available for upstream accounting.
type TokenUsageAdjustment struct {
	InputMultiplier  float64
	OutputMultiplier float64
	MinInputTokens   int
}

// TokenUsageBuckets contains the raw usage buckets reported by an upstream.
// InputTokens is the non-cache input count. CacheWriteTokens corresponds to
// the upstream's cache-creation/write bucket. The buckets are intentionally
// kept separate so upstream cost accounting can continue to use its native
// cache prices.
type TokenUsageBuckets struct {
	InputTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
	OutputTokens     int
}

type AdjustedTokenUsage struct {
	// Raw is the clamped upstream usage. It is never changed by an
	// adjustment and is suitable for administrator/account cost reporting.
	Raw TokenUsageBuckets
	// Customer is the usage to pass to customer billing. When an adjustment
	// is active, cache buckets are folded into Customer.InputTokens and set to
	// zero so cache hits and misses have the same customer charge.
	Customer TokenUsageBuckets
	// Applied reports whether the configured input/output adjustment was
	// applied. It is false for default multipliers and requests below the
	// minimum input threshold.
	Applied bool

	// InputTokens and OutputTokens are compatibility aliases for the customer
	// buckets. Existing callers that only handle non-cache input/output can
	// continue to use these fields.
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
	if a.MinInputTokens < 0 {
		a.MinInputTokens = 0
	}
	return a
}

func (a TokenUsageAdjustment) Apply(inputTokens, outputTokens int) AdjustedTokenUsage {
	return a.ApplyToUsage(TokenUsageBuckets{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	})
}

// ApplyForRequest applies the configured adjustment only when the request has
// reached the minimum input size. A zero threshold means every normal request
// is eligible; internal probes do not enter this billing path.
func (a TokenUsageAdjustment) ApplyForRequest(inputTokens, outputTokens int) AdjustedTokenUsage {
	return a.ApplyToUsage(TokenUsageBuckets{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	})
}

// ApplyToUsage produces both the raw upstream buckets and the customer-facing
// buckets used for billing. The raw counts are clamped but otherwise remain
// unchanged. For an enabled non-1 adjustment, the customer input is defined
// as the logical input (non-cache input + cache reads + cache writes), then
// multiplied by the configured input multiplier; customer output is multiplied
// independently. Cache buckets are zeroed in that adjusted customer view so
// the cache's discounted upstream prices cannot reduce the customer's charge.
//
// The minimum threshold is evaluated against the raw non-cache input only. A
// request below the threshold retains its original cache buckets and is not
// adjusted. A zero threshold makes all requests eligible. A group with both
// multipliers equal to one also retains the original buckets to preserve the
// existing cache pricing semantics.
func (a TokenUsageAdjustment) ApplyToUsage(raw TokenUsageBuckets) AdjustedTokenUsage {
	a = a.normalized()
	raw = clampTokenUsageBuckets(raw)

	result := AdjustedTokenUsage{
		Raw:      raw,
		Customer: raw,
	}
	adjustmentEnabled := a.InputMultiplier != 1 || a.OutputMultiplier != 1
	if !adjustmentEnabled || (a.MinInputTokens > 0 && raw.InputTokens < a.MinInputTokens) {
		result.InputTokens = result.Customer.InputTokens
		result.OutputTokens = result.Customer.OutputTokens
		return result
	}

	logicalInput := raw.InputTokens + raw.CacheReadTokens + raw.CacheWriteTokens
	// All buckets have been clamped to non-negative values. Guard the sum as
	// well so malformed, extremely large usage cannot wrap an int.
	if logicalInput < raw.InputTokens || logicalInput < raw.CacheReadTokens || logicalInput < raw.CacheWriteTokens {
		logicalInput = maxIntValue()
	}
	result.Customer = TokenUsageBuckets{
		InputTokens:  adjustedTokenCount(logicalInput, a.InputMultiplier),
		OutputTokens: adjustedTokenCount(raw.OutputTokens, a.OutputMultiplier),
	}
	result.Applied = true
	result.InputTokens = result.Customer.InputTokens
	result.OutputTokens = result.Customer.OutputTokens
	return result
}

// ApplyForRequestUsage is the cache-aware equivalent of ApplyForRequest. It
// is named explicitly to make call sites distinguish it from the legacy
// two-bucket helper above.
func (a TokenUsageAdjustment) ApplyForRequestUsage(inputTokens, cacheReadTokens, cacheWriteTokens, outputTokens int) AdjustedTokenUsage {
	return a.ApplyToUsage(TokenUsageBuckets{
		InputTokens:      inputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		OutputTokens:     outputTokens,
	})
}

func clampTokenUsageBuckets(usage TokenUsageBuckets) TokenUsageBuckets {
	return TokenUsageBuckets{
		InputTokens:      max(usage.InputTokens, 0),
		CacheReadTokens:  max(usage.CacheReadTokens, 0),
		CacheWriteTokens: max(usage.CacheWriteTokens, 0),
		OutputTokens:     max(usage.OutputTokens, 0),
	}
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func adjustedTokenCount(tokens int, multiplier float64) int {
	if tokens <= 0 {
		return 0
	}
	value := math.Round(float64(tokens) * multiplier)
	if value <= 0 {
		return 0
	}
	maxInt := maxIntValue()
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
