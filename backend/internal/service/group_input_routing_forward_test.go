//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWrapGroupInputRoutingOpenAIResultPreservesPartialUsageAndMetadata(t *testing.T) {
	require.Nil(t, WrapGroupInputRoutingOpenAIResult(nil))
	first, ack, upstreamFirst := 350, 20, 325
	effort, requested, tier := "high", "max", "priority"
	present := true
	native := &OpenAIForwardResult{
		RequestID: "upstream", ResponseID: "resp_1", Model: "client-model", BillingModel: "billing-model", UpstreamModel: "mapped-model",
		UpstreamHeaders: http.Header{"X-Request-Id": []string{"req-header"}}, ResponseHeaders: http.Header{"X-Response": []string{"header"}},
		Usage:        OpenAIUsage{InputTokens: 1200, OutputTokens: 40, CacheReadInputTokens: 300, CacheCreationInputTokens: 200, ImageInputTokens: 80, ImageCacheReadTokens: 15, ImageOutputTokens: 20},
		UsagePresent: &present, UpstreamResponseModel: "actual-model", UpstreamResponseModelConflict: true, UpstreamResponseServiceTier: "default", UpstreamEndpoint: "/v1/responses",
		ServiceTier: &tier, ReasoningEffort: &effort, RequestedReasoningEffort: &requested,
		Stream: true, OpenAIWSMode: true, UpstreamTerminalEvent: "response.failed", Duration: time.Second,
		FirstTokenMs: &first, StreamingAckMs: &ack, UpstreamFirstTokenMs: &upstreamFirst, ClientDisconnect: true,
		ImageCount: 2, ImageEndpoint: true, ImageSize: "2K", ImageInputSize: "1024x1024", ImageOutputSize: "2048x2048", ImageOutputSizes: []string{"2K", "4K"}, ImageSizeSource: "upstream", ImageSizeBreakdown: map[string]int{"2K": 1, "4K": 1},
		VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 5, WebSearchCalls: 3, SearchCount: 4,
		AudioUsage: &AudioUsage{Mode: "tts", DurationOrUnits: 0.5}, wsReplayInput: []json.RawMessage{json.RawMessage(`{"text":"original"}`)}, wsReplayInputExists: true,
	}
	wrapped := WrapGroupInputRoutingOpenAIResult(native)
	require.Same(t, native, wrapped.groupInputRoutingOpenAI)
	require.Equal(t, ClaudeUsage{InputTokens: 700, OutputTokens: 40, CacheReadInputTokens: 300, CacheCreationInputTokens: 200, ImageOutputTokens: 20}, wrapped.Usage)
	require.Equal(t, native.RequestID, wrapped.RequestID)
	require.Equal(t, native.UpstreamHeaders, wrapped.UpstreamHeaders)
	require.Equal(t, native.Model, wrapped.Model)
	require.Equal(t, native.UpstreamModel, wrapped.UpstreamModel)
	require.Equal(t, native.UpstreamResponseModel, wrapped.UpstreamResponseModel)
	require.True(t, wrapped.UpstreamResponseModelConflict)
	require.Equal(t, native.UpstreamResponseServiceTier, wrapped.UpstreamResponseServiceTier)
	require.True(t, wrapped.Stream)
	require.True(t, wrapped.ClientDisconnect)
	require.Equal(t, native.Duration, wrapped.Duration)
	require.Equal(t, native.FirstTokenMs, wrapped.FirstTokenMs)
	require.Equal(t, native.StreamingAckMs, wrapped.StreamingAckMs)
	require.Equal(t, native.ReasoningEffort, wrapped.ReasoningEffort)
	require.Equal(t, native.RequestedReasoningEffort, wrapped.RequestedReasoningEffort)
	require.Equal(t, native.ServiceTier, wrapped.ServiceTier)
	require.Equal(t, native.ImageCount, wrapped.ImageCount)
	require.Equal(t, native.ImageEndpoint, wrapped.ImageEndpoint)
	require.Equal(t, native.ImageSize, wrapped.ImageSize)
	require.Equal(t, native.ImageInputSize, wrapped.ImageInputSize)
	require.Equal(t, native.ImageOutputSize, wrapped.ImageOutputSize)
	require.Equal(t, native.ImageOutputSizes, wrapped.ImageOutputSizes)
	require.Equal(t, native.ImageSizeSource, wrapped.ImageSizeSource)
	require.Equal(t, native.ImageSizeBreakdown, wrapped.ImageSizeBreakdown)
	require.Equal(t, native.SearchCount, wrapped.SearchCount)
	require.Equal(t, native.AudioUsage, wrapped.AudioUsage)
	projected := groupInputRoutingOpenAIUsageInput(&RecordUsageInput{Result: wrapped})
	require.Equal(t, native, projected.Result, "native-only usage, response, image, video and replay fields must survive the bridge")
	require.NotSame(t, native, projected.Result)

	// A cache-only partial stream is still recognizable as used; malformed total
	// input must not produce a negative generic input bucket.
	wrapped = WrapGroupInputRoutingOpenAIResult(&OpenAIForwardResult{Usage: OpenAIUsage{InputTokens: 10, CacheReadInputTokens: 20}, ClientDisconnect: true})
	require.Zero(t, wrapped.Usage.InputTokens)
	require.Equal(t, 20, wrapped.Usage.CacheReadInputTokens)
	require.True(t, wrapped.ClientDisconnect)
}

