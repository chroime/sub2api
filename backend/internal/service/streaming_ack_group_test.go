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
