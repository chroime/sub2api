package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type wsPrechargeSettings struct{ service.SettingRepository }

func (*wsPrechargeSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyBalancePrecharge {
		return `{"enabled":true,"threshold":10,"amount":1}`, nil
	}
	return "", service.ErrSettingNotFound
}

// Keep persistence outside this lifecycle test while exercising the real policy,
// admission, request-context and worker-ownership implementations together.
type wsPrechargeWallet struct {
	service.UsageBillingRepository
	service.BillingCache
	mu      sync.Mutex
	balance float64
	holds   map[string]float64
}

func (w *wsPrechargeWallet) GetUserBalance(context.Context, int64) (float64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.balance, nil
}

func (w *wsPrechargeWallet) ReserveBalancePrecharge(_ context.Context, cmd *service.BalancePrechargeCommand) (*service.BalancePrechargeResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if held, ok := w.holds[cmd.ID]; ok {
		return &service.BalancePrechargeResult{Reserved: true, Amount: held, NewBalance: w.balance}, nil
	}
	if w.balance < cmd.Amount {
		return nil, service.ErrInsufficientBalance
	}
	w.balance -= cmd.Amount
	w.holds[cmd.ID] = cmd.Amount
	return &service.BalancePrechargeResult{Reserved: true, Amount: cmd.Amount, NewBalance: w.balance}, nil
}

func (w *wsPrechargeWallet) ReleaseBalancePrecharge(_ context.Context, id string, _ int64) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	amount, ok := w.holds[id]
	if ok {
		w.balance += amount
		delete(w.holds, id)
	}
	return ok, nil
}

func newWSPrechargeFixture(t *testing.T) (*balancePrechargeTurns, *wsPrechargeWallet, func(context.Context)) {
	t.Helper()
	wallet := &wsPrechargeWallet{balance: 5, holds: make(map[string]float64)}
	cfg := &config.Config{}
	settings := service.NewSettingService(&wsPrechargeSettings{}, cfg)
	gateway := service.NewOpenAIGatewayService(
		nil, nil, wallet, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil,
	)
	billing := service.NewBillingCacheService(wallet, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	key := &service.APIKey{ID: 2, UserID: 1, User: &service.User{ID: 1}}
	turns := newBalancePrechargeTurns(context.Background(), gateway.WithBalancePrecharge)
	return turns, wallet, func(ctx context.Context) {
		t.Helper()
		require.NotNil(t, ctx)
		require.NoError(t, billing.CheckBillingEligibility(ctx, key.User, key, nil, nil, ""))
	}
}

func TestBalancePrechargeTurnsKeepOverlappingCallbackOwnership(t *testing.T) {
	p, _, _ := newWSPrechargeFixture(t)
	first := p.context(1)
	second := p.context(2)
	require.Same(t, first, p.context(1), "a late callback must resolve its original reservation")
	require.True(t, p.retryAttempt())
	require.Same(t, second, p.context(1), "a late callback must not become the retry target")
}

func TestBalancePrechargeTurnsFinishEveryUncompletedReservation(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	admit(p.context(1))
	admit(p.context(2))
	p.finish()
	require.Equal(t, 5.0, wallet.balance)
	require.Empty(t, wallet.holds)
	p.finish()
	require.Equal(t, 5.0, wallet.balance)
}

func TestBalancePrechargeTurnsRetryKeepsActiveReservation(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	first := p.context(1)
	admit(first)
	second := p.context(2)
	admit(second)
	p.retryAttempt()
	require.Same(t, second, p.context(1))
	admit(p.context(1))
	require.Equal(t, 3.0, wallet.balance, "the retried turn must not freeze another hold")
	p.finish()
	require.Equal(t, 5.0, wallet.balance)
}

func TestBalancePrechargeTurnsLocalAdmissionFailureRefunds(t *testing.T) {
	for _, turn := range []int{1, 2} {
		t.Run(map[int]string{1: "first", 2: "followup"}[turn], func(t *testing.T) {
			p, wallet, admit := newWSPrechargeFixture(t)
			admit(p.context(1))
			localErr := errors.New("account concurrency rejected")
			hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{
				BeforeTurn: func(n int) error { admit(p.context(n)); return localErr },
			})
			if turn == 2 {
				// A previous zero-cost turn completed; the next admission fails
				// after reserving, before the forwarder has sent its payload.
				service.BalancePrechargeUsageFinished(p.context(1), nil)
				hooks.AfterTurn(1, &service.OpenAIForwardResult{}, nil)
				require.NoError(t, hooks.BeforeRequest(2, nil, "model"))
			}
			require.ErrorIs(t, hooks.BeforeTurn(turn), localErr)
			hooks.AfterTurn(turn, nil, localErr) // passthrough reports a rejected frame
			p.endAttempt(localErr)
			p.finish()
			require.Empty(t, wallet.holds)
			require.Equal(t, 5.0, wallet.balance)
		})
	}
}

