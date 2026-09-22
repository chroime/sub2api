package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

type balancePrechargeSettingsReader interface {
	GetEffectiveBalancePrechargeSettings(context.Context, int64) (BalancePrechargeSettings, error)
}
type balancePrechargeContextKey struct{}

// requestBalancePrecharge belongs to one billable request, including its detached
// usage workers. The durable ledger, not this process-local state, owns money.
type requestBalancePrecharge struct {
	mu                                                      sync.Mutex
	settings                                                balancePrechargeSettingsReader
	repo                                                    BalancePrechargeRepository
	waiter                                                  *balancePrechargeWaiter
	command                                                 *BalancePrechargeCommand
	invalidate                                              func(context.Context, int64) error
	id                                                      string
	userID, apiKeyID                                        int64
	checked, reserved, settled                              bool
	finished, usageOK, billingFailed                        bool
	upstreamAttempted, upstreamSucceeded, upstreamUncertain bool
	upstreamPending                                         int
	pending                                                 int
	reviewLogged                                            bool
	reviewPersisted                                         bool
	reviewRetryStarted                                      bool
	reviewRetryer                                           *balancePrechargeReviewRetryer
	reviewRetryCancel                                       context.CancelFunc
	failureEvidence                                         BalancePrechargeFailureEvidence
	leaseCancel                                             context.CancelFunc
	leaseInterval                                           time.Duration
}

func newRequestBalancePrecharge(ctx context.Context, settings balancePrechargeSettingsReader, repo BalancePrechargeRepository, invalidate func(context.Context, int64) error) context.Context {
	return context.WithValue(ctx, balancePrechargeContextKey{}, &requestBalancePrecharge{settings: settings, repo: repo, invalidate: invalidate, waiter: defaultBalancePrechargeWaiter, reviewRetryer: defaultBalancePrechargeReviewRetryer})
}

func (s *GatewayService) WithBalancePrecharge(ctx context.Context) context.Context {
	if s == nil || s.settingService == nil || (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) {
		return ctx
	}
	repo, _ := s.usageBillingRepo.(BalancePrechargeRepository)
	var invalidate func(context.Context, int64) error
	if s.billingCacheService != nil {
		invalidate = s.billingCacheService.InvalidateUserBalance
	}
	return newRequestBalancePrecharge(ctx, s.settingService, repo, invalidate)
}

func (s *OpenAIGatewayService) WithBalancePrecharge(ctx context.Context) context.Context {
	if s == nil || s.settingService == nil || (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) {
		return ctx
	}
	repo, _ := s.usageBillingRepo.(BalancePrechargeRepository)
	var invalidate func(context.Context, int64) error
	if s.billingCacheService != nil {
		invalidate = s.billingCacheService.InvalidateUserBalance
	}
	return newRequestBalancePrecharge(ctx, s.settingService, repo, invalidate)
}

func requestPrecharge(ctx context.Context) *requestBalancePrecharge {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(balancePrechargeContextKey{}).(*requestBalancePrecharge)
	return p
}

func CopyBalancePrechargeContext(parent, base context.Context) context.Context {
	if p := requestPrecharge(parent); p != nil {
		return context.WithValue(base, balancePrechargeContextKey{}, p)
	}
	return base
}

// prepareRequestBalancePrecharge snapshots policy before the cached balance
// check. Enabled admissions check the real wallet atomically, including frozen
// funds and the configured minimum, instead of rejecting a stale/zero cache.
func prepareRequestBalancePrecharge(ctx context.Context, user *User, key *APIKey, minimum float64) (bool, error) {
	p := requestPrecharge(ctx)
	if p == nil || user == nil || key == nil {
		return false, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.prepareLocked(ctx, user, key, minimum)
}

func (p *requestBalancePrecharge) prepareLocked(ctx context.Context, user *User, key *APIKey, minimum float64) (bool, error) {
	if p.checked {
		return p.reserved && !p.settled, nil
	}
	if p.command != nil {
		return true, nil
	}
	groupID := int64(0)
	if key.GroupID != nil {
		groupID = *key.GroupID
	}
	policy, err := p.settings.GetEffectiveBalancePrechargeSettings(ctx, groupID)
	if err != nil {
		return false, ErrBillingServiceUnavailable.WithCause(err)
	}
	if !policy.Enabled {
		p.checked = true
		return false, nil
	}
	if p.repo == nil {
		return false, ErrBillingServiceUnavailable.WithCause(fmt.Errorf("balance precharge ledger is unavailable"))
	}
	if p.id == "" {
		p.id = uuid.NewString()
	}
	p.command = &BalancePrechargeCommand{ID: p.id, UserID: user.ID, APIKeyID: key.ID, GroupID: groupID, Threshold: policy.Threshold, Amount: policy.Amount, MinimumBalance: minimum, LeaseOwner: uuid.NewString()}
	return true, nil
}

func ensureRequestBalancePrecharge(ctx context.Context, user *User, key *APIKey) error {
	p := requestPrecharge(ctx)
	if p == nil || user == nil || key == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.checked {
		return nil
	}
	if enabled, err := p.prepareLocked(ctx, user, key, 0); err != nil || !enabled {
		return err
	}
	result, err := p.waiter.reserve(ctx, p.repo, p.command)
	if err != nil {
		var applicationError *infraerrors.ApplicationError
		if !errors.As(err, &applicationError) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return ErrBillingServiceUnavailable.WithCause(err)
		}
		return err
	}
	if result == nil {
		return ErrBillingServiceUnavailable.WithCause(fmt.Errorf("balance precharge returned no result"))
	}
	p.checked = true
	p.userID = user.ID
	p.apiKeyID = key.ID
	p.reserved = result.Reserved
	if result.Reserved {
		p.startLeaseLocked()
		p.invalidateBalance()
	}
	// A cancellation can race a successful COMMIT. Own and release that hold
	// before returning, so a disconnected queued request never reaches upstream.
	if err := ctx.Err(); err != nil {
		p.finished = true
		p.finishLocked()
		return err
	}
	return nil
}

