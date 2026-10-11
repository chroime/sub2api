package service

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
)

// downstreamUsageAdjustmentContextKey keeps the downstream wire projection
// policy in the request context without making the service depend on the HTTP
// middleware package. The raw upstream usage used by billing and administrator
// reporting never passes through this view.
type downstreamUsageAdjustmentContextKey struct{}

// WithDownstreamUsageAdjustment attaches the group token adjustment used when
// serializing a response back to an API-key client.
func WithDownstreamUsageAdjustment(ctx context.Context, adjustment TokenUsageAdjustment) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downstreamUsageAdjustmentContextKey{}, adjustment.normalized())
}

// DownstreamUsageAdjustmentFromContext returns the adjustment attached to a
// request, if any.
func DownstreamUsageAdjustmentFromContext(ctx context.Context) (TokenUsageAdjustment, bool) {
	if ctx == nil {
		return TokenUsageAdjustment{}, false
	}
	adjustment, ok := ctx.Value(downstreamUsageAdjustmentContextKey{}).(TokenUsageAdjustment)
	if !ok {
		return TokenUsageAdjustment{}, false
	}
	return adjustment.normalized(), true
}

// TokenUsageAdjustmentForGroup exposes the same normalized group policy used
// by internal billing to the ingress middleware. Keeping one constructor avoids
// wire usage and customer billing drifting apart.
func TokenUsageAdjustmentForGroup(group *Group) TokenUsageAdjustment {
	return tokenUsageAdjustmentForGroup(group)
}

// ProjectDownstreamUsageJSON rewrites every response usage object in a JSON
// document into the customer-facing billing view. It is intentionally a copy
// of the wire representation: callers retain the original response bytes and
// OpenAIForwardResult.Usage for upstream cost accounting and administrator
// audit. The function returns the original slice and false when the document
// has no recognized usage object or the configured threshold does not apply.
func ProjectDownstreamUsageJSON(raw []byte, adjustment TokenUsageAdjustment) ([]byte, bool) {
	return projectDownstreamUsageJSON(raw, adjustment, nil)
}

// ProjectDownstreamUsageJSONWithEligibility is the streaming variant of
// ProjectDownstreamUsageJSON. Anthropic sends input usage in message_start and
// output usage later in message_delta; eligible carries the threshold decision
// from the first frame to the output-only frame.
func ProjectDownstreamUsageJSONWithEligibility(raw []byte, adjustment TokenUsageAdjustment, eligible *bool) ([]byte, bool) {
	return projectDownstreamUsageJSON(raw, adjustment, eligible)
}

func projectDownstreamUsageJSON(raw []byte, adjustment TokenUsageAdjustment, eligible *bool) ([]byte, bool) {
	if len(raw) == 0 || !downstreamUsageAdjustmentEnabled(adjustment) {
		return raw, false
	}
	// Most streaming frames carry deltas only. Avoid JSON decoding and map
	// allocation unless the frame can actually contain a usage object.
	if !bytes.Contains(raw, []byte(`"usage"`)) {
		return raw, false
	}

	var document any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return raw, false
	}
	if !projectDownstreamUsageValue(document, adjustment.normalized(), eligible) {
		return raw, false
	}
	projected, err := json.Marshal(document)
	if err != nil {
		return raw, false
	}
	return projected, true
}

func downstreamUsageAdjustmentEnabled(adjustment TokenUsageAdjustment) bool {
	adjustment = adjustment.normalized()
	return adjustment.UnifiedMultiplier != 1 || adjustment.InputMultiplier != 1 || adjustment.OutputMultiplier != 1
}

func downstreamCacheScaleMultiplier(adjustment TokenUsageAdjustment) float64 {
	adjustment = adjustment.normalized()
	if adjustment.unifiedEnabled() {
		return adjustment.UnifiedMultiplier
	}
	return adjustment.InputMultiplier
}

