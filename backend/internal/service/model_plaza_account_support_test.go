//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func plazaSupportAccount(platform string, mapping map[string]any) *Account {
	return &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": mapping}}
}

func plazaSupportChannel(platform, billingSource string, allowed []string) *ChannelService {
	return newTestChannelService(makeStandardRepo(Channel{
		ID: 1, Status: StatusActive, GroupIDs: []int64{10}, RestrictModels: len(allowed) > 0,
		BillingModelSource: billingSource,
		ModelPricing:       []ChannelModelPricing{{Platform: platform, Models: allowed}},
		ModelMapping:       map[string]map[string]string{platform: {"public-alias": "channel-model"}},
	}, map[int64]string{10: platform}))
}

func TestPlazaAccountSupportUsesProtocolSchedulingModel(t *testing.T) {
	for _, tc := range []struct {
		platform string
		mapping  map[string]any
		want     bool
	}{
		{PlatformOpenAI, map[string]any{"channel-model": "upstream-model"}, true},
		{PlatformKimi, map[string]any{"channel-model": "upstream-model"}, true},
		{PlatformGrok, map[string]any{"channel-model": "upstream-model"}, true},
		{PlatformAnthropic, map[string]any{"channel-model": "upstream-model"}, false},
		{PlatformGemini, map[string]any{"channel-model": "upstream-model"}, false},
		{PlatformAnthropic, map[string]any{"public-alias": "upstream-model"}, true},
		{PlatformGemini, map[string]any{"public-alias": "upstream-model"}, true},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			svc := &GatewayService{channelService: plazaSupportChannel(tc.platform, BillingModelSourceChannelMapped, nil)}
			require.Equal(t, tc.want, svc.AccountSupportsModelForListing(context.Background(), &Group{ID: 10, Platform: tc.platform}, plazaSupportAccount(tc.platform, tc.mapping), "public-alias"))
		})
	}
}

func TestPlazaAccountSupportAppliesChannelRestrictionAtActualBillingModel(t *testing.T) {
	for _, tc := range []struct {
		name, platform, source string
		allowed                []string
		mapping                map[string]any
		passthrough            bool
		want                   bool
	}{
		{"mapped precheck allowed", PlatformOpenAI, BillingModelSourceChannelMapped, []string{"channel-model"}, map[string]any{"channel-model": "upstream-model"}, false, true},
		{"mapped precheck denied", PlatformOpenAI, BillingModelSourceChannelMapped, []string{"other"}, map[string]any{"channel-model": "upstream-model"}, false, false},
		{"requested precheck uses public", PlatformOpenAI, BillingModelSourceRequested, []string{"public-alias"}, map[string]any{"channel-model": "upstream-model"}, false, true},
		{"upstream allowed", PlatformOpenAI, BillingModelSourceUpstream, []string{"upstream-model"}, map[string]any{"channel-model": "upstream-model"}, false, true},
		{"upstream denied", PlatformOpenAI, BillingModelSourceUpstream, []string{"other"}, map[string]any{"channel-model": "upstream-model"}, false, false},
		{"passthrough ignores stale mapping", PlatformOpenAI, BillingModelSourceUpstream, []string{"channel-model"}, map[string]any{"channel-model": "stale-model"}, true, true},
		{"passthrough denied", PlatformOpenAI, BillingModelSourceUpstream, []string{"stale-model"}, map[string]any{"channel-model": "stale-model"}, true, false},
		{"anthropic upstream uses scheduling name", PlatformAnthropic, BillingModelSourceUpstream, []string{"upstream-model"}, map[string]any{"public-alias": "upstream-model", "channel-model": "other"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := plazaSupportAccount(tc.platform, tc.mapping)
			if tc.passthrough {
				account.Extra = map[string]any{"openai_passthrough": true, "openai_responses_supported": true}
			}
			svc := &GatewayService{channelService: plazaSupportChannel(tc.platform, tc.source, tc.allowed)}
			require.Equal(t, tc.want, svc.AccountSupportsModelForListing(context.Background(), &Group{ID: 10, Platform: tc.platform}, account, "public-alias"))
		})
	}
}

func TestPlazaAccountSupportRetirementAndExactMappingWinOverWildcard(t *testing.T) {
	group := &Group{ID: 10, Platform: PlatformOpenAI}
	svc := &GatewayService{}
	account := plazaSupportAccount(PlatformOpenAI, map[string]any{"gpt-*": "gpt-6.1-sol", "gpt-broken": "gpt-5.5"})
	account.Type = AccountTypeOAuth
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "gpt-broken"))
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "gpt-healthy"))
	account.Extra = map[string]any{"openai_passthrough": true}
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "gpt-5.5"))
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "gpt-6.1-sol"))
}

func TestPlazaAccountSupportQualityIsolationKeepsTransientCooldown(t *testing.T) {
	group := &Group{ID: 10, Platform: PlatformOpenAI}
	svc := &GatewayService{channelService: plazaSupportChannel(PlatformOpenAI, BillingModelSourceChannelMapped, nil)}
	account := plazaSupportAccount(PlatformOpenAI, map[string]any{"channel-model": "upstream-model", "other-model": "other-upstream"})
	until := time.Now().Add(time.Hour)
	account.TempUnschedulableUntil = &until
	account.RateLimitResetAt = &until
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "public-alias"))
	account.Extra = map[string]any{AccountQualityGatesExtraKey: map[string]any{"1": map[string]any{
		"scope": "model", "model": "upstream-model", "blocked": true,
	}}}
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "public-alias"))
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "other-model"))
	account.Extra = map[string]any{AccountQualityGatesExtraKey: map[string]any{"1": map[string]any{
		"scope": "account", "blocked": true,
	}}}
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "other-model"))
}

func TestPlazaAccountSupportGrokMediaAndVoiceUseCapabilities(t *testing.T) {
	group := &Group{ID: 10, Platform: PlatformGrok}
	svc := &GatewayService{}
	account := plazaSupportAccount(PlatformGrok, nil)
	account.Extra = map[string]any{GrokMediaEligibleExtraKey: false}
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "grok-imagine-image"))
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "grok-4.7"))
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "grok-voice-tts"))
	account.Extra = map[string]any{AccountQualityGatesExtraKey: map[string]any{"1": map[string]any{"scope": "account", "blocked": true}}}
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "grok-voice-tts"))
}

func TestPlazaAccountSupportCompositeResolvesPublicAliasBeforeChannel(t *testing.T) {
	group := &Group{ID: 10, Platform: PlatformComposite}
	account := plazaSupportAccount(PlatformOpenAI, map[string]any{"channel-model": "upstream-model"})
	svc := &GatewayService{channelService: plazaSupportChannel(PlatformOpenAI, BillingModelSourceChannelMapped, nil), compositeResolver: NewCompositeRouteResolver(nil)}
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "public-alias"), "a channel alias alone does not give a composite public model platform ownership")
	svc.compositeResolver = NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{{
		ID: 1, GroupID: 10, Enabled: true, PublicModel: "public-alias", MatchType: CompositeRouteMatchExact,
		Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformOpenAI, UpstreamModel: "channel-model",
	}}})
	require.True(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "public-alias"))
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, plazaSupportAccount(PlatformGemini, nil), "public-alias"))
	svc.compositeResolver = NewCompositeRouteResolver(nil)
	svc.compositeResolver.SetModelOwnershipResolver(func(context.Context, int64, string) (CompositeModelOwnership, error) {
		return CompositeModelOwnership{Ambiguous: true}, nil
	})
	require.False(t, svc.AccountSupportsModelForListing(context.Background(), group, account, "public-alias"))
}
