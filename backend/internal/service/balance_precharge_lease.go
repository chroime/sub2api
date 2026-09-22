package service

import (
	"context"
	"log/slog"
	"time"
)

func (p *requestBalancePrecharge) startLeaseLocked() {
	repo, ok := p.repo.(BalancePrechargeRecoveryRepository)
	if !ok || p.leaseCancel != nil || p.command == nil || p.command.LeaseOwner == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.leaseCancel = cancel
	interval := p.leaseInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	id, owner := p.id, p.command.LeaseOwner
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		warned := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			attempt, stop := context.WithTimeout(ctx, 5*time.Second)
			alive, err := repo.RenewBalancePrechargeLease(attempt, id, owner)
			stop()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if !warned {
					slog.Warn("balance precharge ownership renewal failed", "precharge_id", id, "error", err)
					warned = true
				}
				continue
			}
			warned = false
			if !alive {
				return
			}
		}
	}()
}

func (p *requestBalancePrecharge) stopLeaseLocked() {
	if p.leaseCancel == nil {
		return
	}
	p.leaseCancel()
	p.leaseCancel = nil
	if p.settled {
		return
	}
	repo, ok := p.repo.(BalancePrechargeRecoveryRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := repo.EndBalancePrechargeLease(ctx, p.id, p.command.LeaseOwner); err != nil {
		// If the DB is unavailable, the last durable lease expires naturally.
		slog.Warn("balance precharge ownership completion failed", "precharge_id", p.id, "error", err)
	}
}
