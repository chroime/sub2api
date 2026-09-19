package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type balancePrechargeTurn struct {
	ctx       context.Context
	pending   bool
	after     bool
	completed bool
}

// Unfinished owners are independent of attempt-local turn numbers. Detached
// usage workers own successful turns after their callback returns.
type balancePrechargeTurns struct {
	mu          sync.Mutex
	base        context.Context
	newContext  func(context.Context) context.Context
	turns       map[int]*balancePrechargeTurn
	owned       map[*balancePrechargeTurn]struct{}
	last        *balancePrechargeTurn
	maxTurn     int
	maxAfter    int
	attempt     uint64
	attemptOpen bool
	finished    bool
}

func newBalancePrechargeTurns(base context.Context, factory func(context.Context) context.Context) *balancePrechargeTurns {
	return &balancePrechargeTurns{
		base: base, newContext: factory,
		turns: make(map[int]*balancePrechargeTurn), owned: make(map[*balancePrechargeTurn]struct{}),
	}
}

// A retired turn cannot be recreated by a delayed or duplicate callback.
func (p *balancePrechargeTurns) context(turn int) context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.turns[turn]; entry != nil {
		return entry.ctx
	}
	if p.finished || turn <= p.maxTurn {
		return nil
	}
	entry := &balancePrechargeTurn{ctx: p.newContext(p.base)}
	for previous, owner := range p.turns {
		if previous < turn && owner.completed {
			delete(p.turns, previous)
		}
	}
	p.turns[turn] = entry
	p.owned[entry] = struct{}{}
	p.last, p.maxTurn = entry, turn
	return entry.ctx
}

// Forwarders restart numbering when replaying the active payload. Completed
// requests must never be replayed as a new first turn.
func (p *balancePrechargeTurns) retryAttempt() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished || p.last == nil || p.last.completed || p.last.pending {
		return false
	}
	p.last.after = false
	p.turns = map[int]*balancePrechargeTurn{1: p.last}
	p.maxTurn = 1
	p.maxAfter = 0
	return true
}

// Some passthrough terminal events have no matching response.create admission
// (for example an upstream replying to an auxiliary frame). Preserve their
// existing usage billing without inventing a reservation or changing last.
func (p *balancePrechargeTurns) billingContext(turn int) context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.turns[turn]; entry != nil {
		return entry.ctx
	}
	return p.base
}

// Passthrough skips BeforeTurn for its first payload, so start that attempt here;
// native/bridge BeforeTurn(1) is idempotent, and an explicit local rejection
// clears its pending start. Old connections cannot bill a replacement attempt.
func (p *balancePrechargeTurns) trackAttempt(hooks *service.OpenAIWSIngressHooks) *service.OpenAIWSIngressHooks {
	p.mu.Lock()
	p.attempt++
	attempt := p.attempt
	p.attemptOpen = true
	p.mu.Unlock()
	p.start(1)
	tracked := *hooks
	active := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !p.finished && p.attemptOpen && p.attempt == attempt
	}
	tracked.BeforeRequest = func(turn int, payload []byte, model string) error {
		if !active() || p.context(turn) == nil {
			return errors.New("websocket precharge request belongs to an inactive turn")
		}
		if hooks.BeforeRequest != nil {
			if err := hooks.BeforeRequest(turn, payload, model); err != nil {
				p.reject(turn)
				return err
			}
		}
		return nil
	}
	tracked.BeforeTurn = func(turn int) error {
		if !active() || p.context(turn) == nil {
			return errors.New("websocket precharge request belongs to an inactive turn")
		}
		if hooks.BeforeTurn != nil {
			if err := hooks.BeforeTurn(turn); err != nil {
				p.reject(turn)
				return err
			}
		}
		p.start(turn)
		return nil
	}
	tracked.MapRequestModel = func(turn int, model string) (string, error) {
		if !active() {
			return "", errors.New("websocket precharge request belongs to an inactive attempt")
		}
		if hooks.MapRequestModel == nil {
			return model, nil
		}
		mapped, err := hooks.MapRequestModel(turn, model)
		if err != nil {
			p.reject(turn)
		}
		return mapped, err
	}
	tracked.AfterTurn = func(turn int, result *service.OpenAIForwardResult, err error) {
		if !active() {
			return
		}
		finish, ok := p.afterTurn(turn, result, err)
		if !ok {
			return
		}
		defer finish()
		if hooks.AfterTurn != nil {
			hooks.AfterTurn(turn, result, err)
		}
	}
	return &tracked
}

