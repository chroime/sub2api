package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Grok forbidden replay guard.
//
// xAI bills OAuth credentials for requests it rejects with 403, and downstream
// pool-mode clients replay a failed request in tight loops. Every replay is
// routed onto another pooled account and burns its quota for the same refusal.
// The guard remembers request fingerprints whose final outcome followed an
// upstream 403 and rejects identical replays locally, without touching any
// account. Only identical replays are blocked: a key's other requests are never
// paused, so downstreams with a high 403 rate are not cut off wholesale.
// State is process-local, like the other Grok soft-blocks.
const (
	grokForbiddenReplayTTL           = 60 * time.Second
	grokForbiddenReplaySweepInterval = 4096
)

// GrokForbiddenReplayScopeRequest is the scope reported by
// GrokForbiddenReplayGuard.Blocked for an identical-request replay.
const GrokForbiddenReplayScopeRequest = "request"

type grokForbiddenReplayStore struct {
	mu           sync.Mutex
	requests     map[string]time.Time // fingerprint -> blocked until
	insertsSince int
}

var globalGrokForbiddenReplayStore = newGrokForbiddenReplayStore()

func newGrokForbiddenReplayStore() *grokForbiddenReplayStore {
	return &grokForbiddenReplayStore{
		requests: make(map[string]time.Time),
	}
}

// GrokForbiddenReplayGuard tracks one inbound request. The handler creates it
// before account selection; upstream error handling records the statuses seen
// while forwarding; Finish commits the outcome once the response is written.
type GrokForbiddenReplayGuard struct {
	apiKeyID       int64
	fingerprint    string
	lastUpstream   atomic.Int32
	store          *grokForbiddenReplayStore
	finishedCalled atomic.Bool
}

type grokForbiddenReplayGuardContextKey struct{}

// NewGrokForbiddenReplayGuard fingerprints the request by API key, inbound
// path and body. Identical bytes from the same key are treated as a replay.
func NewGrokForbiddenReplayGuard(apiKeyID int64, path string, body []byte) *GrokForbiddenReplayGuard {
	return newGrokForbiddenReplayGuardWithStore(globalGrokForbiddenReplayStore, apiKeyID, path, body)
}

func newGrokForbiddenReplayGuardWithStore(store *grokForbiddenReplayStore, apiKeyID int64, path string, body []byte) *GrokForbiddenReplayGuard {
	h := sha256.New()
	h.Write([]byte(strconv.FormatInt(apiKeyID, 10)))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return &GrokForbiddenReplayGuard{
		apiKeyID:    apiKeyID,
		fingerprint: hex.EncodeToString(h.Sum(nil)),
		store:       store,
	}
}

// WithGrokForbiddenReplayGuard attaches the guard so Grok upstream error
// handling deep in the forward path can report statuses back to it.
func WithGrokForbiddenReplayGuard(ctx context.Context, guard *GrokForbiddenReplayGuard) context.Context {
	if ctx == nil || guard == nil {
		return ctx
	}
	return context.WithValue(ctx, grokForbiddenReplayGuardContextKey{}, guard)
}

func grokForbiddenReplayGuardFromContext(ctx context.Context) *GrokForbiddenReplayGuard {
	if ctx == nil {
		return nil
	}
	guard, _ := ctx.Value(grokForbiddenReplayGuardContextKey{}).(*GrokForbiddenReplayGuard)
	return guard
}

// recordGrokForbiddenReplayUpstreamStatus remembers the most recent upstream
// status of the request. Only the last one matters: a 403 followed by a
// success or a different failure on another account is not a 403 refusal.
func recordGrokForbiddenReplayUpstreamStatus(ctx context.Context, statusCode int) {
	if guard := grokForbiddenReplayGuardFromContext(ctx); guard != nil && statusCode > 0 {
		guard.lastUpstream.Store(int32(statusCode))
	}
}

// Blocked reports whether this request must be rejected locally, how long the
// client should wait, and which scope triggered the block.
func (g *GrokForbiddenReplayGuard) Blocked(now time.Time) (time.Duration, string, bool) {
	if g == nil || g.store == nil {
		return 0, "", false
	}
	s := g.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if until, ok := s.requests[g.fingerprint]; ok {
		if until.After(now) {
			return until.Sub(now), GrokForbiddenReplayScopeRequest, true
		}
		delete(s.requests, g.fingerprint)
	}
	return 0, "", false
}

// Finish commits the request outcome. A request counts as a forbidden refusal
// only when the last upstream status it saw was 403: successful upstream
// responses record 200, so a 403 recovered on another account is ignored.
// Requests whose client disconnected are ignored as well.
func (g *GrokForbiddenReplayGuard) Finish(clientGone bool, now time.Time) bool {
	if g == nil || g.store == nil || !g.finishedCalled.CompareAndSwap(false, true) {
		return false
	}
	if clientGone || g.lastUpstream.Load() != http.StatusForbidden {
		return false
	}
	g.store.recordForbidden(g.fingerprint, now)
	return true
}

func (s *grokForbiddenReplayStore) recordForbidden(fingerprint string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[fingerprint] = now.Add(grokForbiddenReplayTTL)

	s.insertsSince++
	if s.insertsSince >= grokForbiddenReplaySweepInterval {
		s.insertsSince = 0
		s.sweepLocked(now)
	}
}

func (s *grokForbiddenReplayStore) sweepLocked(now time.Time) {
	for key, until := range s.requests {
		if !until.After(now) {
			delete(s.requests, key)
		}
	}
}
