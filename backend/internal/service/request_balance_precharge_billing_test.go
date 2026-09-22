//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRequestBalancePrechargeCanonicalBillingPipelines(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, billingFails := range []bool{false, true} {
			t.Run(protocol+map[bool]string{false: "/captured", true: "/billing-failed"}[billingFails], func(t *testing.T) {
				ctx, _, wallet, key := prechargeFixture(.2)
				key.Group.RateMultiplier = 1
				require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
				id := BalancePrechargeBillingID(ctx, key.UserID, key.ID)
				billing := &openAIRecordUsageBillingRepoStub{}
				if billingFails {
					billing.err = errors.New("billing unavailable")
				}
				usage := &openAIRecordUsageLogRepoStub{inserted: true}
				users := &openAIRecordUsageUserRepoStub{}
				var recordErr error
				worker := WrapBalancePrechargeTask(ctx, func(taskCtx context.Context) {
					if protocol == "openai" {
						svc := newOpenAIRecordUsageServiceForTest(usage, users, &openAIRecordUsageSubRepoStub{}, nil)
						svc.usageBillingRepo = billing
						recordErr = svc.RecordUsage(taskCtx, &OpenAIRecordUsageInput{
							APIKey: key, User: key.User, Account: &Account{ID: 4},
							Result: &OpenAIForwardResult{RequestID: "precharge-openai", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 500, OutputTokens: 100}},
						})
					} else {
						svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usage, billing, users, &openAIRecordUsageSubRepoStub{})
						recordErr = svc.RecordUsage(taskCtx, &RecordUsageInput{
							APIKey: key, User: key.User, Account: &Account{ID: 4},
							Result: &ForwardResult{RequestID: "precharge-anthropic", Model: "claude-sonnet-4-5", Usage: ClaudeUsage{InputTokens: 500, OutputTokens: 100}},
						})
					}
				})
				FinishBalancePrecharge(ctx)
				worker(context.Background())
				require.Equal(t, billingFails, recordErr != nil)
				require.Equal(t, 1, billing.calls)
				require.Equal(t, id, billing.lastCmd.BalancePrechargeID)
				require.Greater(t, billing.lastCmd.BalanceCost, 0.0)
				require.Zero(t, users.deductCalls, "reservation settlement must use canonical Apply, never a second legacy deduction")
				require.Equal(t, billingFails, HasBalancePrecharge(ctx))
				// The Apply spy intentionally leaves the fixture untouched. This
				// asserts lifecycle never issues a second refund after capture or
				// refunds a failed bill; SQL money arithmetic is tested separately.
				require.Len(t, wallet.holds, 1)
			})
		}
	}
}

func TestRequestBalancePrechargeZeroUsageRefunds(t *testing.T) {
	ctx, _, wallet, key := prechargeFixture(.2)
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, 200, nil)
	svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		APIKey: key, User: key.User, Account: &Account{ID: 4},
		Result: &OpenAIForwardResult{Model: "gpt-5.1"},
	}))
	FinishBalancePrecharge(ctx)
	require.InDelta(t, .2, wallet.balance, 1e-8)
	require.Empty(t, wallet.holds)
}

func TestRequestBalancePrechargeEligibility(t *testing.T) {
	for _, mode := range []string{"balance", "disabled", "simple", "subscription"} {
		t.Run(mode, func(t *testing.T) {
			ctx, policy, wallet, key := prechargeFixture(.1)
			cfg := &config.Config{}
			cache := &prechargeEligibilityCache{balance: .1}
			var subscription *UserSubscription
			if mode == "disabled" {
				policy.policy.Enabled = false
			}
			if mode == "simple" {
				cfg.RunMode = config.RunModeSimple
			}
			if mode == "subscription" {
				key.Group.SubscriptionType = SubscriptionTypeSubscription
				subscription = &UserSubscription{ID: 7}
			}
			svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			defer svc.Stop()
			require.NoError(t, svc.CheckBillingEligibility(ctx, key.User, key, key.Group, subscription, ""))
			if mode == "balance" {
				require.InDelta(t, 0, wallet.balance, 1e-8)
				require.InDelta(t, .1, wallet.frozen, 1e-8)
				cache.balance = 0
				require.NoError(t, svc.CheckBillingEligibility(ctx, key.User, key, key.Group, nil, ""), "same request retry must not reject its own held balance")
			} else {
				require.InDelta(t, .1, wallet.balance, 1e-8)
				require.Zero(t, wallet.frozen)
			}
			FinishBalancePrecharge(ctx)
		})
	}
}

