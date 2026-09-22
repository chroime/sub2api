//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestStreamingACKUsesEffectiveFallbackGroup(t *testing.T) {
	source, fallback, unrelated := int64(10), int64(11), int64(12)
	repo := &mockGroupRepoForGateway{groups: map[int64]*Group{
		source:    {ID: source, Platform: PlatformAnthropic, ClaudeCodeOnly: true, FallbackGroupID: &fallback},
		fallback:  {ID: fallback, Platform: PlatformAnthropic},
		unrelated: {ID: unrelated, Platform: PlatformComposite},
	}}
	svc := &GatewayService{groupRepo: repo}
	a := &Account{Platform: PlatformAnthropic, GroupIDs: []int64{fallback}, Extra: map[string]any{StreamingACKEnabledExtraKey: true}}
	ctx := context.Background()
	require.True(t, svc.IsStreamingACKEnabledForRequest(ctx, a, &source))
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, a, &unrelated))
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, a, nil))
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.WithValue(ctx, ctxkey.ForcePlatform, PlatformAntigravity), a, &source))
	a.GroupIDs = []int64{unrelated}
	require.True(t, svc.IsStreamingACKEnabledForRequest(ctx, a, &unrelated))
	a.GroupIDs = []int64{source}
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, a, &source))
	repo.groups[fallback].ClaudeCodeOnly = true
	repo.groups[fallback].FallbackGroupID = &source
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, a, &source), "cycles must fail closed")
}

func TestStreamingACKGroupPolicyOverridesAccountAcrossAllPlatforms(t *testing.T) {
	on, off := true, false
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			onID, offID, oldID := int64(21), int64(22), int64(23)
			repo := &mockGroupRepoForGateway{groups: map[int64]*Group{
				onID:  {ID: onID, Platform: platform, StreamingACKEnabled: &on},
				offID: {ID: offID, Platform: platform, StreamingACKEnabled: &off},
				oldID: {ID: oldID, Platform: platform},
			}}
			svc := &GatewayService{groupRepo: repo}
			account := &Account{Platform: platform, GroupIDs: []int64{onID, offID, oldID}, Extra: map[string]any{StreamingACKEnabledExtraKey: false}}
			require.True(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &onID), "group on must work with account off")
			require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &offID))
			require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &oldID))
			account.Extra[StreamingACKEnabledExtraKey] = true
			require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &offID), "group off must override account on")
			require.True(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &oldID), "existing null groups preserve opt-in")
			account.ParentAccountID = &oldID
			require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &onID), "group policy must not remove shadow protection")
		})
	}
}

func TestStreamingACKGroupPolicyUsesFallbackAndCompositeGroup(t *testing.T) {
	on, off := true, false
	source, fallback, composite := int64(10), int64(11), int64(12)
	repo := &mockGroupRepoForGateway{groups: map[int64]*Group{
		source:    {ID: source, Platform: PlatformAnthropic, ClaudeCodeOnly: true, FallbackGroupID: &fallback, StreamingACKEnabled: &off},
		fallback:  {ID: fallback, Platform: PlatformAnthropic, StreamingACKEnabled: &on},
		composite: {ID: composite, Platform: PlatformComposite, StreamingACKEnabled: &on},
	}}
	svc := &GatewayService{groupRepo: repo}
	account := &Account{Platform: PlatformAnthropic, GroupIDs: []int64{source, fallback, composite}}
	require.True(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &source))
	require.True(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &composite))
	forced := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity)
	require.False(t, svc.IsStreamingACKEnabledForRequest(forced, account, &source), "forced platform retains source group policy")
	repo.groups[source].StreamingACKEnabled = &on
	require.True(t, svc.IsStreamingACKEnabledForRequest(forced, account, &source))
	repo.groups[fallback].StreamingACKEnabled = &off
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &source))
	account.GroupIDs = nil
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &composite), "unrelated account cannot acquire group ACK")
	account.GroupIDs = []int64{composite}
	account.Platform = "unsupported"
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &composite))
	unknown := int64(999)
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &unknown))
}

func TestOpenAIStreamingACKGroupPolicyUsesHydratedContextOrReader(t *testing.T) {
	on, off := true, false
	id, other := int64(7), int64(8)
	group := &Group{ID: id, Platform: PlatformComposite, Status: StatusActive, Hydrated: true, StreamingACKEnabled: &on}
	repo := &mockGroupRepoForGateway{groups: map[int64]*Group{id: group}}
	settings := NewSettingService(nil, nil)
	settings.SetDefaultSubscriptionGroupReader(repo)
	svc := &OpenAIGatewayService{settingService: settings}
	account := &Account{Platform: PlatformOpenAI, GroupIDs: []int64{id}, Extra: map[string]any{StreamingACKEnabledExtraKey: false}}
	require.True(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &id))
	group.StreamingACKEnabled = &off
	account.Extra[StreamingACKEnabledExtraKey] = true
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &id))
	group.StreamingACKEnabled = nil
	require.True(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &id))
	// A context for another group cannot override the requested group's policy.
	foreign := &Group{ID: other, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, StreamingACKEnabled: &on}
	group.StreamingACKEnabled = &off
	ctx := context.WithValue(context.Background(), ctxkey.Group, foreign)
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, account, &id))
	// Valid auth snapshots work without a second database lookup, including the
	// explicit false policy that must not become a legacy opt-in from account.
	ctx = context.WithValue(context.Background(), ctxkey.Group, group)
	delete(repo.groups, id)
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, account, &id))
	group.StreamingACKEnabled = &on
	require.True(t, svc.IsStreamingACKEnabledForRequest(ctx, account, &id))
	require.False(t, svc.IsStreamingACKEnabledForRequest(context.Background(), account, &id), "lookup failure must not fall back to legacy enable")
	group.Hydrated = false
	require.False(t, svc.IsStreamingACKEnabledForRequest(ctx, account, &id), "untrusted context cannot assert group policy")
}