func TestGroupInputRoutingOpenAIUsageInputKeepsIdentityAndHandlerStamps(t *testing.T) {
	oldAck, ack, first := 10, 45, 900
	oldEffort, effort, requested := "medium", "high", "max"
	native := &OpenAIForwardResult{Model: "gpt-5.1", BillingModel: "billing-model", Usage: OpenAIUsage{InputTokens: 1000, CacheReadInputTokens: 300, CacheCreationInputTokens: 100, ImageInputTokens: 70, ImageCacheReadTokens: 40}, StreamingAckMs: &oldAck, ReasoningEffort: &oldEffort}
	wrapped := WrapGroupInputRoutingOpenAIResult(native)
	wrapped.StreamingAckMs, wrapped.FirstTokenMs = &ack, &first
	wrapped.ReasoningEffort, wrapped.RequestedReasoningEffort = &effort, &requested
	wrapped.Duration, wrapped.ClientDisconnect = 2*time.Second, true
	wrapped.Usage = ClaudeUsage{InputTokens: 99999}
	groupID := int64(7)
	input := &RecordUsageInput{
		Result: wrapped, APIKey: &APIKey{ID: 11, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 2}},
		User: &User{ID: 12}, Account: &Account{ID: 13, Platform: PlatformOpenAI}, Subscription: &UserSubscription{ID: 14},
		PricingAt: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC), InboundEndpoint: "/v1/messages", UpstreamEndpoint: "/v1/responses",
		UserAgent: "client", IPAddress: "127.0.0.1", SessionID: "session", RequestPayloadHash: "hash", ForceCacheBilling: true,
		APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}, QuotaPlatform: PlatformAnthropic,
		ChannelUsageFields: ChannelUsageFields{ChannelID: 15, OriginalModel: "client-model", ChannelMappedModel: "mapped-model", BillingModelSource: BillingModelSourceRequested, ModelMappingChain: "client-model->mapped-model"},
	}
	got := groupInputRoutingOpenAIUsageInput(input)
	require.Equal(t, &OpenAIRecordUsageInput{
		Result: got.Result, APIKey: input.APIKey, User: input.User, Account: input.Account, Subscription: input.Subscription,
		PricingAt: input.PricingAt, InboundEndpoint: input.InboundEndpoint, UpstreamEndpoint: input.UpstreamEndpoint,
		UserAgent: input.UserAgent, IPAddress: input.IPAddress, SessionID: input.SessionID, RequestPayloadHash: input.RequestPayloadHash,
		APIKeyService: input.APIKeyService, QuotaPlatform: input.QuotaPlatform, ChannelUsageFields: input.ChannelUsageFields,
	}, got)
	require.Same(t, input.APIKey, got.APIKey)
	require.Equal(t, native.Usage, got.Result.Usage, "generic token projections must not overwrite original OpenAI usage")
	require.Equal(t, native.BillingModel, got.Result.BillingModel)
	require.Equal(t, &ack, got.Result.StreamingAckMs)
	require.Equal(t, &first, got.Result.FirstTokenMs)
	require.Equal(t, &effort, got.Result.ReasoningEffort)
	require.Equal(t, &requested, got.Result.RequestedReasoningEffort)
	require.Equal(t, 2*time.Second, got.Result.Duration)
	require.True(t, got.Result.ClientDisconnect)
	require.Equal(t, &oldAck, native.StreamingAckMs)
	require.Equal(t, &oldEffort, native.ReasoningEffort)
}