func HasBalancePrecharge(ctx context.Context) bool {
	p := requestPrecharge(ctx)
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reserved && !p.settled
}

// All usage events belonging to a precharged request are mandatory, including
// events recorded after an earlier event captured its hold (e.g. a billed retry).
func IsBalancePrechargeRequest(ctx context.Context) bool {
	p := requestPrecharge(ctx)
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reserved
}

func BalancePrechargeBillingID(ctx context.Context, userID, apiKeyID int64) string {
	p := requestPrecharge(ctx)
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reserved && p.userID == userID && p.apiKeyID == apiKeyID {
		return p.id
	}
	return ""
}

func MarkBalancePrechargeSettled(ctx context.Context) {
	p := requestPrecharge(ctx)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.settled = true
	p.stopLeaseLocked()
	if p.reviewRetryCancel != nil {
		p.reviewRetryCancel()
	}
	p.invalidateBalance()
	p.waiter.notify(p.userID)
}

func retainBalancePrechargeTask(ctx context.Context) func() {
	p := requestPrecharge(ctx)
	if p == nil {
		return func() {}
	}
	p.mu.Lock()
	p.pending++
	p.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { p.mu.Lock(); defer p.mu.Unlock(); p.pending--; p.finishLocked() }) }
}

// WrapBalancePrechargeTask claims lifecycle ownership before enqueueing. A caller
// with a reservation must use mandatory/synchronous overflow handling.
func WrapBalancePrechargeTask(parent context.Context, task UsageRecordTask) UsageRecordTask {
	if task == nil {
		return nil
	}
	done := retainBalancePrechargeTask(parent)
	return func(ctx context.Context) {
		defer done()
		defer func() {
			if recovered := recover(); recovered != nil {
				BalancePrechargeUsageFinished(parent, fmt.Errorf("usage task panic: %v", recovered))
				panic(recovered)
			}
		}()
		task(CopyBalancePrechargeContext(parent, ctx))
	}
}

func BalancePrechargeUsageFinished(ctx context.Context, err error) {
	p := requestPrecharge(ctx)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.billingFailed = true
	} else {
		p.usageOK = true
	}
}

func finishBalancePrechargeUsage(ctx context.Context, err *error) {
	if recovered := recover(); recovered != nil {
		BalancePrechargeUsageFinished(ctx, fmt.Errorf("usage billing panic: %v", recovered))
		panic(recovered)
	}
	BalancePrechargeUsageFinished(ctx, *err)
}

func StartBalancePrechargeUpstream(ctx context.Context) {
	p := requestPrecharge(ctx)
	if p == nil {
		return
	}
	p.mu.Lock()
	p.upstreamAttempted = true
	p.upstreamPending++
	p.mu.Unlock()
}

func ObserveBalancePrechargeUpstream(ctx context.Context, status int, err error) {
	p := requestPrecharge(ctx)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.upstreamPending > 0 {
		p.upstreamPending--
	}
	if err != nil || status >= 500 {
		p.upstreamUncertain = true
	}
	if status >= 200 && status < 300 {
		p.upstreamSucceeded = true
	}
}

// Record only a fixed failure category and accounting identifiers for diagnosis.
// Upstream failure evidence alone does not create an accounting failure.
func SetBalancePrechargeFailureEvidence(ctx context.Context, evidence BalancePrechargeFailureEvidence) {
	p := requestPrecharge(ctx)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.upstreamUncertain = true
	evidence.Reason = prechargeEvidenceText(evidence.Reason, 512)
	evidence.RequestID = prechargeEvidenceText(evidence.RequestID, 255)
	evidence.Model = prechargeEvidenceText(evidence.Model, 255)
	if evidence.AccountID < 0 {
		evidence.AccountID = 0
	}
	p.failureEvidence = evidence
}

func prechargeEvidenceText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func FinishBalancePrecharge(ctx context.Context) {
	p := requestPrecharge(ctx)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finished = true
	p.finishLocked()
}

