package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	usageLogRecoveryInterval    = 10 * time.Second
	usageLogRecoveryBatch       = 128
	usageLogRecoveryConcurrency = 8
	usageLogRecoveryItemTimeout = 5 * time.Second
	usageLogRecoveryLease       = 5 * time.Minute
)

// UsageLogRecoveryService repairs display records after billing. It deliberately
// has no billing repository dependency and cannot alter a wallet or quota.
type UsageLogRecoveryService struct {
	repo        UsageLogOutboxRepository
	usage       UsageLogRepository
	runMu       sync.Mutex
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	stopped     bool
	interval    time.Duration
	batchSize   int
}

func NewUsageLogRecoveryService(repo UsageLogOutboxRepository, usage UsageLogRepository) *UsageLogRecoveryService {
	return &UsageLogRecoveryService{repo: repo, usage: usage, interval: usageLogRecoveryInterval, batchSize: usageLogRecoveryBatch}
}

// Configure is intended for startup wiring. Once Start has run the worker's
// bounded interval and batch are immutable.
func (s *UsageLogRecoveryService) Configure(interval time.Duration, batchSize int) {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.cancel != nil {
		return
	}
	if interval >= time.Second {
		s.interval = interval
	}
	if batchSize > 0 && batchSize <= 256 {
		s.batchSize = batchSize
	}
}

func (s *UsageLogRecoveryService) Start() {
	if s == nil || s.repo == nil || s.usage == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.cancel != nil || s.stopped {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			if _, err := s.RunOnce(ctx, s.batchSize); err != nil && ctx.Err() == nil {
				slog.Warn("usage-log recovery will retry durable pending work", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *UsageLogRecoveryService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.stopped = true
	cancel, done := s.cancel, s.done
	s.lifecycleMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// RunOnce processes a bounded batch, with bounded concurrency and per-item
// timeouts. Fenced leases allow other processes to recover items after crashes.
func (s *UsageLogRecoveryService) RunOnce(ctx context.Context, limit int) (int, error) {
	if s == nil || s.repo == nil || s.usage == nil {
		return 0, nil
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = usageLogRecoveryBatch
	}
	if limit > 256 {
		limit = 256
	}
	claimCtx, claimCancel := context.WithTimeout(ctx, usageLogRecoveryItemTimeout)
	items, err := s.repo.ClaimUsageLogOutbox(claimCtx, limit, usageLogRecoveryLease)
	claimCancel()
	if err != nil {
		return 0, fmt.Errorf("claim usage-log recovery work: %w", err)
	}
	if len(items) == 0 {
		return 0, nil
	}
	jobs := make(chan UsageLogOutboxItem, len(items))
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	var wg sync.WaitGroup
	var completed, failed atomic.Int64
	for i := 0; i < min(len(items), usageLogRecoveryConcurrency); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if ctx.Err() != nil {
					return
				}
				ok, err := s.recoverItem(ctx, item)
				if err != nil {
					failed.Add(1)
				}
				if ok {
					completed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if failures := failed.Load(); failures > 0 {
		return int(completed.Load()), fmt.Errorf("%d usage-log recovery items remain pending", failures)
	}
	return int(completed.Load()), ctx.Err()
}

func (s *UsageLogRecoveryService) recoverItem(ctx context.Context, item UsageLogOutboxItem) (completed bool, err error) {
	itemCtx, cancel := context.WithTimeout(ctx, usageLogRecoveryItemTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			// Never log a panic value: a writer could include the row contents.
			_, _ = s.repo.RetryUsageLogOutbox(itemCtx, item.ID, item.ClaimToken, usageLogRecoveryBackoff(item.Attempts), "usage_write_panicked")
			completed, err = false, errors.New("usage-log writer panicked; durable retry retained")
		}
	}()
	log, decodeErr := decodeUsageLogRecoveryPayload(item)
	if decodeErr != nil {
		_, _ = s.repo.RetryUsageLogOutbox(itemCtx, item.ID, item.ClaimToken, usageLogRecoveryBackoff(item.Attempts), "invalid_payload")
		return false, decodeErr
	}
	write := s.usage.Create
	if recoveryWriter, ok := s.usage.(interface {
		RecoverUsageLog(context.Context, *UsageLog) (bool, error)
	}); ok {
		write = recoveryWriter.RecoverUsageLog
	}
	if _, writeErr := write(itemCtx, log); writeErr != nil {
		_, _ = s.repo.RetryUsageLogOutbox(itemCtx, item.ID, item.ClaimToken, usageLogRecoveryBackoff(item.Attempts), "usage_write_failed")
		return false, errors.New("usage-log insert failed; durable retry retained")
	}
	// A crash after Create but before completion leaves the item available after
	// lease expiry. The usage_logs unique request/key index makes that replay safe.
	completed, err = s.repo.CompleteUsageLogOutbox(itemCtx, item.ID, item.ClaimToken)
	if errors.Is(err, ErrUsageLogOutboxConflict) {
		_, _ = s.repo.RetryUsageLogOutbox(itemCtx, item.ID, item.ClaimToken, usageLogRecoveryBackoff(item.Attempts), "usage_conflict")
	}
	return completed, err
}

func usageLogRecoveryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return min(30*time.Second*time.Duration(1<<uint(attempt-1)), time.Hour)
}
