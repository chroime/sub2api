package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// Only a complete, positive pair of windows is evidence that a 429 did not
// exhaust the account quota. Partial headers retain the conservative policy.
func grok429HasAvailableQuota(snapshot *xai.QuotaSnapshot) bool {
	if snapshot == nil || snapshot.StatusCode != http.StatusTooManyRequests {
		return false
	}
	for _, window := range []*xai.QuotaWindow{snapshot.Requests, snapshot.Tokens} {
		if window == nil || window.Limit == nil || *window.Limit <= 0 ||
			window.Remaining == nil || *window.Remaining <= 0 || *window.Remaining > *window.Limit {
			return false
		}
	}
	return true
}

// A throttle with explicitly available quota blocks only the attempted model.
// Billing/free-usage bodies take precedence over these otherwise healthy
// headers. Keep API-key pools and unknown/model-less responses unchanged.
func applyGrokAvailableQuotaModelThrottle(account *Account, snapshot *xai.QuotaSnapshot, decision GrokUpstreamFailureDecision, now time.Time) bool {
	if !isGrokOAuthAccount(account) || account.IsPoolMode() || decision.Class != GrokFailureRateLimit ||
		strings.TrimSpace(decision.Model) == "" || !grok429HasAvailableQuota(snapshot) {
		return false
	}
	resetAt, limited := grokRateLimitResetAt(snapshot, now)
	if !limited {
		return false
	}
	// This is an explicit retry boundary, not free-tier exhaustion. The
	// free-usage writer raises short windows to two hours, while the capacity
	// writer truncates long windows to five minutes; neither fits Retry-After.
	storeGrokModelQuotaBlock(account.ID, decision.Model, resetAt, now)
	return true
}