func (p *requestBalancePrecharge) finishLocked() {
	if !p.finished || p.pending > 0 || !p.reserved || p.settled {
		return
	}
	p.stopLeaseLocked()
	if p.billingFailed {
		p.markForReviewLocked("billing_failed")
		return
	}
	// The request owner and all retained usage workers have ended. Normal
	// billing has either captured the hold or produced no charge, so release
	// any remainder. A timeout, missing usage, or unmatched upstream observation
	// is not a bill: per-token, per-request, and media prices stay in RecordUsage.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.repo.ReleaseBalancePrecharge(ctx, p.id, p.userID); err != nil {
		slog.Error("balance precharge release failed", "precharge_id", p.id, "user_id", p.userID, "error", err)
		p.markForReviewLocked("release_failed")
		return
	}
	p.settled = true
	p.invalidateBalance()
	p.waiter.notify(p.userID)
}

func (p *requestBalancePrecharge) markForReviewLocked(reason string) {
	if !p.reviewLogged {
		slog.Error("balance precharge requires reconciliation", "precharge_id", p.id, "user_id", p.userID, "reason", reason)
		p.reviewLogged = true
	}
	if p.reviewPersisted || p.reviewRetryStarted {
		return
	}
	repo, ok := p.repo.(BalancePrechargeReconciliationRepository)
	if !ok {
		return
	}
	evidence := p.failureEvidence
	if evidence.Reason != "" && reason != "billing_failed" && reason != "release_failed" {
		reason = evidence.Reason
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	review := BalancePrechargeReviewEvidence{
		PrechargeID: p.id, UserID: p.userID, Reason: reason,
		RequestID: evidence.RequestID, AccountID: evidence.AccountID, Model: evidence.Model,
	}
	if err := repo.MarkBalancePrechargeForReview(ctx, &review); err != nil {
		slog.Error("balance precharge review persistence failed", "precharge_id", p.id, "user_id", p.userID, "error", err)
		p.retryReviewPersistenceLocked(repo, review, err)
		return
	}
	p.reviewPersisted = true
}

// Review retries are deliberately separate from the usage pool: sleeping
// through a database outage must not occupy workers needed to settle usage.
// Slots bound all retry goroutines, including their backoff, and are acquired
// before spawning. Exhaustion retains the hold and emits its accounting ID.
type balancePrechargeReviewRetryer struct {
	slots          chan struct{}
	delays         []time.Duration
	timeout        time.Duration
	attemptTimeout time.Duration
}

var defaultBalancePrechargeReviewRetryer = &balancePrechargeReviewRetryer{
	slots: make(chan struct{}, 32), delays: []time.Duration{250 * time.Millisecond, time.Second, 3 * time.Second},
	timeout: 30 * time.Second, attemptTimeout: 5 * time.Second,
}

func (p *requestBalancePrecharge) retryReviewPersistenceLocked(repo BalancePrechargeReconciliationRepository, evidence BalancePrechargeReviewEvidence, initialErr error) {
	// Only a completed request whose final usage worker ended can reach this
	// point. Freeze the existing evidence; never infer an outcome from hold age.
	if p.reviewRetryStarted || !p.finished || p.pending > 0 || p.settled {
		return
	}
	p.reviewRetryStarted = true
	retryer := p.reviewRetryer
	if retryer == nil {
		retryer = defaultBalancePrechargeReviewRetryer
	}
	select {
	case retryer.slots <- struct{}{}:
	default:
		slog.Error("balance precharge review retry capacity exhausted; hold retained", "precharge_id", evidence.PrechargeID, "user_id", evidence.UserID, "error", initialErr)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), retryer.timeout)
	p.reviewRetryCancel = cancel
	go func() {
		defer cancel()
		defer func() { <-retryer.slots }()
		lastErr := initialErr
		attempts := 0
		for _, delay := range retryer.delays {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				lastErr = ctx.Err()
			case <-timer.C:
			}
			p.mu.Lock()
			if p.settled || p.reviewPersisted {
				p.mu.Unlock()
				return
			}
			if ctx.Err() != nil {
				p.mu.Unlock()
				break
			}
			attemptCtx, attemptCancel := context.WithTimeout(ctx, retryer.attemptTimeout)
			attempts++
			lastErr = repo.MarkBalancePrechargeForReview(attemptCtx, &evidence)
			attemptCancel()
			if lastErr == nil {
				p.reviewPersisted = true
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()
		}
		slog.Error("balance precharge review retries exhausted; hold retained", "precharge_id", evidence.PrechargeID, "user_id", evidence.UserID, "attempts", attempts, "error", lastErr)
	}()
}

func (p *requestBalancePrecharge) invalidateBalance() {
	if p.invalidate == nil || p.userID <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.invalidate(ctx, p.userID); err != nil {
		slog.Warn("balance precharge cache invalidation failed", "user_id", p.userID, "error", err)
	}
}