type prechargeEligibilityCache struct {
	BillingCache
	balance float64
}

func (c *prechargeEligibilityCache) GetUserBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}

func (*prechargeEligibilityCache) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return &SubscriptionCacheData{Status: StatusActive, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestRequestBalancePrechargeWaitEligibilityIgnoresFrozenCacheAndCountsRPMOnce(t *testing.T) {
	first, policy, wallet, key := prechargeFixture(.1)
	require.NoError(t, ensureRequestBalancePrecharge(first, key.User, key))
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	second := newRequestBalancePrecharge(parent, policy, wallet, nil)
	cache := &prechargeEligibilityCache{balance: 0}
	rpm := &userRPMCacheStub{}
	key.User.RPMLimit = 500
	key.Group.RPMLimit = 500
	svc := NewBillingCacheService(cache, nil, nil, nil, rpm, nil, &config.Config{}, nil)
	defer svc.Stop()
	result := make(chan error, 1)
	go func() { result <- svc.CheckBillingEligibility(second, key.User, key, key.Group, nil, "") }()
	select {
	case err := <-result:
		t.Fatalf("frozen cache must not reject the queued request: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	FinishBalancePrecharge(first)
	require.NoError(t, <-result)
	require.Equal(t, int32(1), atomic.LoadInt32(&rpm.userCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&rpm.userGroupCalls))
	FinishBalancePrecharge(second)
}

func TestRequestBalancePrechargeWaitEligibilityPreservesMinimumAndDisabledPolicy(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "atomic-minimum", false: "disabled-cache"}[enabled], func(t *testing.T) {
			ctx, policy, wallet, key := prechargeFixture(.1)
			policy.policy.Enabled = enabled
			cfg := &config.Config{}
			cfg.Billing.MinimumBalanceReserve = .2
			svc := NewBillingCacheService(&prechargeEligibilityCache{balance: .1}, nil, nil, nil, nil, nil, cfg, nil)
			defer svc.Stop()
			require.ErrorIs(t, svc.CheckBillingEligibility(ctx, key.User, key, key.Group, nil, ""), ErrInsufficientBalance)
			require.Empty(t, wallet.holds)
			require.Zero(t, wallet.frozen)
		})
	}
}

type prechargeWaitRateCache struct {
	prechargeEligibilityCache
	exhausted atomic.Bool
}

func (c *prechargeWaitRateCache) GetAPIKeyRateLimit(context.Context, int64) (*APIKeyRateLimitCacheData, error) {
	now := time.Now().Unix()
	data := &APIKeyRateLimitCacheData{Window5h: now, Window1d: now, Window7d: now}
	if c.exhausted.Load() {
		data.Usage1d = 1
	}
	return data, nil
}

func TestRequestBalancePrechargeWaitRechecksQuotaBeforeForwarding(t *testing.T) {
	first, policy, wallet, key := prechargeFixture(.1)
	require.NoError(t, ensureRequestBalancePrecharge(first, key.User, key))
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	second := newRequestBalancePrecharge(parent, policy, wallet, nil)
	defer FinishBalancePrecharge(second)
	cache := &prechargeWaitRateCache{}
	key.RateLimit1d = 1
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	defer svc.Stop()
	result := make(chan error, 1)
	go func() { result <- svc.CheckBillingEligibility(second, key.User, key, key.Group, nil, "") }()
	select {
	case err := <-result:
		t.Fatalf("request must wait until settlement: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cache.exhausted.Store(true)
	FinishBalancePrecharge(first)
	require.ErrorIs(t, <-result, ErrAPIKeyRateLimit1dExceeded)
	FinishBalancePrecharge(second)
	require.Empty(t, wallet.holds, "rejection after waiting must refund the unsent request")
	require.InDelta(t, .1, wallet.balance, 1e-8)
}
