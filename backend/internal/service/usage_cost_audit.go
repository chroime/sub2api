package service

import (
	"context"
	"math"
	"strings"
	"time"
)

// UsageCostAudit describes the charge for the stored upstream token buckets
// with customer token adjustments removed. It is neither an upstream invoice
// nor AccountStatsCost. TotalCost is the unamplified charge for the recorded
// upstream token buckets; Amount is retained as the rate-adjusted value for
// compatibility with existing audit consumers. BilledAmount is the charge
// recorded for the user and Difference is BilledAmount - TotalCost.
type UsageCostAudit struct {
	Amount       *float64       `json:"amount"`
	TotalCost    *float64       `json:"total_cost"`
	BilledAmount *float64       `json:"billed_amount"`
	Difference   *float64       `json:"difference"`
	Status       string         `json:"status"`
	Reason       string         `json:"reason"`
	Model        string         `json:"model,omitempty"`
	Breakdown    *CostBreakdown `json:"-"`
}

// UsageCostAuditService is a read-only calculator. It does not alter usage
// records, debit balances, or contact an upstream model provider.
type UsageCostAuditService struct {
	billing        *BillingService
	resolver       *ModelPricingResolver
	groupRepo      GroupRepository
	channelService *ChannelService
}

func NewUsageCostAuditService(billing *BillingService, resolver *ModelPricingResolver, groupRepo GroupRepository, channelService *ChannelService) *UsageCostAuditService {
	if resolver == nil && billing != nil {
		resolver = NewModelPricingResolver(channelService, billing)
	}
	if channelService == nil && resolver != nil {
		channelService = resolver.channelService
	}
	return &UsageCostAuditService{billing: billing, resolver: resolver, groupRepo: groupRepo, channelService: channelService}
}

type usageCostAuditGroup struct {
	group   *Group
	channel *Channel
	reason  string
}

// EstimateBatch keeps both successful and failed group lookups within this
// request. GetByIDLite includes price cards without loading account counts.
func (s *UsageCostAuditService) EstimateBatch(ctx context.Context, logs []UsageLog) map[int64]UsageCostAudit {
	if ctx == nil {
		ctx = context.Background()
	}
	groups := make(map[int64]usageCostAuditGroup)
	results := make(map[int64]UsageCostAudit, len(logs))
	for i := range logs {
		results[logs[i].ID] = s.estimate(ctx, &logs[i], groups)
	}
	return results
}

