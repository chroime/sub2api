package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// Spending-limit is recoverable at the end of the observed billing period.
// When no billing snapshot is available, use a short probe rather than
// fabricating a 24h boundary from the error arrival time. It is also the retry
// lock after a credit refusal on an account that still has prepaid money.
const grokSpendingLimitProbeCooldown = 10 * time.Minute

// grokExhaustedForbiddenCooldown applies to an unclassified 403 from an
// account whose billing snapshot already shows an exhausted product window.
// xAI rejects such accounts intermittently, so a short probe interval keeps
// the account recoverable without sending every request into a known 403.
const grokExhaustedForbiddenCooldown = 10 * time.Minute

// grokSpendingRefusalConfirmWindow is how long a first spending/credit refusal
// on an account with only monthly-limit headroom is remembered. A second
// refusal after the probe cooldown but inside the window means xAI really
// stopped serving, so its verdict wins and the account waits for the billing
// window reset. Refusals inside the probe cooldown are the same burst (requests
// already in flight to the account) and do not confirm anything.
const grokSpendingRefusalConfirmWindow = time.Hour

// grokSpendingRecheckInterval spaces the billing re-checks that follow credit
// refusals on a prepaid account, so a burst of refusals costs one probe. It is
// a little shorter than the retry lock, so every retry cycle gets its check.
const grokSpendingRecheckInterval = grokSpendingLimitProbeCooldown - time.Minute

// grokSpendingLockReleaseTolerance absorbs the few seconds by which xAI's
// reported billing window end drifts between probes.
const grokSpendingLockReleaseTolerance = 5 * time.Minute

// grokBillingHasPrepaidHeadroom reports whether the last billing probe saw
// money xAI draws on once the weekly included credits are used up: a positive
// prepaid balance, or an on-demand cap that is not yet spent. Such accounts
// keep serving with a full weekly window.
func grokBillingHasPrepaidHeadroom(billing *xai.BillingSummary) bool {
	return grokBillingSpendableMoney(billing) > 0
}

// grokBillingSpendableMoney is the prepaid balance plus the unspent on-demand
// cap, in the units the billing probe reports.
func grokBillingSpendableMoney(billing *xai.BillingSummary) float64 {
	if billing == nil {
		return 0
	}
	money := 0.0
	if billing.PrepaidBalance != nil && *billing.PrepaidBalance > 0 {
		money += *billing.PrepaidBalance
	}
	if billing.OnDemandCap != nil && *billing.OnDemandCap > 0 {
		used := 0.0
		if billing.OnDemandUsed != nil {
			used = *billing.OnDemandUsed
		}
		if left := *billing.OnDemandCap - used; left > 0 {
			money += left
		}
	}
	return money
}

// grokBillingWeeklyWindowHasRoom reports a current weekly window whose included
// usage, and every product's usage, is below 100%.
func grokBillingWeeklyWindowHasRoom(billing *xai.BillingSummary, now time.Time) bool {
	if billing == nil || billing.UsagePercent == nil || *billing.UsagePercent >= 100 {
		return false
	}
	periodEnd, err := time.Parse(time.RFC3339, strings.TrimSpace(billing.PeriodEnd))
	if err != nil || !periodEnd.After(now) {
		return false
	}
	for _, product := range billing.ProductUsage {
		if product.UsagePercent != nil && *product.UsagePercent >= 100 {
			return false
		}
	}
	return true
}

// grokBillingWindowEnd returns the end of the billing window a spending lock
// waits for, chosen like grokSpendingLimitResetAt: the rolling (weekly) window
// when present, else the monthly invoice period. Zero when that end is not in
// the future.
func grokBillingWindowEnd(billing *xai.BillingSummary, now time.Time) time.Time {
	if billing == nil {
		return time.Time{}
	}
	raw := strings.TrimSpace(billing.PeriodEnd)
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		raw = strings.TrimSpace(billing.BillingPeriodEnd)
	}
	end, err := time.Parse(time.RFC3339, raw)
	if err != nil || !end.After(now) {
		return time.Time{}
	}
	return end
}

