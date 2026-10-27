package service

import (
	"context"
	"strings"
	"time"
)

// Spending-limit is recoverable at the end of the observed billing period.
// When no billing snapshot is available, use a short probe rather than
// fabricating a 24h boundary from the error arrival time.
const grokSpendingLimitProbeCooldown = 10 * time.Minute

// grokExhaustedForbiddenCooldown applies to an unclassified 403 from an
// account whose billing snapshot already shows an exhausted product window.
// xAI rejects such accounts intermittently, so a short probe interval keeps
// the account recoverable without sending every request into a known 403.
const grokExhaustedForbiddenCooldown = 10 * time.Minute

func grokSpendingLimitResetAt(account *Account, now time.Time) time.Time {
	if account == nil {
		return now.Add(grokSpendingLimitProbeCooldown)
	}
	billing, err := grokBillingSnapshotFromExtra(account.Extra)
	if err != nil || billing == nil {
		return now.Add(grokSpendingLimitProbeCooldown)
	}
	// The rolling (weekly) window is authoritative when present. Once it has
	// elapsed the snapshot is stale: a new window has already started, so the
	// monthly invoice boundary must not be used as a substitute reset. Doing so
	// previously locked accounts with a fresh, unused week until month end.
	if periodEnd := strings.TrimSpace(billing.PeriodEnd); periodEnd != "" {
		if resetAt, err := time.Parse(time.RFC3339, periodEnd); err == nil {
			if resetAt.After(now) {
				return resetAt
			}
			return now.Add(grokSpendingLimitProbeCooldown)
		}
	}
	// Monthly-only billing (no rolling window observed).
	if resetAt, err := time.Parse(time.RFC3339, strings.TrimSpace(billing.BillingPeriodEnd)); err == nil && resetAt.After(now) {
		return resetAt
	}
	return now.Add(grokSpendingLimitProbeCooldown)
}

// grokBillingWindowExhausted reports whether the last billing probe saw the
// account's included usage, or any product's usage, at or above 100%.
func grokBillingWindowExhausted(account *Account) bool {
	if account == nil {
		return false
	}
	billing, err := grokBillingSnapshotFromExtra(account.Extra)
	if err != nil || billing == nil {
		return false
	}
	// A snapshot whose rolling window has already ended describes a past week.
	if periodEnd, err := time.Parse(time.RFC3339, strings.TrimSpace(billing.PeriodEnd)); err == nil && !periodEnd.After(time.Now()) {
		return false
	}
	if billing.UsagePercent != nil && *billing.UsagePercent >= 100 {
		return true
	}
	for _, product := range billing.ProductUsage {
		if product.UsagePercent != nil && *product.UsagePercent >= 100 {
			return true
		}
	}
	return false
}

// clearGrokNeedsReauthExtra drops the soft reauth flag after successful refresh
// or reauth. Best-effort; never fails the request path.
func clearGrokNeedsReauthExtra(ctx context.Context, repo AccountRepository, accountID int64) {
	if repo == nil || accountID <= 0 {
		return
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	_ = repo.UpdateExtra(stateCtx, accountID, map[string]any{
		"grok_needs_reauth":        false,
		"grok_needs_reauth_reason": "",
		"grok_needs_reauth_at":     "",
	})
}

func accountGrokNeedsReauth(account *Account) bool {
	if account == nil {
		return false
	}
	if account.Status == StatusError {
		msg := strings.ToLower(account.ErrorMessage)
		if strings.Contains(msg, "spending limit") || strings.Contains(msg, "reauthorize") {
			return true
		}
	}
	if v, ok := account.Extra["grok_needs_reauth"].(bool); ok && v {
		return true
	}
	if s, ok := account.Extra["grok_needs_reauth"].(string); ok {
		return strings.EqualFold(s, "true") || s == "1"
	}
	return false
}
