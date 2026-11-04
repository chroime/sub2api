package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
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

// grokSpendingRefusalConfirmWindow is how long a first spending/credit refusal
// on an account that may still pay is remembered. A second refusal after the
// probe cooldown but inside the window means xAI really stopped serving, so
// its verdict wins and the account waits for the billing window reset.
// Refusals inside the probe cooldown are the same burst (requests already in
// flight to the account) and do not confirm anything.
const grokSpendingRefusalConfirmWindow = time.Hour

// grokBillingHasPrepaidHeadroom reports whether the last billing probe saw
// money xAI draws on once the weekly included credits are used up: a positive
// prepaid balance, or an on-demand cap that is not yet spent. Such accounts
// keep serving with a full weekly window.
func grokBillingHasPrepaidHeadroom(billing *xai.BillingSummary) bool {
	if billing == nil {
		return false
	}
	if billing.PrepaidBalance != nil && *billing.PrepaidBalance > 0 {
		return true
	}
	if billing.OnDemandCap != nil && *billing.OnDemandCap > 0 {
		used := 0.0
		if billing.OnDemandUsed != nil {
			used = *billing.OnDemandUsed
		}
		return used < *billing.OnDemandCap
	}
	return false
}

// grokBillingHasMonthlyHeadroom reports a monthly limit that is not used up.
// xAI does not document whether an account may draw on it once the weekly
// window is full, so it only earns a probe after an explicit credit refusal,
// never a pass on ambiguous 403s.
func grokBillingHasMonthlyHeadroom(billing *xai.BillingSummary) bool {
	if billing == nil || billing.MonthlyLimitCents == nil || *billing.MonthlyLimitCents <= 0 {
		return false
	}
	used := 0.0
	if billing.UsedCents != nil {
		used = *billing.UsedCents
	}
	return used < *billing.MonthlyLimitCents
}

// grokAccountMayStillPay reports whether the last billing probe saw any money
// the account could still spend: prepaid balance, on-demand cap, or monthly
// limit headroom.
func grokAccountMayStillPay(account *Account) bool {
	if account == nil {
		return false
	}
	billing, err := grokBillingSnapshotFromExtra(account.Extra)
	if err != nil {
		return false
	}
	return grokBillingHasPrepaidHeadroom(billing) || grokBillingHasMonthlyHeadroom(billing)
}

// grokSpendingRefusalResetAt returns the rate-limit horizon for an account xAI
// refused for spending limit or exhausted credits. While the last billing probe
// still shows money the account could spend, the first refusal only gets a
// short probe cooldown: the balance may be shared or topped up, and locking
// until the weekly reset would idle money that is still there. A confirmed
// repeat falls back to the billing window reset.
func grokSpendingRefusalResetAt(account *Account, now time.Time) time.Time {
	if account != nil && grokAccountMayStillPay(account) && !confirmGrokSpendingRefusal(account.ID, now) {
		return now.Add(grokSpendingLimitProbeCooldown)
	}
	return grokSpendingLimitResetAt(account, now)
}

type grokSpendingRefusalStore struct {
	mu    sync.Mutex
	items map[int64]time.Time // account -> first unconfirmed refusal
}

var globalGrokSpendingRefusals = &grokSpendingRefusalStore{items: make(map[int64]time.Time)}

// confirmGrokSpendingRefusal records a spending refusal and reports whether it
// confirms an earlier one: the earlier refusal must be older than the probe
// cooldown (a later burst) and inside grokSpendingRefusalConfirmWindow. A
// confirmation consumes the record. State is process-local, like the other
// Grok soft-blocks.
func confirmGrokSpendingRefusal(accountID int64, now time.Time) bool {
	if accountID <= 0 {
		return true
	}
	s := globalGrokSpendingRefusals
	s.mu.Lock()
	defer s.mu.Unlock()
	if first, ok := s.items[accountID]; ok {
		elapsed := now.Sub(first)
		if elapsed < grokSpendingLimitProbeCooldown {
			return false // same burst as the first refusal
		}
		if elapsed <= grokSpendingRefusalConfirmWindow {
			delete(s.items, accountID)
			return true
		}
	}
	s.items[accountID] = now
	return false
}

// resetGrokSpendingRefusal forgets an unconfirmed refusal after the account
// served a request successfully.
func resetGrokSpendingRefusal(accountID int64) {
	if accountID <= 0 {
		return
	}
	s := globalGrokSpendingRefusals
	s.mu.Lock()
	delete(s.items, accountID)
	s.mu.Unlock()
}

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
	// A full weekly window is not exhaustion while prepaid money remains: xAI
	// keeps serving and bills the overflow against it.
	if grokBillingHasPrepaidHeadroom(billing) {
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