func TestRecordGroupInputRoutingUsageDispatchesNativeOpenAIBilling(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	groupID := int64(7)
	first, ack := 900, 45
	effort, requested := "high", "max"
	native := &OpenAIForwardResult{RequestID: "cross-platform-native", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 50, CacheReadInputTokens: 300, CacheCreationInputTokens: 100}, Stream: true}
	wrapped := WrapGroupInputRoutingOpenAIResult(native)
	wrapped.FirstTokenMs, wrapped.StreamingAckMs = &first, &ack
	wrapped.ReasoningEffort, wrapped.RequestedReasoningEffort = &effort, &requested
	wrapped.Usage.InputTokens = 99999 // must never become the native billed quantity
	key := &APIKey{ID: 11, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 2}}
	err := RecordGroupInputRoutingUsage(context.Background(), nil, svc, &RecordUsageInput{
		Result: wrapped, APIKey: key, User: &User{ID: 12}, Account: &Account{ID: 13, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		InboundEndpoint: "/v1/messages", UpstreamEndpoint: "/v1/responses", ForceCacheBilling: true,
		ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-5.1"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, logs.calls)
	require.Equal(t, 1, billing.calls)
	require.Equal(t, 600, logs.lastLog.InputTokens)
	require.Equal(t, 300, logs.lastLog.CacheReadTokens)
	require.Equal(t, 100, logs.lastLog.CacheCreationTokens)
	require.Equal(t, &groupID, logs.lastLog.GroupID)
	require.Equal(t, &first, logs.lastLog.FirstTokenMs)
	require.Equal(t, &ack, logs.lastLog.StreamingAckMs)
	require.Equal(t, &requested, logs.lastLog.RequestedReasoningEffort)
	require.Equal(t, &effort, logs.lastLog.ReasoningEffort)
	expected := expectedOpenAICost(t, svc, "gpt-5.1", native.Usage, 2)
	require.InDelta(t, expected.ActualCost, logs.lastLog.ActualCost, 1e-12)
	require.Equal(t, PlatformAnthropic, key.Group.Platform)
}

func TestRecordGroupInputRoutingUsagePreservesGenericBilling(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	err := RecordGroupInputRoutingUsage(context.Background(), svc, nil, &RecordUsageInput{
		Result: &ForwardResult{RequestID: "generic", Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 50, CacheReadInputTokens: 300}},
		APIKey: &APIKey{ID: 11}, User: &User{ID: 12}, Account: &Account{ID: 13, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	require.Equal(t, 1, logs.calls)
	require.Equal(t, 1000, logs.lastLog.InputTokens, "generic input is already exclusive of cache")
	require.Equal(t, 300, logs.lastLog.CacheReadTokens)
}

func TestRecordGroupInputRoutingUsageRejectsMissingDependencies(t *testing.T) {
	ctx := context.Background()
	require.Error(t, RecordGroupInputRoutingUsage(ctx, nil, nil, nil))
	require.Error(t, RecordGroupInputRoutingUsage(ctx, nil, nil, &RecordUsageInput{}))
	require.Error(t, RecordGroupInputRoutingUsage(ctx, nil, nil, &RecordUsageInput{Result: &ForwardResult{}}))
	require.Error(t, RecordGroupInputRoutingUsage(ctx, nil, nil, &RecordUsageInput{Result: WrapGroupInputRoutingOpenAIResult(&OpenAIForwardResult{})}))
}
