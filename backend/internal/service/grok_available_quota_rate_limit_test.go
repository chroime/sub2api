//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func availableGrokQuotaHeadersForTest() http.Header {
	return http.Header{
		"X-Ratelimit-Limit-Requests":     {"8300"},
		"X-Ratelimit-Remaining-Requests": {"8300"},
		"X-Ratelimit-Limit-Tokens":       {"53000000"},
		"X-Ratelimit-Remaining-Tokens":   {"53000000"},
		"Retry-After":                    {"45"},
	}
}

func TestGrokAvailableQuota429KeepsOtherModelsSchedulable(t *testing.T) {
	account := &Account{ID: 93082, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	ctx := withGrokTeamRateLimitModel(context.Background(), "grok-4.5")
	svc.handleGrokAccountUpstreamError(ctx, account, http.StatusTooManyRequests,
		availableGrokQuotaHeadersForTest(), []byte(`{"error":{"message":"rate limit exceeded"}}`))

	require.Zero(t, repo.rateLimitedCalls, "remaining quota must not become an account-wide pause")
	require.Zero(t, repo.tempUnschedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, 1, repo.updateCalls, "retain the upstream observation")
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now()))
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.7", time.Now()))
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now().Add(46*time.Second)),
		"honor Retry-After instead of adding an arbitrary longer pause")
}

func TestGrokAvailableQuota429RequiresCompletePositiveEvidence(t *testing.T) {
	for i, tc := range []struct {
		name string
		edit func(http.Header)
	}{
		{"requests exhausted", func(h http.Header) { h.Set("X-Ratelimit-Remaining-Requests", "0") }},
		{"tokens exhausted", func(h http.Header) { h.Set("X-Ratelimit-Remaining-Tokens", "0") }},
		{"missing token window", func(h http.Header) { h.Del("X-Ratelimit-Remaining-Tokens") }},
		{"missing request limit", func(h http.Header) { h.Del("X-Ratelimit-Limit-Requests") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := availableGrokQuotaHeadersForTest()
			tc.edit(h)
			account := &Account{ID: 93100 + int64(i), Platform: PlatformGrok, Type: AccountTypeOAuth}
			repo := &grokQuotaAccountRepo{}
			svc := &OpenAIGatewayService{accountRepo: repo}
			svc.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"),
				account, http.StatusTooManyRequests, h, []byte(`{"error":"rate limit exceeded"}`))
			require.Equal(t, 1, repo.rateLimitedCalls, "retain account protection when quota is exhausted or unknown")
		})
	}
}

func TestGrokAvailableQuota429DoesNotOverrideExplicitExhaustion(t *testing.T) {
	account := &Account{ID: 93110, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"),
		account, http.StatusTooManyRequests, availableGrokQuotaHeadersForTest(),
		[]byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"included usage exhausted"}}`))
	require.Positive(t, repo.rateLimitedCalls, "body-reported exhaustion remains authoritative")
}

func TestGrokAvailableQuota429HonorsLongRetryAfterWithoutQuotaFloor(t *testing.T) {
	account := &Account{ID: 93111, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	headers := availableGrokQuotaHeadersForTest()
	headers.Set("Retry-After", "600")
	svc.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"),
		account, http.StatusTooManyRequests, headers, []byte(`{"error":"rate limit exceeded"}`))
	require.Zero(t, repo.rateLimitedCalls)
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now().Add(6*time.Minute)))
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now().Add(11*time.Minute)))
	require.False(t, shouldMarkGrokTeamModelRateLimit(http.StatusTooManyRequests,
		[]byte(`{"error":"rate limit exceeded"}`), headers), "do not extend this into a team-wide ten-minute pause")
}

func TestGrokAvailableQuota429ActiveProbeKeepsOtherModelsSchedulable(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(93120)
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
		accountsByID: map[int64]*Account{account.ID: account},
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests, Header: availableGrokQuotaHeadersForTest(),
		Body: io.NopCloser(strings.NewReader(`{"error":"rate limit exceeded"}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream, nil)
	result, err := svc.ProbeUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, result.StatusCode)
	require.Zero(t, repo.rateLimitedCalls)
	require.True(t, isGrokModelQuotaBlocked(account.ID, result.Model, time.Now()))
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.7", time.Now()))
}

func TestGrokAvailableQuota429AccountTestKeepsOtherModelsSchedulable(t *testing.T) {
	account := &Account{ID: 93121, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &AccountTestService{accountRepo: repo}
	svc.observeGrokTestResponse(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), account,
		&http.Response{StatusCode: http.StatusTooManyRequests, Header: availableGrokQuotaHeadersForTest(),
			Body: io.NopCloser(strings.NewReader(`{"error":"rate limit exceeded"}`))})
	require.Zero(t, repo.rateLimitedCalls)
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now()))
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.7", time.Now()))
}
