package service

import "maps"

// SanitizeUsageLogForRecovery explicitly copies only persisted usage fields.
// Never copy relation objects: they can carry API keys, upstream credentials,
// customer profile data and other material unrelated to the usage record.
// Keep this allowlist in sync with prepareUsageLogInsert when adding display
// fields. Pointer/map values are copied so a later failure placeholder or caller
// mutation cannot change the already prepared successful billing snapshot.
func SanitizeUsageLogForRecovery(log *UsageLog) *UsageLog {
	if log == nil {
		return nil
	}
	return &UsageLog{
		UserID: log.UserID, APIKeyID: log.APIKeyID, AccountID: log.AccountID,
		RequestID: log.RequestID, Model: log.Model, RequestedModel: log.RequestedModel,
		UpstreamModel:         copyUsageValue(log.UpstreamModel),
		UpstreamResponseModel: copyUsageValue(log.UpstreamResponseModel),
		UpstreamModelMismatch: copyUsageValue(log.UpstreamModelMismatch),
		ChannelID:             copyUsageValue(log.ChannelID), ModelMappingChain: copyUsageValue(log.ModelMappingChain),
		BillingTier: copyUsageValue(log.BillingTier), BillingMode: copyUsageValue(log.BillingMode),
		ServiceTier: copyUsageValue(log.ServiceTier), ReasoningEffort: copyUsageValue(log.ReasoningEffort),
		RequestedReasoningEffort: copyUsageValue(log.RequestedReasoningEffort),
		InboundEndpoint:          copyUsageValue(log.InboundEndpoint), UpstreamEndpoint: copyUsageValue(log.UpstreamEndpoint),
		GroupID: copyUsageValue(log.GroupID), SubscriptionID: copyUsageValue(log.SubscriptionID),
		InputTokens: log.InputTokens, OutputTokens: log.OutputTokens,
		CacheCreationTokens: log.CacheCreationTokens, CacheReadTokens: log.CacheReadTokens,
		CacheCreation5mTokens: log.CacheCreation5mTokens, CacheCreation1hTokens: log.CacheCreation1hTokens,
		ImageInputTokens: log.ImageInputTokens, ImageInputCost: log.ImageInputCost,
		ImageOutputTokens: log.ImageOutputTokens, ImageOutputCost: log.ImageOutputCost,
		InputCost: log.InputCost, OutputCost: log.OutputCost,
		CacheCreationCost: log.CacheCreationCost, CacheReadCost: log.CacheReadCost,
		TotalCost: log.TotalCost, ActualCost: log.ActualCost, RateMultiplier: log.RateMultiplier,
		LongContextBillingApplied: log.LongContextBillingApplied,
		AccountRateMultiplier:     copyUsageValue(log.AccountRateMultiplier), AccountStatsCost: copyUsageValue(log.AccountStatsCost),
		BillingType: log.BillingType, RequestType: log.RequestType,
		Stream: log.Stream, OpenAIWSMode: log.OpenAIWSMode, NativeCompactionV2: log.NativeCompactionV2,
		DurationMs: copyUsageValue(log.DurationMs), FirstTokenMs: copyUsageValue(log.FirstTokenMs),
		StreamingAckMs: copyUsageValue(log.StreamingAckMs), UserAgent: copyUsageValue(log.UserAgent),
		IPAddress: copyUsageValue(log.IPAddress), SessionID: copyUsageValue(log.SessionID),
		UpstreamRequestID: copyUsageValue(log.UpstreamRequestID), CacheTTLOverridden: log.CacheTTLOverridden,
		ImageCount: log.ImageCount, ImageSize: copyUsageValue(log.ImageSize),
		ImageInputSize: copyUsageValue(log.ImageInputSize), ImageOutputSize: copyUsageValue(log.ImageOutputSize),
		ImageSizeSource: copyUsageValue(log.ImageSizeSource), ImageSizeBreakdown: maps.Clone(log.ImageSizeBreakdown),
		MediaType: copyUsageValue(log.MediaType), VideoCount: log.VideoCount,
		VideoResolution: copyUsageValue(log.VideoResolution), VideoDurationSeconds: copyUsageValue(log.VideoDurationSeconds),
		CreatedAt: log.CreatedAt,
	}
}

func copyUsageValue[T any](v *T) *T {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
