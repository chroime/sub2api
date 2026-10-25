package accountquality

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Service struct {
	Store        *Store
	Prober       Prober
	Capabilities []Capability
	Notifier     Notifier
	Estimate     func(Policy, string) *float64
	Price        func(string, int64, int64) *float64
	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	noticeDone   chan struct{}
	active       map[string]context.CancelFunc
	wg           sync.WaitGroup
}

func NewService(store *Store, prober Prober, capabilities []Capability) *Service {
	store.Capabilities = capabilities
	return &Service{Store: store, Prober: prober, Capabilities: capabilities, active: map[string]context.CancelFunc{}}
}
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	p, e := s.Store.Policies(ctx)
	if e != nil {
		return Overview{}, e
	}
	b, e := s.Store.Bindings(ctx, 0)
	if e != nil {
		return Overview{}, e
	}
	settings, e := s.Store.Settings(ctx)
	if e != nil {
		return Overview{}, e
	}
	if e = s.enrich(ctx, b, p); e != nil {
		return Overview{}, e
	}
	return Overview{p, b, Templates(), settings, s.Capabilities}, nil
}
func (s *Service) Detail(ctx context.Context, accountID int64) (AccountDetail, error) {
	b, e := s.Store.Bindings(ctx, accountID)
	if e != nil {
		return AccountDetail{}, e
	}
	policies, e := s.Store.Policies(ctx)
	if e != nil {
		return AccountDetail{}, e
	}
	if e = s.enrich(ctx, b, policies); e != nil {
		return AccountDetail{}, e
	}
	runs, e := s.Store.Runs(ctx, accountID, 0, 100)
	if e != nil {
		return AccountDetail{}, e
	}
	actions, e := s.Store.Actions(ctx, accountID)
	if e != nil {
		return AccountDetail{}, e
	}
	return AccountDetail{b, runs, actions}, nil
}
func (s *Service) enrich(ctx context.Context, bindings []Binding, policies []Policy) error {
	byID := map[int64]Policy{}
	for _, p := range policies {
		byID[p.ID] = p
	}
	ids := []int64{}
	for _, b := range bindings {
		ids = append(ids, b.ID)
	}
	recent, e := s.Store.RecentSummaries(ctx, ids)
	if e != nil {
		return e
	}
	for i := range bindings {
		b := &bindings[i]
		p := EffectivePolicy(byID[b.PolicyID], b.Overrides)
		b.PolicyName = p.Name
		b.PolicyVersion = p.Version
		b.ControlMode = p.ControlMode
		b.IntervalSeconds = p.IntervalSeconds
		b.FailureThreshold = p.FailureThreshold
		b.RecoveryThreshold = p.RecoveryThreshold
		b.IsolationScope = p.IsolationScope
		b.AutoControlEligible = s.Store.eligible(b.Platform, b.AccountType, p.APIMode)
		b.RecentRuns = recent[b.ID]
		if b.RecentRuns == nil {
			b.RecentRuns = []Run{}
		}
	}
	return nil
}
func (s *Service) Summaries(ctx context.Context, ids []int64) (map[string][]Binding, error) {
	out := map[string][]Binding{}
	for _, id := range ids {
		out[fmt.Sprint(id)] = []Binding{}
	}
	bindings, e := s.Store.BindingSummaries(ctx, ids)
	if e != nil {
		return nil, e
	}
	policies, e := s.Store.Policies(ctx)
	if e != nil {
		return nil, e
	}
	if e = s.enrich(ctx, bindings, policies); e != nil {
		return nil, e
	}
	for _, b := range bindings {
		key := fmt.Sprint(b.AccountID)
		out[key] = append(out[key], b)
	}
	return out, nil
}
func (s *Service) Queue(ctx context.Context, id, version, actor int64, request string) (Run, error) {
	return s.Store.Queue(ctx, id, version, actor, request, s.Estimate)
}
func (s *Service) Cancel(ctx context.Context, id string, actor int64) error {
	if e := s.Store.Cancel(ctx, id, actor); e != nil {
		return e
	}
	s.mu.Lock()
	cancel := s.active[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	s.noticeDone = make(chan struct{})
	go func() {
		defer close(s.noticeDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.deliverNotifications(ctx)
			}
		}
	}()
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}
func (s *Service) Stop() {
	s.mu.Lock()
	cancel, done, noticeDone := s.cancel, s.done, s.noticeDone
	if cancel != nil {
		cancel()
	}
	for _, stop := range s.active {
		stop()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	if noticeDone != nil {
		<-noticeDone
	}
	s.wg.Wait()
}
func (s *Service) tick(ctx context.Context) {
	if err := s.Store.Reap(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Warn("account quality lease cleanup failed", "error", err)
		}
		return
	}
	due, err := s.Store.Due(ctx)
	if err != nil {
		return
	}
	for _, id := range due {
		if _, err = s.Queue(ctx, id, 0, 0, ""); err != nil && !errors.Is(err, ErrBusy) && !errors.Is(err, ErrConflict) {
			slog.Warn("account quality schedule failed", "binding", id, "error", err)
		}
	}
	for i := 0; i < 20; i++ {
		r, e := s.Store.Claim(ctx)
		if errors.Is(e, ErrNotFound) || errors.Is(e, ErrBusy) {
			break
		}
		if e != nil {
			break
		}
		s.wg.Add(1)
		go func() { defer s.wg.Done(); s.execute(ctx, r) }()
	}
}
func (s *Service) deliverNotifications(ctx context.Context) {
	if s.Notifier != nil {
		for i := 0; i < 10; i++ {
			n, e := s.Store.claimNotification(ctx)
			if e != nil {
				break
			}
			settings, e := s.Store.Settings(ctx)
			allowed := false
			if e == nil {
				for _, event := range settings.Notifications.Events {
					if event == n.Event {
						for _, target := range settings.Notifications.EmailRecipients {
							if settings.Notifications.EmailEnabled && target == n.Recipient {
								allowed = true
							}
						}
						for _, target := range settings.Notifications.QQTargets {
							if settings.Notifications.QQEnabled && "qq:"+target == n.Recipient {
								allowed = true
							}
						}
					}
				}
			}
			var delivery error
			if e != nil {
				delivery = e
			}
			if allowed {
				sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				delivery = s.Notifier.SendAccountQualityNotice(sendCtx, n.Recipient, n.Body)
				cancel()
			}
			_ = s.Store.finishNotification(ctx, n, delivery)
		}
	}
}
func (s *Service) execute(parent context.Context, r Run) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(r.Config.TimeoutSeconds)*time.Second)
	s.mu.Lock()
	s.active[r.ID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.active, r.ID); s.mu.Unlock() }()
	watchDone := make(chan struct{})
	defer close(watchDone)
	leaseRun := r
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-watchDone:
				return
			case <-ticker.C:
				if !s.Store.Authorize(ctx, leaseRun) {
					cancel()
					return
				}
			}
		}
	}()
	r.ExecutionStatus = "cancelled"
	r.QualityVerdict = "not_evaluated"
	if s.Store.Authorize(ctx, r) {
		expectedMappedModel := r.MappedModel
		result, err := s.Prober.ProbeAccountText(ctx, ProbeRequest{AccountID: r.AccountID, BindingID: r.BindingID, Model: r.Model, APIMode: r.APIMode, Prompt: r.Prompt, Effort: r.Effort, MaxOutputTokens: r.MaxOutputTokens, TimeoutSeconds: r.Config.TimeoutSeconds, Manual: r.ActorID > 0})
		r.RequestedModel = result.RequestedModel
		r.ActualAccountID = result.AccountID
		r.MappedModel = result.MappedModel
		r.ReturnedModel = result.ReturnedModel
		r.OutputLimitMode = result.OutputLimitMode
		r.ResponseText = result.Text
		r.FinishReason = result.FinishReason
		r.ExecutionStatus = result.ExecutionStatus
		r.HTTPStatus = result.HTTPStatus
		r.ErrorClass = result.ErrorClass
		r.DurationMS = result.LatencyMS
		r.InputTokens = result.InputTokens
		r.OutputTokens = result.OutputTokens
		r.ReasoningTokens = result.ReasoningTokens
		r.AutoControlEligible = result.AutoControlEligible
		r.CredentialAccountID = result.CredentialAccountID
		r.UpstreamRequestID = result.UpstreamRequestID
		r.CostUSD = result.CostUSD
		r.Reason = result.ErrorMessage
		if s.Price != nil && r.CostUSD == nil && (r.InputTokens > 0 || r.OutputTokens > 0) {
			costModel := r.MappedModel
			if costModel == "" {
				costModel = r.Model
			}
			r.CostUSD = s.Price(costModel, r.InputTokens, r.OutputTokens)
		}
		if err != nil {
			r.ExecutionStatus = "error"
			r.ErrorClass = "diagnostic_error"
			r.Reason = "固定账号诊断未能完成"
		}
		if result.IdentityVersion != "" && result.IdentityVersion != r.IdentityVersion {
			r.Stale = true
		}
		if expectedMappedModel != "" && result.MappedModel != "" && result.MappedModel != expectedMappedModel {
			r.Stale = true
		}
		if result.AccountID != r.AccountID {
			r.Stale = true
			r.ExecutionStatus = "error"
			r.AutoControlEligible = false
			r.ErrorClass = "account_identity_mismatch"
			r.Reason = "实际执行账号与指定账号不一致，结果仅留档"
		}
		if r.ExecutionStatus == "completed" {
			r.QualityVerdict, r.Reason = Judge(r.Template, r.Expected, r.ResponseText)
		}
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finishCancel()
	if e := s.Store.Complete(finishCtx, r); e != nil {
		slog.Error("account quality evidence commit failed", "run", r.ID, "error", e)
	}
}
