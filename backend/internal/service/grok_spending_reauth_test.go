//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

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
