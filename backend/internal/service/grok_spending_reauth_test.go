//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func grokBillingExtra(periodEnd, billingPeriodEnd string, productPercent *float64) map[string]any {
	billing := map[string]any{
		"period_type":        "weekly",
		"period_end":         periodEnd,
		"billing_period_end": billingPeriodEnd,
	}
	if productPercent != nil {
		billing["product_usage"] = []any{map[string]any{"product": "GrokBuild", "usage_percent": *productPercent}}
	}
	return map[string]any{grokBillingExtraKey: billing}
}

func TestGrokSpendingLimitResetAtUsesActiveWeeklyWindow(t *testing.T) {
	now := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
	weekEnd := now.Add(5 * 24 * time.Hour)
	account := &Account{Extra: grokBillingExtra(weekEnd.Format(time.RFC3339), "2026-11-01T00:00:00Z", nil)}

	require.True(t, grokSpendingLimitResetAt(account, now).Equal(weekEnd))
}

// Regression: an account whose weekly window had just rolled over was locked
// until the monthly invoice boundary (two weeks away) instead of being probed.
func TestGrokSpendingLimitResetAtElapsedWeekDoesNotFallBackToMonthEnd(t *testing.T) {
	now := time.Date(2026, 10, 16, 6, 21, 59, 0, time.UTC)
	elapsedWeekEnd := now.Add(-10 * time.Second)
	account := &Account{Extra: grokBillingExtra(elapsedWeekEnd.Format(time.RFC3339), "2026-11-01T00:00:00Z", nil)}

	require.Equal(t, now.Add(grokSpendingLimitProbeCooldown), grokSpendingLimitResetAt(account, now))
}

func TestGrokSpendingLimitResetAtMonthlyOnlyBillingKeepsInvoiceBoundary(t *testing.T) {
	now := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
	monthEnd := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	account := &Account{Extra: grokBillingExtra("", monthEnd.Format(time.RFC3339), nil)}

	require.True(t, grokSpendingLimitResetAt(account, now).Equal(monthEnd))
	require.Equal(t, now.Add(grokSpendingLimitProbeCooldown), grokSpendingLimitResetAt(&Account{}, now))
}

func TestGrokBillingWindowExhausted(t *testing.T) {
	full, partial := 100.0, 42.0
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)

	require.True(t, grokBillingWindowExhausted(&Account{Extra: grokBillingExtra(future, "", &full)}))
	require.False(t, grokBillingWindowExhausted(&Account{Extra: grokBillingExtra(future, "", &partial)}))
	require.False(t, grokBillingWindowExhausted(&Account{Extra: grokBillingExtra(past, "", &full)}), "a finished week is stale")
	require.False(t, grokBillingWindowExhausted(&Account{}))
}

func TestHandleGrokAccountUpstreamErrorAmbiguous403CoolsExhaustedAccount(t *testing.T) {
	full := 100.0
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 4801, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Extra: grokBillingExtra(time.Now().Add(72*time.Hour).Format(time.RFC3339), "", &full)}

	svc.handleGrokAccountUpstreamError(context.Background(), account, http.StatusForbidden, nil, []byte(`{"error":{"message":"Request forbidden"}}`))

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok usage exhausted (403)", repo.lastTempUnschedReason)
	require.WithinDuration(t, time.Now().Add(grokExhaustedForbiddenCooldown), repo.lastTempUnschedUntil, 2*time.Second)
	require.Zero(t, repo.rateLimitedCalls, "an exhausted-window 403 must not install a long rate limit")
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

// grokPrepaidBillingExtra is a full weekly window plus the given money fields.
func grokPrepaidBillingExtra(periodEnd string, money map[string]any) map[string]any {
	full := 100.0
	extra := grokBillingExtra(periodEnd, "", &full)
	billing := extra[grokBillingExtraKey].(map[string]any)
	billing["usage_percent"] = full
	for k, v := range money {
		billing[k] = v
	}
	return extra
}