func TestBalancePrechargeTurnsModelMappingFailureRefunds(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	admit(p.context(1))
	localErr := errors.New("model not supported")
	hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{MapRequestModel: func(int, string) (string, error) { return "", localErr }})
	require.NoError(t, hooks.BeforeTurn(1))
	_, err := hooks.MapRequestModel(1, "model")
	require.ErrorIs(t, err, localErr)
	p.endAttempt(err)
	p.finish()
	require.Equal(t, 5.0, wallet.balance)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeTurnsForwarderPolicyRejectionRefunds(t *testing.T) {
	for _, denied := range []error{
		&service.OpenAIFastBlockedError{Message: "fast policy denied"},
		&service.ReasoningEffortOverLimitError{Requested: "high", Max: "low"},
		&service.ReasoningEffortMappingDeniedError{Requested: "high"},
	} {
		p, wallet, admit := newWSPrechargeFixture(t)
		admit(p.context(1))
		p.trackAttempt(&service.OpenAIWSIngressHooks{})
		// Native and passthrough validation can reject before their first hooks.
		p.endAttempt(service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "policy denied", denied))
		p.finish()
		require.Equal(t, 5.0, wallet.balance)
		require.Empty(t, wallet.holds)
	}
}

func TestBalancePrechargeTurnsFailoverKeepsHoldUntilRetryFinishes(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	admit(p.context(1))
	id := service.BalancePrechargeBillingID(p.context(1), 1, 2)
	callbacks := 0
	baseHooks := &service.OpenAIWSIngressHooks{AfterTurn: func(turn int, result *service.OpenAIForwardResult, err error) {
		callbacks++
		if err == nil {
			service.BalancePrechargeUsageFinished(p.context(turn), nil)
		}
	}}
	first := p.trackAttempt(baseHooks)
	rejected := &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
	first.AfterTurn(1, nil, rejected)
	p.endAttempt(rejected)
	require.Equal(t, 4.0, wallet.balance, "a retryable rejection must not refund before retry is decided")
	require.True(t, p.retryAttempt())
	second := p.trackAttempt(baseHooks)
	require.Equal(t, id, service.BalancePrechargeBillingID(p.context(1), 1, 2))
	admit(p.context(1))
	require.Equal(t, 4.0, wallet.balance)
	first.AfterTurn(1, &service.OpenAIForwardResult{}, nil) // late old-connection callback
	require.Equal(t, 1, callbacks)
	second.AfterTurn(1, &service.OpenAIForwardResult{}, nil)
	second.AfterTurn(1, &service.OpenAIForwardResult{}, nil) // duplicate terminal callback
	require.Equal(t, 2, callbacks)
	p.endAttempt(nil)
	p.finish()
	require.Equal(t, 5.0, wallet.balance)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeTurnsExhaustedRejectedAttemptRefunds(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	admit(p.context(1))
	hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{})
	rejected := &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
	hooks.AfterTurn(1, nil, rejected)
	p.endAttempt(rejected)
	p.finish()
	require.Equal(t, 5.0, wallet.balance)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeTurnsDisconnectReleasesUnusedHoldWhenFinished(t *testing.T) {
	for _, err := range []error{context.Canceled, errors.New("connection reset"), nil} {
		p, wallet, admit := newWSPrechargeFixture(t)
		admit(p.context(1))
		p.trackAttempt(&service.OpenAIWSIngressHooks{})
		p.endAttempt(err) // no terminal callback means consumption is unproven
		require.Equal(t, 4.0, wallet.balance, "an ended connection may still be retried before the turn finishes")
		p.finish()
		require.Equal(t, 5.0, wallet.balance)
		require.Empty(t, wallet.holds)
	}
}

func TestBalancePrechargeTurnsPendingWorkerOwnsCompletedTurn(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	first := p.context(1)
	admit(first)
	var worker service.UsageRecordTask
	callbacks := 0
	hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{AfterTurn: func(turn int, _ *service.OpenAIForwardResult, _ error) {
		callbacks++
		worker = service.WrapBalancePrechargeTask(p.context(turn), func(ctx context.Context) { service.BalancePrechargeUsageFinished(ctx, nil) })
	}})
	hooks.AfterTurn(1, &service.OpenAIForwardResult{}, nil)
	require.False(t, p.retryAttempt(), "a completed request cannot be retried as a new first turn")
	require.NoError(t, hooks.BeforeRequest(2, nil, "model"))
	admit(p.context(2))
	hooks.AfterTurn(1, &service.OpenAIForwardResult{}, nil)
	hooks.AfterTurn(3, nil, context.Canceled) // adapter's idle-disconnect callback
	require.Equal(t, 1, callbacks)
	require.Nil(t, p.context(1), "a completed and retired callback must not recreate a reservation")
	p.endAttempt(context.Canceled)
	p.finish()
	require.Equal(t, 4.0, wallet.balance, "only the unforwarded second hold can be refunded before the worker finishes")
	require.Len(t, wallet.holds, 1)
	worker(context.Background())
	require.Equal(t, 5.0, wallet.balance)
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeTurnsStartsOnceForRepeatedBeforeTurn(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	admit(p.context(1))
	hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{})
	require.NoError(t, hooks.BeforeTurn(1))
	require.NoError(t, hooks.BeforeTurn(1))
	rejected := &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}
	hooks.AfterTurn(1, nil, rejected)
	p.endAttempt(rejected)
	p.finish()
	require.Equal(t, 5.0, wallet.balance, "unmatched duplicate starts would incorrectly retain this rejected request")
	require.Empty(t, wallet.holds)
}

