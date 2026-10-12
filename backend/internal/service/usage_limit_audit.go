package service

import (
	"encoding/json"
	"math"
	"strings"
)

const usageAuditMaxSafeInteger int64 = 1<<53 - 1

// UsageLimitAudit is a read-only integrity check of stored upstream usage.
// Unknown includes partial metadata: a valid result requires every ceiling to
// be known. Invalid always wins when a count or a known ceiling is violated.
// Nil totals mean the raw count is unavailable, negative, or cannot be summed
// and represented exactly in a JSON/JavaScript number. The original UsageLog
// and its billed values never change.
type UsageLimitAudit struct {
	Status             string   `json:"status"`
	Model              string   `json:"model"`
	InputLimit         *int64   `json:"input_limit"`
	OutputLimit        *int64   `json:"output_limit"`
	ContextLimit       *int64   `json:"context_limit"`
	TotalInputTokens   *int64   `json:"total_input_tokens"`
	TotalOutputTokens  *int64   `json:"total_output_tokens"`
	TotalContextTokens *int64   `json:"total_context_tokens"`
	Issues             []string `json:"issues"`
}

// AuditUsageLimits checks raw usage against the account's already loaded model
// snapshot. Use the PricingService method to also supply catalog input/output
// ceilings. Neither form performs I/O or changes billing/response projection.
func AuditUsageLimits(log *UsageLog) *UsageLimitAudit {
	return (*PricingService)(nil).AuditUsageLimits(log)
}

// AuditUsageLimits uses exact/canonical catalog identities and account model
// metadata, never the pricing service's fuzzy model-family fallbacks. Cache
// reads and writes consume the same input budget as non-cache input; there is
// no invented independent cache ceiling. Upstream output already includes
// reasoning and image tokens, so those subsets are not added a second time.
func (s *PricingService) AuditUsageLimits(log *UsageLog) *UsageLimitAudit {
	audit := &UsageLimitAudit{Status: "valid", Issues: make([]string, 0)}
	if log == nil {
		audit.unknown("missing_usage")
		return audit
	}
	audit.Model = usageAuditModel(log)
	if audit.Model == "" {
		audit.unknown("missing_model")
	}
	s.populateUsageAuditLimits(audit, log.Account)
	for _, limit := range []struct {
		value   **int64
		missing string
		unsafe  string
	}{
		{&audit.InputLimit, "missing_input_limit", "input_limit_unsafe_integer"},
		{&audit.OutputLimit, "missing_output_limit", "output_limit_unsafe_integer"},
		{&audit.ContextLimit, "missing_context_limit", "context_limit_unsafe_integer"},
	} {
		if *limit.value == nil {
			audit.unknown(limit.missing)
		} else if **limit.value > usageAuditMaxSafeInteger {
			*limit.value = nil
			audit.unknown(limit.unsafe)
		}
	}

	unsafeCount := false
	for _, count := range []struct {
		value int
		code  string
	}{
		{log.UpstreamInputTokens, "negative_upstream_input_tokens"},
		{log.UpstreamOutputTokens, "negative_upstream_output_tokens"},
		{log.AuditCacheReadTokens(), "negative_cache_read_tokens"},
		{log.AuditCacheCreationTokens(), "negative_cache_creation_tokens"},
		{log.AuditCacheCreation5mTokens(), "negative_cache_creation_5m_tokens"},
		{log.AuditCacheCreation1hTokens(), "negative_cache_creation_1h_tokens"},
	} {
		if count.value < 0 {
			audit.invalid(count.code)
		}
		unsafeCount = unsafeCount || int64(count.value) > usageAuditMaxSafeInteger
	}
	if unsafeCount {
		audit.invalid("token_count_unsafe_integer")
	}

	cacheRead := log.AuditCacheReadTokens()
	cacheCreation := log.AuditCacheCreationTokens()
	cacheCreation5m := log.AuditCacheCreation5mTokens()
	cacheCreation1h := log.AuditCacheCreation1hTokens()
	var cacheDetails *int64
	if cacheCreation5m >= 0 && cacheCreation1h >= 0 {
		if total, ok := sumUsageAuditTokens(int64(cacheCreation5m), int64(cacheCreation1h)); ok {
			cacheDetails = &total
			if total > usageAuditMaxSafeInteger {
				audit.invalid("cache_creation_sum_unsafe_integer")
			}
		} else {
			audit.invalid("cache_creation_sum_overflow")
		}
	}
	var cacheWrite *int64
	if cacheCreation > 0 {
		total := int64(cacheCreation)
		cacheWrite = &total
		// Zero details mean no TTL breakdown was reported. A nonzero breakdown
		// must agree with the aggregate; report contradictions without repair.
		if cacheDetails != nil && *cacheDetails > 0 && *cacheDetails != total {
			audit.invalid("cache_creation_total_mismatch")
		}
	} else if cacheCreation == 0 {
		cacheWrite = cacheDetails
	}

	inputAvailable := log.UpstreamInputTokens >= 0
	outputAvailable := log.UpstreamOutputTokens >= 0
	// Older rows have zero defaults for raw counts. A positive billed count
	// with a zero raw count cannot establish raw usage, and must not become a
	// fabricated zero or a substitution of customer billing for upstream usage.
	if log.UpstreamInputTokens == 0 && log.InputTokens > 0 {
		audit.unknown("upstream_input_unavailable")
		inputAvailable = false
	}
	if log.UpstreamOutputTokens == 0 && log.OutputTokens > 0 {
		audit.unknown("upstream_output_unavailable")
		outputAvailable = false
	}
	// Even if a historical raw count is missing, the known cache/input counts
	// are a lower bound. They can prove a violation without fabricating a total.
	var knownInput, knownOutput, knownContext *int64
	if log.UpstreamInputTokens >= 0 && cacheRead >= 0 && cacheWrite != nil {
		if total, ok := sumUsageAuditTokens(int64(log.UpstreamInputTokens), int64(cacheRead), *cacheWrite); ok {
			knownInput = &total
			if total > usageAuditMaxSafeInteger {
				audit.invalid("input_sum_unsafe_integer")
			} else if inputAvailable {
				audit.TotalInputTokens = &total
			}
		} else {
			audit.invalid("input_sum_overflow")
		}
	}
	if log.UpstreamOutputTokens >= 0 {
		total := int64(log.UpstreamOutputTokens)
		knownOutput = &total
		if outputAvailable && total <= usageAuditMaxSafeInteger {
			audit.TotalOutputTokens = &total
		}
	}
	if knownInput != nil && knownOutput != nil {
		if total, ok := sumUsageAuditTokens(*knownInput, *knownOutput); ok {
			knownContext = &total
			if total > usageAuditMaxSafeInteger {
				audit.invalid("context_sum_unsafe_integer")
			} else if inputAvailable && outputAvailable {
				audit.TotalContextTokens = &total
			}
		} else {
			audit.invalid("context_sum_overflow")
		}
	}
	for _, budget := range []struct {
		total *int64
		limit *int64
		code  string
	}{
		{knownInput, audit.InputLimit, "input_limit_exceeded"},
		{knownOutput, audit.OutputLimit, "output_limit_exceeded"},
		{knownContext, audit.ContextLimit, "context_limit_exceeded"},
	} {
		if budget.total != nil && budget.limit != nil && *budget.total > *budget.limit {
			audit.invalid(budget.code)
		}
	}
	return audit
}

