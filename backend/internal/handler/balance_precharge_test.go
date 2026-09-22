package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBalancePrechargeLifecycleRouteClassification(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/responses", "/backend-api/codex/responses", "/antigravity/v1/messages", "/v1beta/models/gemini:streamGenerateContent", "/images/generations", "/v1/tts"} {
		require.True(t, balancePrechargeRequestPath("POST", path), path)
	}
	for _, path := range []string{"/v1/messages/count_tokens", "/responses/input_tokens", "/v1beta/models/gemini:countTokens", "/v1/images/generations/async", "/v1/images/batches", "/v1/videos/generations", "/v1/live"} {
		require.False(t, balancePrechargeRequestPath("POST", path), path)
	}
	require.False(t, balancePrechargeRequestPath("GET", "/v1/responses"))
}

func TestBalancePrechargeHTTPAdmissionAndRefund(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, balance := range []float64{.5, 5} {
		wallet := &wsPrechargeWallet{balance: balance, holds: make(map[string]float64)}
		cfg := &config.Config{}
		settings := service.NewSettingService(&wsPrechargeSettings{}, cfg)
		gateway := service.NewGatewayService(
			nil, nil, nil, wallet, nil, nil, nil, nil, cfg,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, settings, nil, nil, nil, nil, nil, nil,
		)
		billing := service.NewBillingCacheService(wallet, nil, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(billing.Stop)
		h := &GatewayHandler{gatewayService: gateway}
		router := gin.New()
		forwarded := false
		key := &service.APIKey{ID: 2, UserID: 1, User: &service.User{ID: 1}}
		router.POST("/v1/responses", h.BalancePrechargeLifecycle(), func(c *gin.Context) {
			ctx := c.Request.Context()
			if err := billing.CheckBillingEligibility(ctx, key.User, key, nil, nil, ""); err != nil {
				c.Status(http.StatusPaymentRequired)
				return
			}
			require.Len(t, wallet.holds, 1, "funds must already be frozen before contacting upstream")
			forwarded = true
			service.StartBalancePrechargeUpstream(ctx)
			service.ObserveBalancePrechargeUpstream(ctx, http.StatusBadRequest, nil)
			c.Status(http.StatusBadRequest)
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
		require.Equal(t, balance >= 1, forwarded)
		require.Equal(t, balance, wallet.balance, "admission failure or explicit upstream rejection leaves no debit")
		require.Empty(t, wallet.holds)
	}
}

func TestBalancePrechargeUsageCannotBeDropped(t *testing.T) {
	for _, gateway := range []string{"openai", "anthropic"} {
		t.Run(gateway, func(t *testing.T) {
			p, wallet, admit := newWSPrechargeFixture(t)
			ctx := p.context(1)
			admit(ctx)
			pool := newStoppedUsageRecordPoolForTest()
			called := false
			task := func(worker context.Context) {
				called = true
				require.Equal(t, service.BalancePrechargeBillingID(ctx, 1, 2), service.BalancePrechargeBillingID(worker, 1, 2))
				service.BalancePrechargeUsageFinished(worker, nil)
			}
			if gateway == "openai" {
				(&OpenAIGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask(ctx, task)
			} else {
				(&GatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask(ctx, task)
			}
			p.finish()
			require.True(t, called)
			require.Equal(t, 5.0, wallet.balance)
			require.Empty(t, wallet.holds)
		})
	}
}

func TestBalancePrechargeUsageOverflowAlwaysRuns(t *testing.T) {
	for _, alreadyCaptured := range []bool{false, true} {
		p, _, admit := newWSPrechargeFixture(t)
		ctx := p.context(1)
		admit(ctx)
		if alreadyCaptured {
			service.MarkBalancePrechargeSettled(ctx)
		}
		pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
			WorkerCount: 1, QueueSize: 1, TaskTimeout: time.Second, OverflowPolicy: "drop",
		})
		blocked, release := make(chan struct{}), make(chan struct{})
		pool.Submit(func(context.Context) { close(blocked); <-release })
		<-blocked
		pool.Submit(func(context.Context) { <-release })
		for _, submit := range []func(context.Context, service.UsageRecordTask){
			(&OpenAIGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask,
			(&GatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask,
		} {
			called := false
			submit(ctx, func(worker context.Context) { called = true; service.BalancePrechargeUsageFinished(worker, nil) })
			require.True(t, called, "money-critical work must run synchronously when the pool is full")
		}
		close(release)
		pool.Stop()
		p.finish()
	}
}