func downstreamOutputScaleMultiplier(adjustment TokenUsageAdjustment) float64 {
	adjustment = adjustment.normalized()
	if adjustment.unifiedEnabled() {
		return adjustment.UnifiedMultiplier
	}
	return adjustment.OutputMultiplier
}

func projectDownstreamUsageValue(value any, adjustment TokenUsageAdjustment, eligible *bool) bool {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		if usage, ok := typed["usage"].(map[string]any); ok {
			changed = projectDownstreamUsageMap(usage, adjustment, eligible) || changed
		}
		for key, child := range typed {
			if key == "usage" {
				continue
			}
			switch key {
			case "response", "message", "data", "output", "item", "choices":
				changed = projectDownstreamUsageValue(child, adjustment, eligible) || changed
			}
		}
		return changed
	case []any:
		changed := false
		for _, item := range typed {
			changed = projectDownstreamUsageValue(item, adjustment, eligible) || changed
		}
		return changed
	default:
		return false
	}
}

func projectDownstreamUsageMap(usage map[string]any, adjustment TokenUsageAdjustment, eligible *bool) bool {
	_, hasPrompt := usage["prompt_tokens"]
	_, hasInput := usage["input_tokens"]
	_, hasOutput := usage["output_tokens"]
	if hasPrompt && hasInput {
		return projectMixedUsageMap(usage, adjustment, eligible)
	}
	if hasPrompt {
		return projectOpenAIUsageMap(usage, adjustment, "prompt_tokens", "completion_tokens", "prompt_tokens_details", eligible)
	}
	if hasInput {
		return projectInputOutputUsageMap(usage, adjustment, eligible)
	}
	if hasOutput && !hasPrompt && !hasInput {
		return projectInputOutputUsageMap(usage, adjustment, eligible)
	}
	return false
}

// projectMixedUsageMap handles compatibility responses that expose both
// OpenAI aliases (prompt/completion) and native input/output fields. The
// aliases must share one canonical input/output decision: running the two
// projections independently can apply the minimum-input threshold to two
// different raw values and leave the response internally inconsistent.
func projectMixedUsageMap(usage map[string]any, adjustment TokenUsageAdjustment, eligible *bool) bool {
	rawPrompt, promptOK := jsonInteger(usage["prompt_tokens"])
	rawInput, inputOK := jsonInteger(usage["input_tokens"])
	rawCompletion, completionOK := jsonInteger(usage["completion_tokens"])
	rawOutput, outputOK := jsonInteger(usage["output_tokens"])
	if !promptOK && !inputOK && !completionOK && !outputOK {
		return false
	}

	cacheRead, cacheWrite := mixedUsageCacheBuckets(usage)
	canonicalTotalInput := rawPrompt
	if canonicalTotalInput <= 0 {
		canonicalTotalInput = max(rawInput, 0)
	}
	rawNonCache := max(canonicalTotalInput-cacheRead-cacheWrite, 0)
	canonicalOutput := rawOutput
	if completionOK {
		canonicalOutput = rawCompletion
	} else if !outputOK {
		canonicalOutput = 0
	}

	projectedInput, projectedOutput, projectedRead, projectedWrite, applied := projectUsageBuckets(
		adjustment, rawNonCache, cacheRead, cacheWrite, canonicalOutput,
	)
	if !applied {
		if eligible != nil {
			*eligible = false
		}
		return false
	}
	if eligible != nil {
		*eligible = true
	}

	// prompt_tokens is the total input alias. Native Anthropic input_tokens
	// excludes cache buckets, while an OpenAI-compatible input_tokens alias
	// includes them. A positive native cache bucket without total_tokens is the
	// only mixed-shape signal available here, so preserve that native meaning.
	nativeInput := hasNativeAnthropicCacheFields(usage) && !hasUsageField(usage, "total_tokens")
	projectedNonCache := projectedInput - projectedRead - projectedWrite
	if projectedNonCache < 0 {
		projectedNonCache = 0
	}
	if promptOK {
		usage["prompt_tokens"] = projectedInput
	}
	if inputOK {
		if nativeInput {
			usage["input_tokens"] = projectedNonCache
		} else {
			usage["input_tokens"] = projectedInput
		}
	}
	if completionOK {
		usage["completion_tokens"] = projectedOutput
	}
	if outputOK {
		usage["output_tokens"] = projectedOutput
	}
	if promptOK || inputOK || completionOK || outputOK || hasUsageField(usage, "total_tokens") {
		usage["total_tokens"] = sumTokenCounts(projectedInput, projectedOutput)
	}
	updateUsageCacheFields(usage, "prompt_tokens_details", projectedRead, projectedWrite, downstreamCacheScaleMultiplier(adjustment))
	// Top-level cache_creation was already updated by the first call. Updating
	// only the second details object avoids multiplying the same nested buckets
	// a second time when both details aliases are present.
	updateUsageCacheDetails(usage, "input_tokens_details", projectedRead, projectedWrite, downstreamCacheScaleMultiplier(adjustment))
	return true
}

