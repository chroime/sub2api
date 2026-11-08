package service

import (
	"context"
	"errors"
)

// WrapGroupInputRoutingOpenAIResult exposes the fields used by the common HTTP
// handlers while retaining the native result for OpenAI billing. In particular,
// the generic usage projection must never replace the original image/cache buckets.
func WrapGroupInputRoutingOpenAIResult(result *OpenAIForwardResult) *ForwardResult {
	if result == nil {
		return nil
	}
	return &ForwardResult{
		groupInputRoutingOpenAI: result,
		RequestID:               result.RequestID,
		UpstreamHeaders:         result.UpstreamHeaders,
		Usage: ClaudeUsage{
			InputTokens:              max(result.Usage.InputTokens-result.Usage.CacheReadInputTokens-result.Usage.CacheCreationInputTokens, 0),
			OutputTokens:             result.Usage.OutputTokens,
			CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
			ImageOutputTokens:        result.Usage.ImageOutputTokens,
		},
		Model:                         result.Model,
		UpstreamModel:                 result.UpstreamModel,
		UpstreamResponseModel:         result.UpstreamResponseModel,
		UpstreamResponseModelConflict: result.UpstreamResponseModelConflict,
		UpstreamResponseServiceTier:   result.UpstreamResponseServiceTier,
		Stream:                        result.Stream,
		Duration:                      result.Duration,
		FirstTokenMs:                  result.FirstTokenMs,
		StreamingAckMs:                result.StreamingAckMs,
		ClientDisconnect:              result.ClientDisconnect,
		ReasoningEffort:               result.ReasoningEffort,
		RequestedReasoningEffort:      result.RequestedReasoningEffort,
		ServiceTier:                   result.ServiceTier,
		ImageCount:                    result.ImageCount,
		ImageEndpoint:                 result.ImageEndpoint,
		ImageSize:                     result.ImageSize,
		ImageInputSize:                result.ImageInputSize,
		ImageOutputSize:               result.ImageOutputSize,
		ImageOutputSizes:              result.ImageOutputSizes,
		ImageSizeSource:               result.ImageSizeSource,
		ImageSizeBreakdown:            result.ImageSizeBreakdown,
		SearchCount:                   result.SearchCount,
		AudioUsage:                    result.AudioUsage,
	}
}

func groupInputRoutingOpenAIUsageInput(input *RecordUsageInput) *OpenAIRecordUsageInput {
	if input == nil || input.Result == nil || input.Result.groupInputRoutingOpenAI == nil {
		return nil
	}
	wrapped := input.Result
	// Usage recording can resolve tiers and image billing in place. Work on a
	// copy so recording does not mutate the forwarder's original result.
	result := *wrapped.groupInputRoutingOpenAI
	result.RequestID = wrapped.RequestID
	result.UpstreamHeaders = wrapped.UpstreamHeaders
	result.Model = wrapped.Model
	result.UpstreamModel = wrapped.UpstreamModel
	result.UpstreamResponseModel = wrapped.UpstreamResponseModel
	result.UpstreamResponseModelConflict = wrapped.UpstreamResponseModelConflict
	result.UpstreamResponseServiceTier = wrapped.UpstreamResponseServiceTier
	result.Stream = wrapped.Stream
	result.Duration = wrapped.Duration
	result.FirstTokenMs = wrapped.FirstTokenMs
	result.StreamingAckMs = wrapped.StreamingAckMs
	result.ClientDisconnect = wrapped.ClientDisconnect
	result.ReasoningEffort = wrapped.ReasoningEffort
	result.RequestedReasoningEffort = wrapped.RequestedReasoningEffort
	result.ServiceTier = wrapped.ServiceTier
	return &OpenAIRecordUsageInput{
		Result:             &result,
		APIKey:             input.APIKey,
		User:               input.User,
		Account:            input.Account,
		Subscription:       input.Subscription,
		PricingAt:          input.PricingAt,
		InboundEndpoint:    input.InboundEndpoint,
		UpstreamEndpoint:   input.UpstreamEndpoint,
		UserAgent:          input.UserAgent,
		IPAddress:          input.IPAddress,
		SessionID:          input.SessionID,
		RequestPayloadHash: input.RequestPayloadHash,
		APIKeyService:      input.APIKeyService,
		QuotaPlatform:      input.QuotaPlatform,
		ChannelUsageFields: input.ChannelUsageFields,
	}
}

// RecordGroupInputRoutingUsage uses the selected forwarder's native billing
// implementation while keeping the source API key, group, subscription and prices.
func RecordGroupInputRoutingUsage(ctx context.Context, gateway *GatewayService, openai *OpenAIGatewayService, input *RecordUsageInput) error {
	if input == nil || input.Result == nil {
		return errors.New("group input routing usage result is nil")
	}
	if native := groupInputRoutingOpenAIUsageInput(input); native != nil {
		if openai == nil {
			return errors.New("group input routing OpenAI usage service is not configured")
		}
		// ForceCacheBilling is specific to the generic gateway. Native OpenAI
		// already accounts for mutually exclusive upstream input/cache buckets.
		return openai.RecordUsage(ctx, native)
	}
	if gateway == nil {
		return errors.New("group input routing usage service is not configured")
	}
	return gateway.RecordUsage(ctx, input)
}