func TestGrokBillingWindowExhaustedIgnoresFullWindowWhilePrepaidRemains(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	require.False(t, grokBillingWindowExhausted(&Account{Extra: grokPrepaidBillingExtra(future, map[string]any{"prepaid_balance": 1674.0})}))
	require.False(t, grokBillingWindowExhausted(&Account{Extra: grokPrepaidBillingExtra(future, map[string]any{"on_demand_cap": 500.0, "on_demand_used": 120.0})}))
	require.True(t, grokBillingWindowExhausted(&Account{Extra: grokPrepaidBillingExtra(future, map[string]any{"prepaid_balance": 0.0})}))
	require.True(t, grokBillingWindowExhausted(&Account{Extra: grokPrepaidBillingExtra(future, map[string]any{"on_demand_cap": 500.0, "on_demand_used": 500.0})}))
}

func TestHandleGrokAccountUpstreamErrorAmbiguous403KeepsPrepaidAccountSchedulable(t *testing.T) {
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 4803, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Extra: grokPrepaidBillingExtra(time.Now().Add(72*time.Hour).Format(time.RFC3339), map[string]any{"prepaid_balance": 1674.0})}

	svc.handleGrokAccountUpstreamError(context.Background(), account, http.StatusForbidden, nil, []byte(`{"error":{"message":"Request forbidden"}}`))

	require.Zero(t, repo.tempUnschedCalls, "prepaid money keeps the account in rotation")
	require.Zero(t, repo.rateLimitedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

// backdateGrokSpendingRefusal moves the recorded first refusal into the past,
// as if the probe cooldown had already run out.
func backdateGrokSpendingRefusal(t *testing.T, accountID int64, age time.Duration) {
	t.Helper()
	s := globalGrokSpendingRefusals
	s.mu.Lock()
	defer s.mu.Unlock()
	first, ok := s.items[accountID]
	require.True(t, ok, "a first refusal must be recorded")
	s.items[accountID] = first.Add(-age)
}

func TestGrokSpendingRefusalProbesFirstThenLocksUntilReset(t *testing.T) {
	const accountID = 4804
	t.Cleanup(func() { resetGrokSpendingRefusal(accountID) })
	weekEnd := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	body := []byte(`{"code":"x","error":"You have run out of credits or need a Grok subscription."}`)
	money := map[string]map[string]any{
		"prepaid":       {"prepaid_balance": 12294.0},
		"monthly limit": {"prepaid_balance": 0.0, "monthly_limit_cents": 283486.0, "used_cents": 0.0},
		"on-demand cap": {"on_demand_cap": 500.0, "on_demand_used": 120.0},
	}

	for name, fields := range money {
		for _, status := range []int{http.StatusPaymentRequired, http.StatusForbidden} {
			resetGrokSpendingRefusal(accountID)
			repo := &grokQuotaAccountRepo{}
			svc := &OpenAIGatewayService{accountRepo: repo}
			extra := grokPrepaidBillingExtra(weekEnd.Format(time.RFC3339), fields)
			newAccount := func() *Account {
				return &Account{ID: accountID, Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: extra}
			}

			svc.handleGrokAccountUpstreamError(context.Background(), newAccount(), status, nil, body)
			require.Equal(t, 1, repo.rateLimitedCalls, "%s/%d", name, status)
			require.WithinDuration(t, time.Now().Add(grokSpendingLimitProbeCooldown), repo.lastRateLimitResetAt, 2*time.Second,
				"%s/%d: first refusal with money left is only a probe cooldown", name, status)

			// Requests already in flight fail in the same burst; they must not
			// count as the confirming repeat.
			svc.handleGrokAccountUpstreamError(context.Background(), newAccount(), status, nil, body)
			require.WithinDuration(t, time.Now().Add(grokSpendingLimitProbeCooldown), repo.lastRateLimitResetAt, 2*time.Second,
				"%s/%d: a refusal in the same burst keeps the probe cooldown", name, status)

			backdateGrokSpendingRefusal(t, accountID, grokSpendingLimitProbeCooldown+time.Second)
			svc.handleGrokAccountUpstreamError(context.Background(), newAccount(), status, nil, body)
			require.True(t, repo.lastRateLimitResetAt.Equal(weekEnd),
				"%s/%d: a refusal after the probe cooldown trusts xAI and waits for the weekly reset", name, status)
		}
	}
}

func TestGrokSpendingRefusalWithoutMoneyLocksUntilResetImmediately(t *testing.T) {
	const accountID = 4805
	t.Cleanup(func() { resetGrokSpendingRefusal(accountID) })
	weekEnd := time.Now().Add(72 * time.Hour).Truncate(time.Second)

	for name, fields := range map[string]map[string]any{
		"no money fields": {"prepaid_balance": 0.0},
		"monthly used up": {"prepaid_balance": 0.0, "monthly_limit_cents": 4000.0, "used_cents": 4016.0},
		"on-demand spent": {"on_demand_cap": 500.0, "on_demand_used": 500.0},
	} {
		resetGrokSpendingRefusal(accountID)
		account := &Account{ID: accountID, Extra: grokPrepaidBillingExtra(weekEnd.Format(time.RFC3339), fields)}
		require.True(t, grokSpendingRefusalResetAt(account, time.Now()).Equal(weekEnd), name)
	}
}

// Monthly headroom only earns a probe on explicit credit refusals. An ambiguous
// 403 on a full window still cools the account, because xAI may keep refusing
// every request and it bills for 403s.
func TestGrokBillingWindowExhaustedIgnoresMonthlyHeadroom(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	account := &Account{Extra: grokPrepaidBillingExtra(future, map[string]any{
		"prepaid_balance": 0.0, "monthly_limit_cents": 283486.0, "used_cents": 0.0,
	})}

	require.True(t, grokBillingWindowExhausted(account))
	require.True(t, grokAccountMayStillPay(account))
}

func TestGrokSpendingRefusalConfirmation(t *testing.T) {
	const accountID = 4806
	t.Cleanup(func() { resetGrokSpendingRefusal(accountID) })
	now := time.Now()
	afterProbe := grokSpendingLimitProbeCooldown + time.Minute

	resetGrokSpendingRefusal(accountID)
	require.False(t, confirmGrokSpendingRefusal(accountID, now))
	require.False(t, confirmGrokSpendingRefusal(accountID, now.Add(time.Minute)), "same burst")
	require.True(t, confirmGrokSpendingRefusal(accountID, now.Add(afterProbe)), "a refusal after the probe confirms")
	require.False(t, confirmGrokSpendingRefusal(accountID, now.Add(afterProbe+time.Second)), "confirmation consumes the record")

	resetGrokSpendingRefusal(accountID)
	require.False(t, confirmGrokSpendingRefusal(accountID, now))
	resetGrokSpendingRefusal(accountID) // a request succeeded in between
	require.False(t, confirmGrokSpendingRefusal(accountID, now.Add(afterProbe)), "success starts a fresh probe")

	resetGrokSpendingRefusal(accountID)
	require.False(t, confirmGrokSpendingRefusal(accountID, now))
	require.False(t, confirmGrokSpendingRefusal(accountID, now.Add(grokSpendingRefusalConfirmWindow+time.Minute)), "an old refusal no longer confirms")
}

func TestHandleGrokAccountUpstreamErrorAmbiguous403PoolModeStaysUntouched(t *testing.T) {
	full := 100.0
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 4802, Platform: PlatformGrok, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"pool_mode": true},
		Extra:       grokBillingExtra(time.Now().Add(72*time.Hour).Format(time.RFC3339), "", &full)}

	svc.handleGrokAccountUpstreamError(context.Background(), account, http.StatusForbidden, nil, []byte(`Forbidden`))

	require.Zero(t, repo.tempUnschedCalls)
}