func mixedUsageCacheBuckets(usage map[string]any) (cacheRead, cacheWrite int) {
	cacheRead, cacheWrite = usageCacheBuckets(usage, "prompt_tokens_details")
	inputRead, inputWrite := usageCacheBuckets(usage, "input_tokens_details")
	return max(cacheRead, inputRead), max(cacheWrite, inputWrite)
}

func hasUsageField(usage map[string]any, key string) bool {
	_, ok := usage[key]
	return ok
}

func cloneUsageMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case map[string]any:
			cloned[key] = cloneUsageMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				if child, ok := item.(map[string]any); ok {
					items[i] = cloneUsageMap(child)
				} else {
					items[i] = item
				}
			}
			cloned[key] = items
		default:
			cloned[key] = value
		}
	}
	return cloned
}

func copyProjectedInputFields(dst, src map[string]any) {
	for _, key := range []string{
		"input_tokens", "output_tokens", "total_tokens",
		"cache_read_input_tokens", "cache_creation_input_tokens",
		"cache_read_tokens", "cache_creation_tokens", "cache_write_input_tokens",
		"cache_write_tokens", "cached_tokens", "cache_creation",
	} {
		if value, exists := src[key]; exists {
			dst[key] = value
		}
	}
}

func projectOpenAIUsageMap(usage map[string]any, adjustment TokenUsageAdjustment, inputKey, outputKey, detailsKey string, eligible *bool) bool {
	rawInput, inputOK := jsonInteger(usage[inputKey])
	rawOutput, outputOK := jsonInteger(usage[outputKey])
	if !inputOK && !outputOK {
		return false
	}
	if !inputOK && outputOK {
		if eligible == nil || !*eligible {
			return false
		}
		projectedOutput := adjustedTokenCount(rawOutput, downstreamOutputScaleMultiplier(adjustment))
		cacheRead, cacheWrite := usageCacheBuckets(usage, detailsKey)
		projectedRead := adjustedTokenCount(cacheRead, downstreamCacheScaleMultiplier(adjustment))
		projectedWrite := adjustedTokenCount(cacheWrite, downstreamCacheScaleMultiplier(adjustment))
		changed := projectedOutput != rawOutput || projectedRead != cacheRead || projectedWrite != cacheWrite
		if !changed {
			return false
		}
		usage[outputKey] = projectedOutput
		updateUsageCacheFields(usage, detailsKey, projectedRead, projectedWrite, downstreamCacheScaleMultiplier(adjustment))
		return true
	}

	cacheRead, cacheWrite := 0, 0
	if details, ok := usage[detailsKey].(map[string]any); ok {
		cacheRead = firstJSONInteger(details, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens")
		cacheWrite = firstJSONInteger(details, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens")
		nestedFive, nestedHour, _ := nestedCacheCreationBuckets(details)
		cacheWrite = max(cacheWrite, sumTokenCounts(nestedFive, nestedHour))
	}
	cacheRead = max(cacheRead, firstJSONInteger(usage, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens"))
	cacheWrite = max(cacheWrite, firstJSONInteger(usage, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens", "cache_write_input_tokens"))
	nestedFive, nestedHour, _ := nestedCacheCreationBuckets(usage)
	cacheWrite = max(cacheWrite, sumTokenCounts(nestedFive, nestedHour))
	if rawInput < 0 {
		rawInput = 0
	}
	if rawOutput < 0 {
		rawOutput = 0
	}
	rawNonCache := max(rawInput-cacheRead-cacheWrite, 0)
	projectedInput, projectedOutput, projectedRead, projectedWrite, applied := projectUsageBuckets(
		adjustment, rawNonCache, cacheRead, cacheWrite, rawOutput,
	)
	if !applied {
		if eligible != nil {
			*eligible = false
		}
		return false
	}
	if eligible != nil {
		*eligible = true
	}
	if inputOK {
		usage[inputKey] = projectedInput
	}
	if outputOK {
		usage[outputKey] = projectedOutput
	}
	if _, exists := usage["total_tokens"]; exists || inputOK || outputOK {
		usage["total_tokens"] = sumTokenCounts(projectedInput, projectedOutput)
	}
	updateUsageCacheFields(usage, detailsKey, projectedRead, projectedWrite, downstreamCacheScaleMultiplier(adjustment))
	return true
}

func projectInputOutputUsageMap(usage map[string]any, adjustment TokenUsageAdjustment, eligible *bool) bool {
	rawInput, inputOK := jsonInteger(usage["input_tokens"])
	rawOutput, outputOK := jsonInteger(usage["output_tokens"])
	if !inputOK && !outputOK {
		return false
	}
	if !inputOK && outputOK {
		if eligible == nil || !*eligible {
			return false
		}
		projectedOutput := adjustedTokenCount(max(rawOutput, 0), downstreamOutputScaleMultiplier(adjustment))
		cacheRead, cacheWrite := usageCacheBuckets(usage, "input_tokens_details")
		projectedRead := adjustedTokenCount(cacheRead, downstreamCacheScaleMultiplier(adjustment))
		projectedWrite := adjustedTokenCount(cacheWrite, downstreamCacheScaleMultiplier(adjustment))
		changed := projectedOutput != max(rawOutput, 0) || projectedRead != cacheRead || projectedWrite != cacheWrite
		if !changed {
			return false
		}
		usage["output_tokens"] = projectedOutput
		updateUsageCacheFields(usage, "input_tokens_details", projectedRead, projectedWrite, downstreamCacheScaleMultiplier(adjustment))
		return true
	}

	// Anthropic's native input_tokens excludes cache buckets. OpenAI Responses
	// includes cache reads in input_tokens and exposes them under details. The
	// presence of native top-level cache fields disambiguates the two shapes.
	cacheRead := firstJSONInteger(usage, "cache_read_input_tokens", "cache_read_tokens")
	cacheWrite := firstJSONInteger(usage, "cache_creation_input_tokens", "cache_creation_tokens", "cache_write_input_tokens", "cache_write_tokens")
	nestedFive, nestedHour, _ := nestedCacheCreationBuckets(usage)
	cacheWrite = max(cacheWrite, sumTokenCounts(nestedFive, nestedHour))
	detailsKey := "input_tokens_details"
	if details, ok := usage[detailsKey].(map[string]any); ok {
		cacheRead = max(cacheRead, firstJSONInteger(details, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens"))
		cacheWrite = max(cacheWrite, firstJSONInteger(details, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens"))
		nestedFive, nestedHour, _ := nestedCacheCreationBuckets(details)
		cacheWrite = max(cacheWrite, sumTokenCounts(nestedFive, nestedHour))
	}
	_, hasTotalTokens := usage["total_tokens"]
	inputIncludesCache := hasTotalTokens || !hasNativeAnthropicCacheFields(usage)
	rawNonCache := rawInput
	if inputIncludesCache {
		rawNonCache = max(rawInput-cacheRead-cacheWrite, 0)
	}
	projectedInput, projectedOutput, projectedRead, projectedWrite, applied := projectUsageBuckets(
		adjustment, rawNonCache, cacheRead, cacheWrite, max(rawOutput, 0),
	)
	if !applied {
		if eligible != nil {
			*eligible = false
		}
		return false
	}
	if eligible != nil {
		*eligible = true
	}
	if inputOK {
		if inputIncludesCache {
			usage["input_tokens"] = projectedInput
		} else {
			usage["input_tokens"] = projectedInput - projectedRead - projectedWrite
			if projectedInput < projectedRead+projectedWrite {
				usage["input_tokens"] = 0
			}
		}
	}
	if outputOK {
		usage["output_tokens"] = projectedOutput
	}
	if _, exists := usage["total_tokens"]; exists {
		usage["total_tokens"] = sumTokenCounts(projectedInput, projectedOutput)
	}
	updateUsageCacheFields(usage, detailsKey, projectedRead, projectedWrite, downstreamCacheScaleMultiplier(adjustment))
	return true
}

func sumTokenCounts(input, output int) int {
	input = max(input, 0)
	output = max(output, 0)
	if input > maxIntValue()-output {
		return maxIntValue()
	}
	return input + output
}

func hasNativeAnthropicCacheFields(usage map[string]any) bool {
	for _, key := range []string{
		"cache_read_input_tokens", "cache_creation_input_tokens",
		"cache_read_tokens", "cache_creation_tokens",
	} {
		if value, ok := jsonInteger(usage[key]); ok && value > 0 {
			return true
		}
	}
	if _, _, ok := nestedCacheCreationBuckets(usage); ok {
		return true
	}
	return false
}

func nestedCacheCreationBuckets(values map[string]any) (fiveMinute, oneHour int, ok bool) {
	creation, exists := values["cache_creation"].(map[string]any)
	if !exists {
		return 0, 0, false
	}
	fiveMinute = firstJSONInteger(creation, "ephemeral_5m_input_tokens")
	oneHour = firstJSONInteger(creation, "ephemeral_1h_input_tokens")
	return fiveMinute, oneHour, fiveMinute > 0 || oneHour > 0
}

func projectUsageBuckets(adjustment TokenUsageAdjustment, rawNonCache, cacheRead, cacheWrite, rawOutput int) (input, output, projectedRead, projectedWrite int, applied bool) {
	rawNonCache = max(rawNonCache, 0)
	cacheRead = max(cacheRead, 0)
	cacheWrite = max(cacheWrite, 0)
	rawOutput = max(rawOutput, 0)
	adjusted := adjustment.ApplyForRequestUsage(rawNonCache, cacheRead, cacheWrite, rawOutput)
	if !adjusted.Applied {
		return rawNonCache + cacheRead + cacheWrite, rawOutput, cacheRead, cacheWrite, false
	}
	if adjustment.unifiedEnabled() {
		projectedRead := adjusted.Customer.CacheReadTokens
		projectedWrite := adjusted.Customer.CacheWriteTokens
		total := adjusted.Customer.InputTokens + projectedRead + projectedWrite
		if total < adjusted.Customer.InputTokens {
			total = maxIntValue()
		}
		projectedRead, projectedWrite = fitCacheBuckets(total, projectedRead, projectedWrite)
		return total, adjusted.Customer.OutputTokens, projectedRead, projectedWrite, true
	}
	projectedRead = adjustedTokenCount(cacheRead, adjustment.normalized().InputMultiplier)
	projectedWrite = adjustedTokenCount(cacheWrite, adjustment.normalized().InputMultiplier)
	projectedRead, projectedWrite = fitCacheBuckets(adjusted.Customer.InputTokens, projectedRead, projectedWrite)
	return adjusted.Customer.InputTokens,
		adjusted.Customer.OutputTokens,
		projectedRead,
		projectedWrite,
		true
}

func updateUsageCacheFields(usage map[string]any, detailsKey string, cacheRead, cacheWrite int, inputMultiplier float64) {
	updateUsageCacheDetails(usage, detailsKey, cacheRead, cacheWrite, inputMultiplier)
	updateExistingIntegerFields(usage, cacheRead, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens")
	updateExistingIntegerFields(usage, cacheWrite, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens", "cache_write_input_tokens")
	updateNestedCacheCreation(usage, cacheWrite, inputMultiplier)
}

func updateUsageCacheDetails(usage map[string]any, detailsKey string, cacheRead, cacheWrite int, inputMultiplier float64) {
	if details, ok := usage[detailsKey].(map[string]any); ok {
		updateExistingIntegerFields(details, cacheRead, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens")
		updateExistingIntegerFields(details, cacheWrite, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens")
		updateNestedCacheCreation(details, cacheWrite, inputMultiplier)
	}
}

func usageCacheBuckets(usage map[string]any, detailsKey string) (cacheRead, cacheWrite int) {
	cacheRead = firstJSONInteger(usage, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens")
	cacheWrite = firstJSONInteger(usage, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens", "cache_write_input_tokens")
	if details, ok := usage[detailsKey].(map[string]any); ok {
		cacheRead = max(cacheRead, firstJSONInteger(details, "cached_tokens", "cache_read_tokens", "cache_read_input_tokens"))
		cacheWrite = max(cacheWrite, firstJSONInteger(details, "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens"))
		nestedFive, nestedHour, _ := nestedCacheCreationBuckets(details)
		cacheWrite = max(cacheWrite, sumTokenCounts(nestedFive, nestedHour))
	}
	nestedFive, nestedHour, _ := nestedCacheCreationBuckets(usage)
	cacheWrite = max(cacheWrite, sumTokenCounts(nestedFive, nestedHour))
	return cacheRead, cacheWrite
}

func updateNestedCacheCreation(values map[string]any, projectedWrite int, inputMultiplier float64) {
	creation, ok := values["cache_creation"].(map[string]any)
	if !ok {
		return
	}
	fiveMinute := adjustedTokenCount(firstJSONInteger(creation, "ephemeral_5m_input_tokens"), inputMultiplier)
	oneHour := adjustedTokenCount(firstJSONInteger(creation, "ephemeral_1h_input_tokens"), inputMultiplier)
	fiveMinute, oneHour = fitCacheBuckets(projectedWrite, fiveMinute, oneHour)
	updateExistingIntegerFields(creation, fiveMinute, "ephemeral_5m_input_tokens")
	updateExistingIntegerFields(creation, oneHour, "ephemeral_1h_input_tokens")
}

func updateExistingIntegerFields(values map[string]any, value int, keys ...string) {
	for _, key := range keys {
		if _, exists := values[key]; exists {
			values[key] = value
		}
	}
}

func firstJSONInteger(values map[string]any, keys ...string) int {
	found := false
	for _, key := range keys {
		if value, ok := jsonInteger(values[key]); ok {
			found = true
			if value > 0 {
				return value
			}
		}
	}
	if found {
		return 0
	}
	return 0
}

func jsonInteger(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil || parsed > int64(maxIntValue()) || parsed < 0 {
			return 0, err == nil
		}
		return int(parsed), true
	case float64:
		if typed < 0 || typed > float64(maxIntValue()) || math.Trunc(typed) != typed {
			return 0, false
		}
		return int(typed), true
	case int:
		return max(typed, 0), true
	default:
		return 0, false
	}
}
