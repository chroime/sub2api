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