// grokSpendingLockedAccount is a Grok OAuth account rate-limited until resetAt.
func grokSpendingLockedAccount(id int64, resetAt time.Time) *Account {
	limitedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	return &Account{ID: id, Platform: PlatformGrok, Type: AccountTypeOAuth, RateLimitedAt: &limitedAt, RateLimitResetAt: &resetAt}
}

func grokWeeklyBillingSummary(periodEnd time.Time, usage *float64) *xai.BillingSummary {
	return &xai.BillingSummary{
		PeriodType:       "weekly",
		UsagePercent:     usage,
		PeriodEnd:        periodEnd.Format(time.RFC3339),
		BillingPeriodEnd: periodEnd.Add(20 * 24 * time.Hour).Format(time.RFC3339),
	}
}

func TestReleaseGrokSpendingLockIfServable(t *testing.T) {
	const accountID = 4811
	t.Cleanup(func() { globalGrokSpendingLockReleases.forget(accountID) })
	now := time.Now()
	weekEnd := now.Add(72 * time.Hour).Truncate(time.Second)
	monthEnd := weekEnd.Add(20 * 24 * time.Hour)

	cases := []struct {
		name    string
		resetAt time.Time
		billing func() *xai.BillingSummary
		want    bool
	}{
		{"prepaid with a full week", weekEnd, func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(100))
			b.PrepaidBalance = floatPtr(427)
			return b
		}, true},
		{"on-demand left, window end drifted", weekEnd.Add(-3 * time.Second), func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(100))
			b.OnDemandCap, b.OnDemandUsed = floatPtr(500), floatPtr(120)
			return b
		}, true},
		{"weekly window has room, no money", weekEnd, func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(26))
			b.ProductUsage = []xai.BillingProductSummary{{Product: "GrokBot", UsagePercent: floatPtr(20)}, {Product: "Api", UsagePercent: floatPtr(6)}}
			return b
		}, true},
		{"stale lock past the current window", monthEnd, func() *xai.BillingSummary {
			return grokWeeklyBillingSummary(weekEnd, floatPtr(26))
		}, true},
		{"monthly-only billing with prepaid", monthEnd, func() *xai.BillingSummary {
			return &xai.BillingSummary{PeriodType: "monthly", BillingPeriodEnd: monthEnd.Format(time.RFC3339), PrepaidBalance: floatPtr(100)}
		}, true},
		{"monthly headroom only", weekEnd, func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(100))
			b.MonthlyLimitCents, b.UsedCents = floatPtr(283486), floatPtr(0)
			return b
		}, false},
		{"full week, no money", weekEnd, func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(100))
			b.PrepaidBalance = floatPtr(0)
			return b
		}, false},
		{"one product exhausted", weekEnd, func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(40))
			b.ProductUsage = []xai.BillingProductSummary{{Product: "GrokBuild", UsagePercent: floatPtr(100)}}
			return b
		}, false},
		{"no weekly usage observed", weekEnd, func() *xai.BillingSummary {
			return grokWeeklyBillingSummary(weekEnd, nil)
		}, false},
		{"short lock with prepaid", now.Add(grokSpendingLimitProbeCooldown), func() *xai.BillingSummary {
			b := grokWeeklyBillingSummary(weekEnd, floatPtr(100))
			b.PrepaidBalance = floatPtr(427)
			return b
		}, false},
		{"lock already over", now.Add(-time.Minute), func() *xai.BillingSummary {
			return grokWeeklyBillingSummary(weekEnd, floatPtr(26))
		}, false},
	}
	for _, tc := range cases {
		globalGrokSpendingLockReleases.forget(accountID)
		repo := &grokQuotaAccountRepo{recoveryClearResult: true}
		account := grokSpendingLockedAccount(accountID, tc.resetAt)

		got := releaseGrokSpendingLockIfServable(context.Background(), repo, account, tc.billing(), now)

		require.Equal(t, tc.want, got, tc.name)
		if !tc.want {
			require.Zero(t, repo.recoveryClearCalls, tc.name)
			continue
		}
		require.Equal(t, 1, repo.recoveryClearCalls, tc.name)
		require.True(t, repo.recoveryObservedAt.Equal(*account.RateLimitedAt), tc.name)
		require.True(t, repo.recoveryObservedReset.Equal(tc.resetAt), tc.name)
	}
}