func TestBalancePrechargeTurnsAuxiliaryUsagePreservesBillingWithoutInventingHold(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	first := p.context(1)
	admit(first)
	billed := 0
	hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{AfterTurn: func(turn int, _ *service.OpenAIForwardResult, _ error) {
		billed++
		require.Empty(t, service.BalancePrechargeBillingID(p.billingContext(turn), 1, 2))
	}})
	hooks.AfterTurn(2, &service.OpenAIForwardResult{RequestID: "auxiliary-event"}, nil)
	hooks.AfterTurn(2, &service.OpenAIForwardResult{RequestID: "auxiliary-event"}, nil)
	require.Equal(t, 1, billed)
	require.Equal(t, 4.0, wallet.balance)
	require.Len(t, wallet.holds, 1)
	p.endAttempt(&service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests})
	require.True(t, p.retryAttempt())
	require.Same(t, first, p.context(1), "auxiliary usage must not become the retry target")
}

func TestBalancePrechargeTurnsConcurrentNextAdmissionPreservesPriorWorker(t *testing.T) {
	p, wallet, admit := newWSPrechargeFixture(t)
	first := p.context(1)
	admit(first)
	firstID := service.BalancePrechargeBillingID(first, 1, 2)
	callbackEntered := make(chan struct{})
	continueCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	var worker service.UsageRecordTask
	var workerID string
	hooks := p.trackAttempt(&service.OpenAIWSIngressHooks{AfterTurn: func(turn int, _ *service.OpenAIForwardResult, _ error) {
		close(callbackEntered)
		<-continueCallback
		worker = service.WrapBalancePrechargeTask(p.billingContext(turn), func(ctx context.Context) {
			workerID = service.BalancePrechargeBillingID(ctx, 1, 2)
			service.BalancePrechargeUsageFinished(ctx, nil)
		})
	}})
	go func() {
		defer close(callbackDone)
		hooks.AfterTurn(1, &service.OpenAIForwardResult{}, nil)
	}()
	<-callbackEntered
	require.NoError(t, hooks.BeforeRequest(2, nil, "model"))
	admit(p.context(2))
	close(continueCallback)
	<-callbackDone
	p.endAttempt(context.Canceled)
	p.finish()
	require.Equal(t, 4.0, wallet.balance)
	worker(context.Background())
	require.Equal(t, firstID, workerID)
	require.Equal(t, 5.0, wallet.balance)
	require.Empty(t, wallet.holds)
}
