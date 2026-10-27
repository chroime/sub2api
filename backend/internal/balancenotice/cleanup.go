package balancenotice

import (
	"context"
	"log/slog"
	"time"
)

// Cleanup never touches legacy delivery markers or business tables.
func (s *Service) Cleanup(ctx context.Context) error {
	for i := 0; i < 20; i++ {
		count, err := s.store.Prune(ctx, s.now(), 500)
		if err != nil {
			return err
		}
		if count > 0 {
			slog.Info("expired balance notification records removed", "count", count)
		}
		if count < 500 {
			return nil
		}
	}
	return nil
}

func (s *Service) Start() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			cleanupCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			err := s.Cleanup(cleanupCtx)
			stop()
			if err != nil && ctx.Err() == nil {
				slog.Warn("balance notification record cleanup failed", "error", err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.cancel = nil
}