// An account xAI keeps refusing must not be released on every billing refresh.
func TestReleaseGrokSpendingLockOncePerWindowUnlessMoneyGrows(t *testing.T) {
	const accountID = 4812
	t.Cleanup(func() { globalGrokSpendingLockReleases.forget(accountID) })
	globalGrokSpendingLockReleases.forget(accountID)
	now := time.Now()
	weekEnd := now.Add(72 * time.Hour).Truncate(time.Second)
	billing := func(end time.Time, prepaid float64) *xai.BillingSummary {
		b := grokWeeklyBillingSummary(end, floatPtr(100))
		b.PrepaidBalance = floatPtr(prepaid)
		return b
	}
	release := func(resetAt time.Time, b *xai.BillingSummary) (bool, int) {
		repo := &grokQuotaAccountRepo{recoveryClearResult: true}
		released := releaseGrokSpendingLockIfServable(context.Background(), repo, grokSpendingLockedAccount(accountID, resetAt), b, now)
		return released, repo.recoveryClearCalls
	}

	released, _ := release(weekEnd, billing(weekEnd, 427))
	require.True(t, released)

	released, clears := release(weekEnd, billing(weekEnd, 427))
	require.False(t, released, "relocked with the same money in the same window")
	require.Zero(t, clears)

	released, _ = release(weekEnd, billing(weekEnd, 5000))
	require.True(t, released, "a top-up earns another release")

	nextWeekEnd := weekEnd.Add(7 * 24 * time.Hour)
	released, _ = release(nextWeekEnd, billing(nextWeekEnd, 5000))
	require.True(t, released, "a new billing window starts over")
}