// releaseGrokSpendingLockIfServable clears a spending lock once a fresh billing
// probe shows the account can serve again. A spending lock is a rate limit
// that runs to the billing window end or past it: credit refusals install
// one, and locks written under older rules can outlast the current window.
// The account can serve when its weekly window has room or it still has
// prepaid / on-demand money. Monthly-limit headroom does not count, because
// xAI keeps refusing such accounts once the weekly window is full. Shorter
// rate limits (429 quota windows, probe cooldowns) are left alone.
//
// An account xAI keeps refusing is released at most once per billing window
// unless its money grows, so billing refreshes cannot loop between release
// and refusal. Clearing the persisted lock is enough: the scheduler drops the
// in-process block once the account's cooldown fields are empty.
func releaseGrokSpendingLockIfServable(ctx context.Context, repo AccountRepository, account *Account, billing *xai.BillingSummary, now time.Time) bool {
	if repo == nil || account == nil || account.RateLimitedAt == nil || account.RateLimitResetAt == nil || !account.RateLimitResetAt.After(now) {
		return false
	}
	windowEnd := grokBillingWindowEnd(billing, now)
	if windowEnd.IsZero() || account.RateLimitResetAt.Before(windowEnd.Add(-grokSpendingLockReleaseTolerance)) {
		return false
	}
	money := grokBillingSpendableMoney(billing)
	windowRoom := grokBillingWeeklyWindowHasRoom(billing, now)
	if money <= 0 && !windowRoom {
		return false
	}
	if !globalGrokSpendingLockReleases.allow(account.ID, windowEnd, money) {
		return false
	}
	if !clearGrokRateLimitAfterRecovery(ctx, repo, account) {
		return false
	}
	globalGrokSpendingLockReleases.record(account.ID, windowEnd, money, now)
	resetGrokSpendingRefusal(account.ID)
	slog.Info("grok_spending_lock_released",
		"account_id", account.ID,
		"rate_limit_reset_at", account.RateLimitResetAt.UTC(),
		"window_end", windowEnd.UTC(),
		"spendable_money", money,
		"weekly_window_room", windowRoom,
	)
	return true
}

// grokSpendingLockRelease is the evidence behind an automatic unlock.
type grokSpendingLockRelease struct {
	windowEnd time.Time
	money     float64
}

type grokSpendingLockReleaseStore struct {
	mu    sync.Mutex
	items map[int64]grokSpendingLockRelease
}

var globalGrokSpendingLockReleases = &grokSpendingLockReleaseStore{items: make(map[int64]grokSpendingLockRelease)}

// allow reports whether a release backed by this evidence is new: the first
// one in this billing window, or one with more money than the last. State is
// process-local, like the other Grok soft-blocks.
func (s *grokSpendingLockReleaseStore) allow(accountID int64, windowEnd time.Time, money float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.items[accountID]
	if !ok {
		return true
	}
	drift := last.windowEnd.Sub(windowEnd)
	if drift < 0 {
		drift = -drift
	}
	return drift > grokSpendingLockReleaseTolerance || money > last.money
}

func (s *grokSpendingLockReleaseStore) record(accountID int64, windowEnd time.Time, money float64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, item := range s.items {
		if !item.windowEnd.After(now) {
			delete(s.items, id)
		}
	}
	s.items[accountID] = grokSpendingLockRelease{windowEnd: windowEnd, money: money}
}

// grokBillingProber refreshes an account's billing snapshot from xAI. The
// gateway gets one after construction (GrokQuotaService is built later).
type grokBillingProber interface {
	ProbeBilling(ctx context.Context, accountID int64) (*GrokQuotaProbeResult, error)
}

// SetGrokBillingProber lets credit refusals re-check the refused account's
// billing.
func (s *OpenAIGatewayService) SetGrokBillingProber(quota *GrokQuotaService) {
	if s != nil && quota != nil {
		s.grokBillingProbe = quota
	}
}