func (s *UsageCostAuditService) estimate(ctx context.Context, row *UsageLog, groups map[int64]usageCostAuditGroup) UsageCostAudit {
	if !usageCostAuditValidTokens(row) {
		return unavailableUsageCostAudit("invalid_usage")
	}
	// Zero is a supported free rate in BillingService; retain it verbatim.
	if !usageCostAuditFinite(row.RateMultiplier) || row.RateMultiplier < 0 {
		return unavailableUsageCostAudit("invalid_rate_multiplier")
	}
	if usageCostAuditUnsupported(row) {
		return unavailableUsageCostAudit("unsupported_billing_mode")
	}
	// These non-null columns defaulted to zero when introduced. Without at
	// least one positive raw count there is no evidence they were populated.
	if row.UpstreamInputTokens == 0 && row.UpstreamOutputTokens == 0 ||
		row.UpstreamInputTokens == 0 && row.InputTokens > 0 ||
		row.UpstreamOutputTokens == 0 && row.OutputTokens > 0 {
		return unavailableUsageCostAudit("missing_upstream_usage")
	}
	for _, amount := range []float64{row.InputCost, row.OutputCost, row.CacheCreationCost, row.CacheReadCost, row.TotalCost, row.ActualCost} {
		if !usageCostAuditFinite(amount) || amount < 0 {
			return unavailableUsageCostAudit("invalid_usage")
		}
	}
	// Search surcharges are added to TotalCost without storing their units or
	// a separate cost field. Repricing only the tokens would silently omit them.
	componentTotal := row.InputCost + row.OutputCost + row.CacheCreationCost + row.CacheReadCost
	if !usageCostAuditAmountsEqual(componentTotal, row.TotalCost) {
		return unavailableUsageCostAudit("incomplete_cost_breakdown")
	}
	if usageCostAuditOriginalBillUsable(row) {
		amount, total, billed := row.ActualCost, row.TotalCost, row.ActualCost
		difference := billed - total
		return UsageCostAudit{
			Amount: &amount, TotalCost: &total, BilledAmount: &billed, Difference: &difference,
			Status: "recorded", Reason: "original_usage_unadjusted",
		}
	}
	if !usageCostAuditRawCacheAvailable(row) {
		return unavailableUsageCostAudit("missing_upstream_cache_usage")
	}
	if s == nil || s.billing == nil || s.resolver == nil || s.resolver.billingService == nil {
		return unavailableUsageCostAudit("pricing_unavailable")
	}
	group := s.loadGroup(ctx, row, groups)
	if group.reason != "" {
		return unavailableUsageCostAudit(group.reason)
	}
	models, reason := usageCostAuditModels(row, group.channel)
	if reason != "" {
		return unavailableUsageCostAudit(reason)
	}
	// OpenAI bills credential shadows with their parent's account policy. The
	// usage row only hydrates the shadow, so its own extra cannot establish it.
	if row.Account != nil && row.Account.IsOpenAI() && row.Account.IsShadow() {
		return unavailableUsageCostAudit("pricing_unavailable")
	}
	longContextBillingGate := openAILongContextBillingGate(row.Account)
	tokens := UsageTokens{
		InputTokens: row.UpstreamInputTokens, OutputTokens: row.UpstreamOutputTokens,
		CacheReadTokens: row.AuditCacheReadTokens(), CacheCreationTokens: row.AuditCacheCreationTokens(),
		CacheCreation5mTokens: row.AuditCacheCreation5mTokens(), CacheCreation1hTokens: row.AuditCacheCreation1hTokens(),
	}
	pricingAt := row.CreatedAt
	if pricingAt.IsZero() {
		pricingAt = time.Now()
	}
	for _, model := range models {
		// Resolve's general channel lookup can normalize unknown GPT-family
		// names to an older model. Auditing requires a positively identified
		// catalog model or a directly matching operator price card first.
		if !s.billing.HasIdentifiedTokenPricing(model) && matchGroupModelPricing(group.group, model) == nil &&
			(s.channelService == nil || row.GroupID == nil || s.channelService.GetChannelModelPricing(ctx, *row.GroupID, model) == nil) {
			continue
		}
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: row.GroupID, Group: group.group})
		if resolved == nil {
			continue
		}
		if resolved.Mode != "" && resolved.Mode != BillingModeToken {
			return unavailableUsageCostAudit("unsupported_billing_mode")
		}
		if !s.usablePricing(model, resolved, tokens, longContextBillingGate) {
			continue
		}
		cost, err := s.billing.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: model, GroupID: row.GroupID, Group: group.group,
			Tokens: tokens, RateMultiplier: row.RateMultiplier, PricingAt: pricingAt,
			ServiceTier: optionalStringValue(row.ServiceTier), ReasoningEffort: optionalStringValue(row.ReasoningEffort),
			Resolver: s.resolver, Resolved: resolved,
			LongContextBillingEnabled: longContextBillingGate,
		})
		if err != nil || cost == nil {
			continue
		}
		for _, amount := range []float64{cost.InputCost, cost.OutputCost, cost.CacheCreationCost, cost.CacheReadCost, cost.TotalCost, cost.ActualCost} {
			if !usageCostAuditFinite(amount) || amount < 0 {
				return unavailableUsageCostAudit("pricing_unavailable")
			}
		}
		amount, total, billed := cost.ActualCost, cost.TotalCost, row.ActualCost
		difference := billed - total
		return UsageCostAudit{
			Amount: &amount, TotalCost: &total, BilledAmount: &billed, Difference: &difference,
			Status: "calculated", Reason: "current_pricing_calculation", Model: model, Breakdown: cost,
		}
	}
	return unavailableUsageCostAudit("pricing_unavailable")
}

func (s *UsageCostAuditService) loadGroup(ctx context.Context, row *UsageLog, groups map[int64]usageCostAuditGroup) usageCostAuditGroup {
	id := row.GroupID
	if id == nil {
		return usageCostAuditGroup{}
	}
	if cached, ok := groups[*id]; ok {
		return cached
	}
	result := usageCostAuditGroup{}
	if *id <= 0 {
		result.reason = "group_unavailable"
	} else {
		// Usage-log queries hydrate a complete groupEntityToService value.
		// Reuse it instead of serially reloading every group during exports.
		group := row.Group
		var err error
		if group == nil || group.ID != *id {
			group = nil
			if s.groupRepo != nil {
				group, err = s.groupRepo.GetByIDLite(ctx, *id)
			}
		}
		if err != nil || group == nil || group.ID != *id {
			result.reason = "group_unavailable"
		} else {
			result.group = group
			if s.channelService != nil {
				result.channel, err = s.channelService.GetChannelForGroup(ctx, *id)
				if err != nil {
					result.reason = "pricing_unavailable"
				}
			}
		}
	}
	groups[*id] = result
	return result
}