func TestReleaseGrokSpendingLockRecordsOnlyClearedLocks(t *testing.T) {
	const accountID = 4813
	t.Cleanup(func() {
		globalGrokSpendingLockReleases.forget(accountID)
		resetGrokSpendingRefusal(accountID)
	})
	globalGrokSpendingLockReleases.forget(accountID)
	now := time.Now()
	weekEnd := now.Add(72 * time.Hour).Truncate(time.Second)
	b := grokWeeklyBillingSummary(weekEnd, floatPtr(26))

	// The lock changed underneath (CAS miss): nothing released, nothing recorded.
	missRepo := &grokQuotaAccountRepo{recoveryClearResult: false}
	require.False(t, releaseGrokSpendingLockIfServable(context.Background(), missRepo, grokSpendingLockedAccount(accountID, weekEnd), b, now))
	require.Equal(t, 1, missRepo.recoveryClearCalls)

	// A release forgets the pending refusal, so the next refusal starts a fresh
	// probe instead of confirming the old one.
	resetGrokSpendingRefusal(accountID)
	require.False(t, confirmGrokSpendingRefusal(accountID, now))
	repo := &grokQuotaAccountRepo{recoveryClearResult: true}
	require.True(t, releaseGrokSpendingLockIfServable(context.Background(), repo, grokSpendingLockedAccount(accountID, weekEnd), b, now))
	require.False(t, confirmGrokSpendingRefusal(accountID, now.Add(grokSpendingLimitProbeCooldown+time.Minute)))
}

// grokBillingBodiesUpstream answers the weekly (credits) and monthly billing
// endpoints with fixed bodies.
type grokBillingBodiesUpstream struct {
	httpUpstreamRecorder
	weekly, monthly string
}

func (u *grokBillingBodiesUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body := u.monthly
	if req.URL.RawQuery == "format=credits" {
		body = u.weekly
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestGrokQuotaServiceProbeBillingReleasesSpendingLockWhilePrepaidRemains(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(4814)
	t.Cleanup(func() { globalGrokSpendingLockReleases.forget(account.ID) })
	globalGrokSpendingLockReleases.forget(account.ID)
	weekEnd := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	limitedAt := time.Now().Add(-8 * time.Hour).Truncate(time.Second)
	account.RateLimitedAt, account.RateLimitResetAt = &limitedAt, &weekEnd
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{account.ID: account}},
		recoveryClearResult:        true,
	}
	upstream := &grokBillingBodiesUpstream{
		weekly: `{"config":{"currentPeriod":{"type":"WEEKLY","start":"` + weekEnd.Add(-7*24*time.Hour).Format(time.RFC3339) +
			`","end":"` + weekEnd.Format(time.RFC3339) + `"},"creditUsagePercent":100,"prepaidBalance":{"val":427}}}`,
		monthly: `{"config":{"monthlyLimit":{"val":30589},"used":{"val":30755},"billingPeriodStart":"2026-11-01T00:00:00Z","billingPeriodEnd":"2026-12-01T00:00:00Z"}}`,
	}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream, nil)

	_, err := svc.ProbeBilling(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, 1, repo.recoveryClearCalls)
	require.True(t, repo.recoveryObservedAt.Equal(limitedAt))
	require.True(t, repo.recoveryObservedReset.Equal(weekEnd))
}