func (a *UsageLimitAudit) unknown(code string) {
	if a.Status != "invalid" {
		a.Status = "unknown"
	}
	a.Issues = append(a.Issues, code)
}

func (a *UsageLimitAudit) invalid(code string) {
	a.Status = "invalid"
	a.Issues = append(a.Issues, code)
}

func usageAuditModel(log *UsageLog) string {
	for _, model := range []*string{log.UpstreamResponseModel, log.UpstreamModel, &log.RequestedModel, &log.Model} {
		if model != nil {
			if value := strings.TrimSpace(*model); value != "" {
				return value
			}
		}
	}
	return ""
}

func (s *PricingService) populateUsageAuditLimits(audit *UsageLimitAudit, account *Account) {
	if audit.Model == "" {
		return
	}
	// Preserve exact entry precedence when a catalog publishes limits for an
	// alias separately; normalize only when no exact catalog entry is present.
	modelLower := strings.ToLower(audit.Model)
	candidates := []string{audit.Model, modelLower}
	candidates = append(candidates, s.buildModelLookupCandidates(modelLower)...)
	if s != nil {
		s.mu.RLock()
		for _, model := range candidates {
			if pricing := s.pricingData[model]; pricing != nil {
				audit.InputLimit = positiveUsageAuditLimit(pricing.MaxInputTokens)
				audit.OutputLimit = positiveUsageAuditLimit(pricing.MaxOutputTokens)
				break
			}
		}
		s.mu.RUnlock()
	}
	if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
		for _, model := range candidates {
			if metadata, ok := snapshot.Models[model]; ok {
				context := metadata.MaxContextWindow
				if context <= 0 {
					context = metadata.ContextWindow
				}
				audit.ContextLimit = positiveUsageAuditLimit(&context)
				if output := positiveUsageAuditLimit(&metadata.MaxOutputTokens); output != nil {
					audit.OutputLimit = output
				}
				break
			}
		}
	}
}

func positiveUsageAuditLimit(value *int64) *int64 {
	if value == nil || *value <= 0 {
		return nil
	}
	copy := *value
	return &copy
}

// parsePricingTokenLimit rejects missing/non-integral/nonpositive limits while
// allowing the unrelated price fields of that entry to remain usable.
func parsePricingTokenLimit(raw json.RawMessage) *int64 {
	var value int64
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value <= 0 {
		return nil
	}
	return &value
}

func sumUsageAuditTokens(values ...int64) (int64, bool) {
	var total int64
	for _, value := range values {
		if value < 0 || total > math.MaxInt64-value {
			return 0, false
		}
		total += value
	}
	return total, true
}
