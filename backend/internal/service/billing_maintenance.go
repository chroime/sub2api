package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type balancePrechargeArchiver interface {
	ArchiveBalancePrecharges(context.Context, time.Time, int) (int, error)
}

type BillingMaintenanceService struct {
	recovery    BalancePrechargeRecoveryRepository
	archive     balancePrechargeArchiver
	interval    time.Duration
	batch, days int
	mu          sync.Mutex
	runMu       sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	stopped     bool
}

func NewBillingMaintenanceService(repo UsageBillingRepository, cfg *config.Config) *BillingMaintenanceService {
	s := &BillingMaintenanceService{interval: 30 * time.Second, batch: 500, days: 90}
	s.recovery, _ = repo.(BalancePrechargeRecoveryRepository)
	s.archive, _ = repo.(balancePrechargeArchiver)
	if cfg != nil {
		c := cfg.BillingMaintenance
		if c.IntervalSeconds >= 5 && c.IntervalSeconds <= 3600 {
			s.interval = time.Duration(c.IntervalSeconds) * time.Second
		}
		if c.BatchSize >= 1 && c.BatchSize <= 1000 {
			s.batch = c.BatchSize
		}
		if c.HotRetentionDays >= 7 && c.HotRetentionDays <= 3650 {
			s.days = c.HotRetentionDays
		}
	}
	return s
}

func (s *BillingMaintenanceService) Start() {
	if s == nil || s.recovery == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || s.stopped {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			run, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := s.RunOnce(run)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Warn("billing maintenance deferred", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *BillingMaintenanceService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *BillingMaintenanceService) RunOnce(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.recovery == nil {
		return nil
	}
	recovered, err := s.recovery.RecoverBalancePrecharges(ctx, s.batch)
	if err != nil {
		return fmt.Errorf("recover unresolved precharges: %w", err)
	}
	if recovered > 0 {
		slog.Warn("interrupted precharges added to reconciliation", "count", recovered)
	}
	if s.archive == nil {
		return nil
	}
	archived, err := s.archive.ArchiveBalancePrecharges(ctx, time.Now().AddDate(0, 0, -s.days), s.batch)
	if err != nil {
		return fmt.Errorf("archive terminal precharges: %w", err)
	}
	if archived > 0 {
		slog.Info("terminal precharges archived", "count", archived)
	}
	return nil
}