// The log has no effective-billing-model snapshot. Prefer its logged model
// when no source override is present; use the historical mapping chain for a
// channel override. An ambiguous response-model adoption cannot be replayed.
func usageCostAuditModels(row *UsageLog, channel *Channel) ([]string, string) {
	model := strings.TrimSpace(row.Model)
	requested := strings.TrimSpace(row.RequestedModel)
	if requested == "" {
		requested = model
	}
	upstream := optionalStringValue(row.UpstreamModel)
	if row.ChannelID != nil && (channel == nil || channel.ID != *row.ChannelID) {
		return nil, "billing_model_unavailable"
	}
	if channel != nil {
		switch channel.BillingModelSource {
		case BillingModelSourceRequested:
			model = requested
		case BillingModelSourceUpstream:
			// OpenAI may select a mapped BillingModel that was not persisted;
			// distinct model names cannot establish that historical choice.
			if upstream != "" && !strings.EqualFold(model, upstream) {
				return nil, "billing_model_unavailable"
			}
		case "", BillingModelSourceChannelMapped:
			chain := optionalStringValue(row.ModelMappingChain)
			parts := strings.Split(chain, "→")
			if len(parts) == 3 && strings.TrimSpace(parts[0]) == requested {
				model = strings.TrimSpace(parts[1])
			} else if len(parts) > 1 && !strings.EqualFold(model, strings.TrimSpace(parts[1])) {
				return nil, "billing_model_unavailable"
			}
		case BillingModelSourceResponse:
			if response := optionalStringValue(row.UpstreamResponseModel); response != "" && !strings.EqualFold(response, model) {
				return nil, "billing_model_unavailable"
			}
		default:
			return nil, "billing_model_unavailable"
		}
	}
	return usageCostAuditModelCandidates(model, upstream), ""
}

func usageCostAuditModelCandidates(models ...string) []string {
	var candidates []string
	seen := make(map[string]bool)
	add := func(model string) {
		model = strings.TrimSpace(model)
		key := strings.ToLower(model)
		if model != "" && !seen[key] {
			seen[key] = true
			candidates = append(candidates, model)
		}
	}
	for _, model := range models {
		add(model)
		// Spelling normalization and explicit alias tables are deterministic.
		// Do not use normalizeKnownOpenAICodexModel's family guesses here.
		canonical := normalizeModelNameForPricing(model)
		add(canonical)
		add(getNormalizedCodexModel(canonical))
	}
	return candidates
}

func (s *UsageCostAuditService) usablePricing(model string, resolved *ResolvedPricing, tokens UsageTokens, longContextBillingGate *bool) bool {
	contextTokens := tokens.InputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
	if !resolved.longContextPricingEnabled && (longContextBillingGate == nil || !*longContextBillingGate) {
		contextTokens = 1
	}
	pricing := s.resolver.GetIntervalPricing(resolved, contextTokens)
	if pricing == nil {
		return false
	}
	for _, price := range []float64{
		pricing.InputPricePerToken, pricing.OutputPricePerToken, pricing.CacheReadPricePerToken, pricing.CacheCreationPricePerToken,
		pricing.CacheCreation5mPrice, pricing.CacheCreation1hPrice,
		pricing.InputPricePerTokenPriority, pricing.OutputPricePerTokenPriority, pricing.CacheReadPricePerTokenPriority, pricing.CacheCreationPricePerTokenPriority,
	} {
		if !usageCostAuditFinite(price) || price < 0 {
			return false
		}
	}
	if s.billing.HasIdentifiedTokenPricing(model) {
		return true
	}
	// A custom model with no catalog entry needs explicit prices for its used
	// buckets. An empty card must not turn missing prices into a free estimate.
	card := resolved.channelPricing
	if card == nil {
		return false
	}
	input, output, read, write, write1h := card.InputPrice, card.OutputPrice, card.CacheReadPrice, card.CacheWritePrice, card.CacheWrite1hPrice
	if interval := FindMatchingInterval(resolved.Intervals, contextTokens); interval != nil {
		if interval.InputPrice != nil {
			input = interval.InputPrice
		}
		if interval.OutputPrice != nil {
			output = interval.OutputPrice
		}
		if interval.CacheReadPrice != nil {
			read = interval.CacheReadPrice
		}
		if interval.CacheWritePrice != nil {
			write = interval.CacheWritePrice
		}
		if interval.CacheWrite1hPrice != nil {
			write1h = interval.CacheWrite1hPrice
		}
	}
	return (tokens.InputTokens == 0 || input != nil) && (tokens.OutputTokens == 0 || output != nil) &&
		(tokens.CacheReadTokens == 0 || read != nil) && (tokens.CacheCreationTokens == 0 || write != nil) &&
		(tokens.CacheCreation1hTokens == 0 || write1h != nil || write != nil)
}

