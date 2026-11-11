package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const expiryCheckTimeout = 30 * time.Second

const (
	// paymentOrderExpiryLeaderLockKey gates the periodic reconcile + expiry sweep so
	// that only one instance issues the upstream payment-provider calls per cycle.
	paymentOrderExpiryLeaderLockKey = "payment:order:expiry:leader"
	// paymentOrderExpiryLeaderLockTTL must exceed the combined reconcile + expiry
	// timeouts (3 * expiryCheckTimeout) so the lock never expires mid-run.
	paymentOrderExpiryLeaderLockTTL   = 3 * time.Minute
	paymentBillCollectorLeaderLockKey = "payment:bill:collector:leader"
	paymentBillActivePollInterval     = 5 * time.Second
)

// PaymentOrderExpiryService periodically expires timed-out payment orders.
type PaymentOrderExpiryService struct {
	paymentSvc *PaymentService
	interval   time.Duration
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

func NewPaymentOrderExpiryService(paymentSvc *PaymentService, interval time.Duration) *PaymentOrderExpiryService {
	return &PaymentOrderExpiryService{
		paymentSvc: paymentSvc,
		interval:   interval,
		stopCh:     make(chan struct{}),
		instanceID: uuid.NewString(),
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the periodic reconcile/expiry sweep. When both are nil the job
// runs ungated (single-instance / test behavior).
func (s *PaymentOrderExpiryService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *PaymentOrderExpiryService) Start() {
	if s == nil || s.paymentSvc == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
	// Keep bill confirmation independent of slow queries to unrelated payment
	// providers in the expiry sweep. One worker owns both fast and regular bill
	// collection, so its own scans cannot overlap.
	go func() {
		defer s.wg.Done()
		pollInterval := paymentBillActivePollInterval
		if s.interval < pollInterval {
			pollInterval = s.interval
		}
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		var nextFull time.Time
		for {
			now := time.Now()
			full := !now.Before(nextFull)
			if s.runBillCollection(full) && full {
				nextFull = now.Add(s.interval)
			}
			select {
			case <-ticker.C:
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *PaymentOrderExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *PaymentOrderExpiryService) runOnce() {
	// Multi-instance guard: only the leader reconciles/expires orders per cycle,
	// avoiding N× upstream payment-provider API calls and update races.
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, paymentOrderExpiryLeaderLockKey, s.instanceID, paymentOrderExpiryLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	// Notification recovery uses persisted external orders and continues when
	// the payment system or provider has been disabled for new orders.
	notifyCtx, notifyCancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	if err := s.paymentSvc.RetryBuiltinEasyPayNotifications(notifyCtx); err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to retry built-in EasyPay notifications")
	}
	notifyCancel()

	reconcileCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	recovered, err := s.paymentSvc.ReconcilePendingPaymentOrders(reconcileCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending payment orders", "error", err)
	} else if recovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid orders", "count", recovered)
	}

	expireCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	defer cancel()
	expired, err := s.paymentSvc.ExpireTimedOutOrders(expireCtx)
	if err != nil {
		slog.Error("[PaymentOrderExpiry] failed to expire orders", "error", err)
		return
	}
	if expired > 0 {
		slog.Info("[PaymentOrderExpiry] expired timed-out orders", "count", expired)
	}
}

func (s *PaymentOrderExpiryService) runBillCollection(full bool) bool {
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, paymentBillCollectorLeaderLockKey, s.instanceID, paymentOrderExpiryLeaderLockTTL)
	lockCancel()
	if !ok {
		return false
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	defer cancel()
	var err error
	if full {
		err = s.paymentSvc.CollectAlipayBillReceipts(ctx)
	} else {
		err = s.paymentSvc.CollectActiveAlipayBillReceipts(ctx)
	}
	if err != nil {
		slog.Warn("[PaymentBillCollector] failed to collect Alipay bill receipts")
	}
	return true
}