func (p *balancePrechargeTurns) start(turn int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.turns[turn]; entry != nil && !entry.pending && !entry.after && !entry.completed {
		entry.pending = true
		service.StartBalancePrechargeUpstream(entry.ctx)
	}
}

func (p *balancePrechargeTurns) reject(turn int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.turns[turn]; entry != nil && entry.pending {
		entry.pending = false
		// Admission/mapping rejected before the payload was sent upstream.
		service.ObserveBalancePrechargeUpstream(entry.ctx, 0, nil)
	}
}

func (p *balancePrechargeTurns) afterTurn(turn int, result *service.OpenAIForwardResult, err error) (func(), bool) {
	p.mu.Lock()
	entry := p.turns[turn]
	if entry == nil {
		if result != nil && turn > p.maxAfter {
			p.maxAfter = turn
			p.mu.Unlock()
			return func() {}, true
		}
		p.mu.Unlock()
		return nil, false
	}
	if entry.after || entry.completed {
		p.mu.Unlock()
		return nil, false
	}
	if turn > p.maxAfter {
		p.maxAfter = turn
	}
	entry.after = true
	if entry.pending {
		entry.pending = false
		observeBalancePrechargeWSTurn(entry.ctx, result, err)
	}
	p.mu.Unlock()
	return func() {
		// A failed attempt may still retry this hold. Finishing a known 429 here
		// could refund it before the replacement upstream receives the request.
		if err != nil {
			return
		}
		service.FinishBalancePrecharge(entry.ctx)
		p.mu.Lock()
		entry.completed = true
		delete(p.owned, entry)
		p.mu.Unlock()
	}, true
}

// Resolve only unmatched starts. Idle disconnects and duplicate terminal events
// must not contaminate another turn's financial outcome.
func (p *balancePrechargeTurns) endAttempt(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attemptOpen = false
	for _, entry := range p.turns {
		if entry.pending {
			entry.pending = false
			observeBalancePrechargeWSTurn(entry.ctx, nil, err)
		}
	}
}

func observeBalancePrechargeWSTurn(ctx context.Context, result *service.OpenAIForwardResult, err error) {
	if result != nil && err == nil {
		service.ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
		return
	}
	var fastPolicyDenied *service.OpenAIFastBlockedError
	if result == nil && (errors.As(err, &fastPolicyDenied) || service.IsReasoningEffortPolicyDenied(err)) {
		// These typed policy errors are produced while preparing the client
		// payload, before the forwarder writes it to an upstream connection.
		service.ObserveBalancePrechargeUpstream(ctx, 0, nil)
		return
	}
	var failover *service.UpstreamFailoverError
	if result == nil && errors.As(err, &failover) && failover != nil {
		switch failover.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge,
			http.StatusUnprocessableEntity, http.StatusTooManyRequests:
			service.ObserveBalancePrechargeUpstream(ctx, failover.StatusCode, nil)
			return
		}
	}
	if err == nil {
		err = errors.New("websocket attempt ended without a terminal usage result")
	}
	service.ObserveBalancePrechargeUpstream(ctx, 0, err)
}

func (p *balancePrechargeTurns) finish() {
	p.mu.Lock()
	if p.finished {
		p.mu.Unlock()
		return
	}
	p.finished, p.attemptOpen = true, false
	owners := make([]*balancePrechargeTurn, 0, len(p.owned))
	for entry := range p.owned {
		owners = append(owners, entry)
	}
	p.owned = nil
	p.mu.Unlock()
	for _, entry := range owners {
		service.FinishBalancePrecharge(entry.ctx)
	}
}