// rateLimitGrokSpendingRefusal locks an account xAI refused for a spending
// limit or exhausted credits. When the stored billing still shows prepaid
// money, the lock is only the probe cooldown, and a background billing probe
// checks the balance: if the money is gone, the lock is extended to the
// billing window reset, so an account that really ran dry is not retried
// every few minutes.
func (s *OpenAIGatewayService) rateLimitGrokSpendingRefusal(ctx context.Context, account *Account) {
	if s == nil || account == nil {
		return
	}
	now := time.Now()
	s.rateLimitGrok(ctx, account, grokSpendingRefusalResetAt(account, now))
	billing, _ := grokBillingSnapshotFromExtra(account.Extra)
	if !grokBillingHasPrepaidHeadroom(billing) || s.grokBillingProbe == nil || !globalGrokSpendingRechecks.begin(account.ID, now) {
		return
	}
	refused := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type}
	go func() {
		recheckCtx, cancel := context.WithTimeout(context.Background(), grokQuotaUpstreamTimeout+10*time.Second)
		defer cancel()
		s.recheckGrokSpendingRefusal(recheckCtx, refused)
	}()
}

// recheckGrokSpendingRefusal probes the refused account's billing and, when
// the fresh weekly answer shows no prepaid or on-demand money, applies the
// lock a refusal without money gets.
func (s *OpenAIGatewayService) recheckGrokSpendingRefusal(ctx context.Context, account *Account) {
	if s == nil || s.grokBillingProbe == nil || account == nil {
		return
	}
	result, err := s.grokBillingProbe.ProbeBilling(ctx, account.ID)
	if err != nil || result == nil || result.Billing == nil {
		return
	}
	billing := result.Billing
	// Prepaid money rides the weekly (credits) answer; without a fresh one the
	// stored balance is not proven stale.
	if !grokBillingWeeklyFresh(billing) || grokBillingHasPrepaidHeadroom(billing) {
		return
	}
	now := time.Now()
	checked := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type,
		Extra: map[string]any{grokBillingExtraKey: billing}}
	resetAt := grokSpendingRefusalResetAt(checked, now)
	if !resetAt.After(now.Add(grokSpendingLimitProbeCooldown)) {
		return
	}
	s.rateLimitGrok(ctx, account, resetAt)
	slog.Info("grok_spending_refusal_lock_extended",
		"account_id", account.ID,
		"rate_limit_reset_at", resetAt.UTC(),
	)
}

// grokBillingWeeklyFresh reports whether the summary's weekly (credits) fields
// come from the probe that produced it rather than from an earlier snapshot.
func grokBillingWeeklyFresh(billing *xai.BillingSummary) bool {
	if billing == nil {
		return false
	}
	for _, window := range billing.FailedWindows {
		if window == "weekly" {
			return false
		}
	}
	return true
}

type grokSpendingRecheckStore struct {
	mu    sync.Mutex
	items map[int64]time.Time // account -> last billing re-check
}

var globalGrokSpendingRechecks = &grokSpendingRecheckStore{items: make(map[int64]time.Time)}

// begin reports whether a billing re-check may start for the account now, at
// most one per grokSpendingRecheckInterval.
func (s *grokSpendingRecheckStore) begin(accountID int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.items[accountID]; ok && now.Sub(last) < grokSpendingRecheckInterval {
		return false
	}
	for id, last := range s.items {
		if now.Sub(last) >= grokSpendingRecheckInterval {
			delete(s.items, id)
		}
	}
	s.items[accountID] = now
	return true
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

// grokSpendingRefusalResetAt returns the rate-limit horizon for an account xAI
// refused for spending limit or exhausted credits.
//
//   - Prepaid balance or on-demand cap left: every refusal locks only for the
//     probe cooldown. xAI refuses such accounts for a while and then serves
//     them again, so a lock until the weekly reset idles money that is still
//     there. A stale balance is caught by the billing re-check that follows
//     the refusal (see rateLimitGrokSpendingRefusal).
//   - Only monthly-limit headroom: the first refusal is probed, and a
//     confirmed repeat waits for the billing window reset, because xAI keeps
//     refusing such accounts once the weekly window is full.
//   - No money left: wait for the billing window reset.
func grokSpendingRefusalResetAt(account *Account, now time.Time) time.Time {
	if account != nil {
		billing, _ := grokBillingSnapshotFromExtra(account.Extra)
		if grokBillingHasPrepaidHeadroom(billing) {
			return now.Add(grokSpendingLimitProbeCooldown)
		}
		if grokBillingHasMonthlyHeadroom(billing) && !confirmGrokSpendingRefusal(account.ID, now) {
			return now.Add(grokSpendingLimitProbeCooldown)
		}
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
