package service

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

type balancePrechargeHeartbeatKey struct{}

// WithBalancePrechargeWaitHeartbeat keeps streaming transports alive during
// admission. The callback runs only on the requesting goroutine, never on a
// billing worker, and is not an upstream or synthetic first-token event.
func WithBalancePrechargeWaitHeartbeat(ctx context.Context, heartbeat func() error) context.Context {
	return context.WithValue(ctx, balancePrechargeHeartbeatKey{}, heartbeat)
}

// A process-local FIFO limits one user's waiting admission probes to one at a
// time. Money remains exclusively owned by the database transaction; polling
// also observes settlement/top-ups from other processes. No SQL connection is
// held while sleeping. Empty queues are removed rather than retained per user.
type balancePrechargeWaiter struct {
	mu                sync.Mutex
	users             map[int64]*balancePrechargeWaitQueue
	timeout           time.Duration
	pollInterval      time.Duration
	heartbeatInterval time.Duration
}

type balancePrechargeWaitQueue struct {
	requests list.List
	changed  chan struct{}
}

func newBalancePrechargeWaiter() *balancePrechargeWaiter {
	return &balancePrechargeWaiter{
		users: make(map[int64]*balancePrechargeWaitQueue), timeout: 5 * time.Minute,
		pollInterval: time.Second, heartbeatInterval: 10 * time.Second,
	}
}

var defaultBalancePrechargeWaiter = newBalancePrechargeWaiter()

func (w *balancePrechargeWaiter) enqueue(userID int64) (<-chan struct{}, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	queue := w.users[userID]
	if queue == nil {
		queue = &balancePrechargeWaitQueue{changed: make(chan struct{})}
		w.users[userID] = queue
	}
	ready := make(chan struct{})
	entry := queue.requests.PushBack(ready)
	if queue.requests.Len() == 1 {
		close(ready)
	}
	return ready, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		first := queue.requests.Front() == entry
		queue.requests.Remove(entry)
		if queue.requests.Len() == 0 {
			delete(w.users, userID)
		} else if first {
			close(queue.requests.Front().Value.(chan struct{}))
		}
	}
}

func (w *balancePrechargeWaiter) changed(userID int64) <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.users[userID].changed
}

func (w *balancePrechargeWaiter) notify(userID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if queue := w.users[userID]; queue != nil {
		close(queue.changed)
		queue.changed = make(chan struct{})
	}
}

func (w *balancePrechargeWaiter) reserve(parent context.Context, repo BalancePrechargeRepository, cmd *BalancePrechargeCommand) (*BalancePrechargeResult, error) {
	ctx, cancel := context.WithTimeout(parent, w.timeout)
	defer cancel()
	contextError := func() error {
		if err := parent.Err(); err != nil {
			return err
		}
		return ErrBalancePrechargeWaitTimeout
	}
	if ctx.Err() != nil {
		return nil, contextError()
	}
	ready, leave := w.enqueue(cmd.UserID)
	defer leave()
	heartbeat, _ := parent.Value(balancePrechargeHeartbeatKey{}).(func() error)
	var beats <-chan time.Time
	if heartbeat != nil {
		ticker := time.NewTicker(w.heartbeatInterval)
		defer ticker.Stop()
		beats = ticker.C
	}
	wait := func(signal <-chan struct{}) error {
		for {
			select {
			case <-ctx.Done():
				return contextError()
			case <-signal:
				return nil
			case <-beats:
				if err := heartbeat(); err != nil {
					return err
				}
			}
		}
	}
	if err := wait(ready); err != nil {
		return nil, err
	}
	for {
		if ctx.Err() != nil {
			return nil, contextError()
		}
		// Subscribe before probing so a settlement racing the transaction
		// cannot be missed until the next polling interval.
		changed := w.changed(cmd.UserID)
		result, err := repo.ReserveBalancePrecharge(ctx, cmd)
		if !errors.Is(err, ErrBalancePrechargeWaiting) {
			if err != nil && ctx.Err() != nil {
				return nil, contextError()
			}
			return result, err
		}
		timer := time.NewTimer(w.pollInterval)
		waiting := true
		for waiting {
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, contextError()
			case <-changed:
				waiting = false
			case <-timer.C:
				waiting = false
			case <-beats:
				if err := heartbeat(); err != nil {
					timer.Stop()
					return nil, err
				}
			}
		}
		timer.Stop()
	}
}