func usageCostAuditValidTokens(row *UsageLog) bool {
	const maxSafeInteger = uint64(1<<53 - 1)
	cacheRead := row.AuditCacheReadTokens()
	cacheCreation := row.AuditCacheCreationTokens()
	cacheCreation5m := row.AuditCacheCreation5mTokens()
	cacheCreation1h := row.AuditCacheCreation1hTokens()
	for _, tokens := range []int{row.InputTokens, row.OutputTokens, row.UpstreamInputTokens, row.UpstreamOutputTokens, cacheCreation, cacheRead, cacheCreation5m, cacheCreation1h} {
		if tokens < 0 || uint64(tokens) > maxSafeInteger {
			return false
		}
	}
	// Check before adding: both the pricing engine's int context sum and JSON
	// consumers' exact integer range must remain safe.
	var total uint64
	for _, count := range []int{row.UpstreamInputTokens, row.UpstreamOutputTokens, cacheCreation, cacheRead} {
		total += uint64(count)
		if total > maxSafeInteger || total > uint64(maxIntValue()) {
			return false
		}
	}
	detail := uint64(cacheCreation5m) + uint64(cacheCreation1h)
	// Partial or contradictory TTL breakdowns cannot safely be repaired into
	// original usage. An absent breakdown is the established aggregate path.
	return detail == 0 || detail == uint64(cacheCreation)
}

func usageCostAuditUnsupported(row *UsageLog) bool {
	mode := strings.ToLower(optionalStringValue(row.BillingMode))
	if mode != "" && mode != string(BillingModeToken) || row.BillingTier != nil ||
		row.ImageCount != 0 || row.VideoCount != 0 || row.ImageInputTokens != 0 || row.ImageOutputTokens != 0 ||
		row.ImageInputCost != 0 || row.ImageOutputCost != 0 || len(row.ImageSizeBreakdown) != 0 ||
		row.CacheTTLOverridden || row.RequestType == RequestTypeCyberBlocked || row.RequestType == RequestTypeLive {
		return true
	}
	if media := strings.ToLower(optionalStringValue(row.MediaType)); media != "" && media != "text" {
		return true
	}
	for _, value := range []string{row.Model, row.RequestedModel, optionalStringValue(row.UpstreamModel), optionalStringValue(row.InboundEndpoint), optionalStringValue(row.UpstreamEndpoint)} {
		value = strings.ToLower(value)
		for _, marker := range []string{"image", "video", "audio", "speech", "realtime", "search", "whisper", "tts"} {
			if strings.Contains(value, marker) {
				return true
			}
		}
	}
	return false
}

func usageCostAuditOriginalBillUsable(row *UsageLog) bool {
	if row.InputTokens != row.UpstreamInputTokens || row.OutputTokens != row.UpstreamOutputTokens || row.TotalCost <= 0 ||
		!usageCostAuditAmountsEqual(row.ActualCost, row.TotalCost*row.RateMultiplier) {
		return false
	}
	if row.hasPersistedUpstreamCache() &&
		(row.CacheReadTokens != row.UpstreamCacheReadTokens || row.CacheCreationTokens != row.UpstreamCacheCreationTokens) {
		return false
	}
	if row.InputTokens == 0 && row.InputCost != 0 || row.OutputTokens == 0 && row.OutputCost != 0 ||
		row.CacheReadTokens == 0 && row.CacheReadCost != 0 || row.CacheCreationTokens == 0 && row.CacheCreationCost != 0 {
		return false
	}
	// A non-1 output adjustment can fold cache into billed input even when
	// displayed non-cache counts happen to match through zeroes or rounding.
	// Positive separate cache charges establish that folding did not happen.
	return (row.CacheReadTokens == 0 || row.CacheReadCost > 0) &&
		(row.CacheCreationTokens == 0 || row.CacheCreationCost > 0)
}

// usageCostAuditRawCacheAvailable prevents adjusted cache counts from being
// mistaken for upstream counts. A non-zero billed cache bucket without any
// persisted upstream bucket is ambiguous, so the audit must remain unavailable
// instead of producing a misleading price.
func usageCostAuditRawCacheAvailable(row *UsageLog) bool {
	if row == nil || row.hasPersistedUpstreamCache() {
		return true
	}
	return row.CacheReadTokens == 0 && row.CacheCreationTokens == 0 &&
		row.CacheCreation5mTokens == 0 && row.CacheCreation1hTokens == 0
}

func usageCostAuditFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func usageCostAuditAmountsEqual(a, b float64) bool {
	// Four component columns and their total are rounded independently to
	// NUMERIC(20,10). Tolerate only that precision plus float representation.
	return usageCostAuditFinite(a) && math.Abs(a-b) <= 3e-10+math.Max(math.Abs(a), math.Abs(b))*1e-12
}

func unavailableUsageCostAudit(reason string) UsageCostAudit {
	return UsageCostAudit{Status: "unavailable", Reason: reason}
}
