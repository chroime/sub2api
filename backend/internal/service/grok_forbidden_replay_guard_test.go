package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestGrokForbiddenGuard(store *grokForbiddenReplayStore, apiKeyID int64, body string) (*GrokForbiddenReplayGuard, context.Context) {
	guard := newGrokForbiddenReplayGuardWithStore(store, apiKeyID, "/v1/responses", []byte(body))
	return guard, WithGrokForbiddenReplayGuard(context.Background(), guard)
}

func TestGrokForbiddenReplayGuardBlocksIdenticalReplay(t *testing.T) {
	store := newGrokForbiddenReplayStore()
	now := time.Now()

	first, ctx := newTestGrokForbiddenGuard(store, 1, `{"model":"grok-4.7"}`)
	_, _, blocked := first.Blocked(now)
	require.False(t, blocked)
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
	require.True(t, first.Finish(false, now))

	replay, _ := newTestGrokForbiddenGuard(store, 1, `{"model":"grok-4.7"}`)
	wait, scope, blocked := replay.Blocked(now.Add(time.Second))
	require.True(t, blocked)
	require.Equal(t, GrokForbiddenReplayScopeRequest, scope)
	require.Greater(t, wait, time.Duration(0))

	// A different body from the same key is not a replay.
	other, _ := newTestGrokForbiddenGuard(store, 1, `{"model":"grok-4.6"}`)
	_, _, blocked = other.Blocked(now.Add(time.Second))
	require.False(t, blocked)

	// The same body from another key is isolated.
	otherKey, _ := newTestGrokForbiddenGuard(store, 2, `{"model":"grok-4.7"}`)
	_, _, blocked = otherKey.Blocked(now.Add(time.Second))
	require.False(t, blocked)

	// The replay block expires.
	expired, _ := newTestGrokForbiddenGuard(store, 1, `{"model":"grok-4.7"}`)
	_, _, blocked = expired.Blocked(now.Add(grokForbiddenReplayTTL + time.Second))
	require.False(t, blocked)
}

func TestGrokForbiddenReplayGuardIgnoresRecoveredAndDisconnected(t *testing.T) {
	store := newGrokForbiddenReplayStore()
	now := time.Now()

	// 403 on one account, then success on another: not a refusal.
	recovered, ctx := newTestGrokForbiddenGuard(store, 1, `a`)
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusOK)
	require.False(t, recovered.Finish(false, now))

	// 403 followed by a different terminal failure: not a pure 403 refusal.
	mixed, ctx := newTestGrokForbiddenGuard(store, 1, `b`)
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusBadGateway)
	require.False(t, mixed.Finish(false, now))

	// Client disconnected: never recorded.
	gone, ctx := newTestGrokForbiddenGuard(store, 1, `c`)
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
	require.False(t, gone.Finish(true, now))

	for _, body := range []string{"a", "b", "c"} {
		g, _ := newTestGrokForbiddenGuard(store, 1, body)
		_, _, blocked := g.Blocked(now)
		require.False(t, blocked, body)
	}
}

func TestGrokForbiddenReplayGuardAPIKeyStrikes(t *testing.T) {
	store := newGrokForbiddenReplayStore()
	now := time.Now()

	for i := 0; i < grokForbiddenStrikeThreshold; i++ {
		// Clients that mutate the body each retry still accumulate strikes.
		g, ctx := newTestGrokForbiddenGuard(store, 7, time.Duration(i).String())
		recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
		require.True(t, g.Finish(false, now.Add(time.Duration(i)*time.Second)))
	}

	fresh, _ := newTestGrokForbiddenGuard(store, 7, "never-seen")
	_, scope, blocked := fresh.Blocked(now.Add(10 * time.Second))
	require.True(t, blocked)
	require.Equal(t, GrokForbiddenReplayScopeAPIKey, scope)

	_, _, blocked = fresh.Blocked(now.Add(grokForbiddenAPIKeyCooldown + 10*time.Second))
	require.False(t, blocked)

	unrelated, _ := newTestGrokForbiddenGuard(store, 8, "never-seen")
	_, _, blocked = unrelated.Blocked(now.Add(10 * time.Second))
	require.False(t, blocked)
}

func TestGrokForbiddenReplayGuardStrikesOutsideWindowDoNotAccumulate(t *testing.T) {
	store := newGrokForbiddenReplayStore()
	now := time.Now()
	for i := 0; i < grokForbiddenStrikeThreshold; i++ {
		g, ctx := newTestGrokForbiddenGuard(store, 9, time.Duration(i).String())
		recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
		g.Finish(false, now.Add(time.Duration(i)*(grokForbiddenStrikeWindow/2+time.Second)))
	}
	probe, _ := newTestGrokForbiddenGuard(store, 9, "probe")
	_, scope, blocked := probe.Blocked(now.Add(time.Duration(grokForbiddenStrikeThreshold) * grokForbiddenStrikeWindow))
	require.False(t, blocked, scope)
}

func TestGrokForbiddenReplayGuardFinishOnce(t *testing.T) {
	store := newGrokForbiddenReplayStore()
	g, ctx := newTestGrokForbiddenGuard(store, 3, "x")
	recordGrokForbiddenReplayUpstreamStatus(ctx, http.StatusForbidden)
	require.True(t, g.Finish(false, time.Now()))
	require.False(t, g.Finish(false, time.Now()))

	// Requests without a guard in context are a no-op.
	recordGrokForbiddenReplayUpstreamStatus(context.Background(), http.StatusForbidden)
	var nilGuard *GrokForbiddenReplayGuard
	_, _, blocked := nilGuard.Blocked(time.Now())
	require.False(t, blocked)
	require.False(t, nilGuard.Finish(false, time.Now()))
}
