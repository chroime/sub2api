package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"
)

func validProbeModel(model string) bool {
	return model != "" && len(model) <= 256 && strings.TrimSpace(model) == model && strings.IndexFunc(model, unicode.IsControl) < 0
}

func (s *Service) binding(ctx context.Context, siteID, bindingID int64) (*Binding, error) {
	bindings, err := s.store.ListBindings(ctx, siteID)
	if err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if b.ID == bindingID && b.SiteID == siteID {
			if b.AccountID <= 0 || b.KeyCipher == "" {
				return nil, ErrInvalid
			}
			return &b, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) ConfigureMonitor(ctx context.Context, siteID, bindingID int64, enabled bool, model string, interval int) (*Binding, error) {
	if enabled && (!s.durableKey || s.cipher == nil) {
		return nil, ErrEncryption
	}
	if interval == 0 {
		interval = 30
	}
	if !validIntervalMinutes(interval) || (enabled && !validProbeModel(model)) {
		return nil, ErrInvalid
	}
	_, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	b, err := s.binding(ctx, siteID, bindingID)
	if err != nil {
		return nil, err
	}
	b.ProbeIntervalMinutes = interval
	if enabled {
		conflict, e := s.modelLegacyConflict(ctx, siteID, b, model)
		if e != nil {
			return nil, e
		}
		if conflict {
			return nil, ErrConflict
		}
		a, err := s.local.FindAccount(ctx, b.Marker)
		if err != nil {
			return nil, err
		}
		if a == nil || a.ID != b.AccountID {
			return nil, ErrConflict
		}
		b.ProbeModel = model
		b.NextProbeAt = s.now()
	}
	b.ProbeEnabled = enabled
	if err = s.store.SaveBinding(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) Probe(ctx context.Context, siteID, bindingID int64, model string) (*Check, error) {
	if !s.durableKey || s.cipher == nil {
		return nil, ErrEncryption
	}
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer free()
	site, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	b, err := s.binding(ctx, siteID, bindingID)
	if err != nil {
		return nil, err
	}
	if model == "" {
		model = b.ProbeModel
	}
	if !validProbeModel(model) {
		return nil, ErrInvalid
	}
	return s.probeLocked(ctx, *site, b, model)
}

// The caller holds both the remote-operation slot and the site's advisory lock.
func (s *Service) probeLocked(ctx context.Context, site Site, b *Binding, model string) (*Check, error) {
	if conflict, err := s.modelLegacyConflict(ctx, site.ID, b, model); err != nil {
		return nil, err
	} else if conflict {
		return nil, ErrBusy
	}
	previous, err := s.store.LatestCheck(ctx, site.ID, b.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Reserve the next time before a potentially billable operation. A crash or
	// cancelled request must not cause another worker to immediately bill it again.
	b.NextProbeAt = addMinutes(s.now(), int64(b.ProbeIntervalMinutes))
	if err = s.store.SaveBinding(ctx, b); err != nil {
		return nil, err
	}
	result := ProbeResult{}
	a, err := s.local.FindAccount(ctx, b.Marker)
	if err != nil {
		result.ErrorCode = ErrorCode(err)
	} else if a == nil || a.ID != b.AccountID {
		result.ErrorCode = "local_account_missing"
	} else {
		var key RemoteKey
		raw, decryptErr := s.cipher.Decrypt(b.KeyCipher)
		if decryptErr != nil || json.Unmarshal([]byte(raw), &key) != nil || key.Key == "" {
			result.ErrorCode = "reauth_required"
		} else {
			result, err = s.connector.Probe(ctx, site, key, b.Platform, model)
			if err != nil {
				result.Success = false
			}
			if result.Success {
				result.ErrorCode = ""
			} else {
				switch result.ErrorCode {
				case "invalid_text_response":
				default:
					result.ErrorCode = ErrorCode(err)
				}
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if result.LatencyMS < 0 {
		result.LatencyMS = 0
	}
	check := &Check{SiteID: site.ID, BindingID: b.ID, Model: model, ProbeResult: result, CreatedAt: s.now()}
	if err = s.store.AddCheck(ctx, check); err != nil {
		return nil, err
	}
	if !result.Success && (previous == nil || previous.Success) {
		err = s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "probe_failed", Resource: b.Marker, After: result.ErrorCode, CreatedAt: s.now()})
	} else if result.Success && previous != nil && !previous.Success {
		err = s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "probe_recovered", Resource: b.Marker, Before: previous.ErrorCode, After: "healthy", CreatedAt: s.now()})
	}
	return check, err
}

// Start is idempotent. All outbound work is skipped without durable encryption.
func (s *Service) Start() {
	s.startModelWorker()
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.workerDone != nil || !s.durableKey || s.cipher == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.workerCancel, s.workerDone = cancel, done
	go func() {
		defer close(done)
		for {
			if err := s.runDue(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[UpstreamGovernance] scheduled operation: %s", ErrorCode(err))
			}
			wait := s.workerWait(ctx)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
		}
	}()
}

// workerWait returns the next persisted fast deadline, clamped to a short
// wake-up. A legacy store without migration 261 retains its one-minute cadence.
func (s *Service) workerWait(ctx context.Context) time.Duration {
	wait := time.Minute
	if fastStore, ok := s.store.(FastObservationScheduleStore); ok {
		if next, err := fastStore.NextFastObservationAt(ctx); err == nil && next != nil {
			delta := next.Sub(s.now())
			if delta < 100*time.Millisecond {
				delta = 100 * time.Millisecond
			}
			if delta < wait {
				wait = delta
			}
		}
	}
	return wait
}

func (s *Service) Stop() {
	s.stopBrowserAuthorizations()
	s.stopModelWorker()
	s.workerMu.Lock()
	done := s.workerDone
	if done == nil {
		s.workerMu.Unlock()
		return
	}
	s.workerCancel()
	s.workerMu.Unlock()
	<-done
	s.workerMu.Lock()
	if s.workerDone == done {
		s.workerDone, s.workerCancel = nil, nil
	}
	s.workerMu.Unlock()
}

func (s *Service) runDue(ctx context.Context) error {
	if !s.durableKey || s.cipher == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	// Fast observation is deliberately first: a slow/expired session renewal
	// must not make an independent second-based group deadline wait a minute.
	firstErr := s.runFastDue(ctx)
	// Renewal failures have their own budget; collection retains the parent
	// context even when a session's identity endpoint consumes this phase.
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, sessionRefreshBatchTimeout)
	refreshErr := s.refreshDueSessions(refreshCtx)
	cancelRefresh()
	if firstErr == nil {
		firstErr = refreshErr
	}
	sites, err := s.store.DueSites(ctx, s.now(), 20)
	if err != nil {
		return err
	}
	for _, candidate := range sites {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = s.runSiteDue(ctx, candidate.ID); err != nil && !errors.Is(err, ErrBusy) && firstErr == nil {
			firstErr = err
		}
	}
	// Deliver only after all site work has returned. The dispatcher performs
	// SMTP I/O but never holds a site advisory lock or remote-operation slot.
	if notifyErr := s.DispatchChangeNotifications(ctx, 20); notifyErr != nil && firstErr == nil {
		firstErr = notifyErr
	}
	return firstErr
}

// runFastDue services the optional second-based group observer. Each candidate
// is reserved before decrypting credentials or making a remote request, so a
// cancelled worker cannot issue the same observation again immediately.
func (s *Service) runFastDue(ctx context.Context) error {
	fastStore, ok := s.store.(FastObservationStore)
	if !ok {
		return nil
	}
	now := s.now()
	candidates, err := fastStore.DueFastObservations(ctx, now, 20)
	if err != nil {
		return err
	}
	var firstErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		dueAt := candidate.NextFastObserveAt
		if dueAt.IsZero() {
			dueAt = now
		}
		reserved, reserveErr := fastStore.ReserveFastObservation(ctx, candidate.ID, dueAt, now)
		if reserveErr != nil {
			if firstErr == nil {
				firstErr = reserveErr
			}
			continue
		}
		if !reserved {
			continue
		}
		candidateID := candidate.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			if runErr := s.runFastSite(ctx, candidateID); runErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = runErr
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func (s *Service) runFastSite(ctx context.Context, siteID int64) error {
	fastStore := s.store.(FastObservationStore)
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return err
	}
	remoteReleased := false
	defer func() {
		if !remoteReleased {
			free()
		}
	}()
	site, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return err
	}
	siteReleased := false
	defer func() {
		if !siteReleased {
			release()
		}
	}()
	now := s.now()
	interval := site.FastIntervalSeconds
	if !validIntervalSeconds(interval) {
		interval = int64(site.IntervalMinutes) * 60
	}
	if !validIntervalSeconds(interval) {
		interval = 900
	}
	next := addSeconds(now, interval)
	observe := GroupObservation{ObservedAt: now, GroupsComplete: false}
	status, message := "error", ""
	session, sessionErr := s.session(*site)
	if sessionErr == nil {
		connector, supported := s.connector.(FastObservationConnector)
		if !supported {
			message = ErrorCode(ErrUnsupported)
		} else {
			observe, sessionErr = connector.ObserveGroups(ctx, *site, session)
			if sessionErr == nil {
				status, message = "healthy", ""
			} else if limited, ok := sessionErr.(*RateLimitError); ok {
				status, message = "rate_limited", "rate_limited"
				next = addSeconds(now, int64(limited.RetryAfter/time.Second))
				if !next.After(now) {
					next = now.Add(time.Second)
				}
			} else {
				message = ErrorCode(sessionErr)
			}
		}
	} else {
		message = ErrorCode(sessionErr)
	}
	result, saveErr := fastStore.ObserveFastResult(ctx, siteID, now, next, status, message, observe)
	if saveErr != nil {
		return saveErr
	}
	if sessionErr != nil {
		// Some older upstream deployments support the complete catalog contract
		// but do not expose the cheap group/rate endpoints. Keep the observation
		// status visible for the administrator without turning every scheduled
		// capability mismatch into a failing worker cycle.
		if errors.Is(sessionErr, ErrUnsupported) {
			return nil
		}
		return sessionErr
	}
	var pricingOps []PricingOperation
	var pricingErr error
	if observe.GroupsComplete {
		pricingOps, pricingErr = s.recalculatePricingForObservation(ctx, *site, observe.Groups)
	}
	if len(pricingOps) > 0 || result.Changed {
		release()
		siteReleased = true
		free()
		remoteReleased = true
		if result.Changed && result.Revision > 1 {
			notice := renderFastObservationChangeNotice(*site, result.Revision, result.ObservedAt)
			_ = s.EnqueueChangeNotice(ctx, notice)
		}
		for _, operation := range pricingOps {
			_ = s.EnqueuePricingOperationNotice(ctx, site.ID, operation)
		}
		return pricingErr
	}
	return nil
}

func renderFastObservationChangeNotice(site Site, revision int64, observedAt time.Time) ChangeNotice {
	return ChangeNotice{
		SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL,
		Kind: "rate_change", Severity: "warning",
		DedupKey:   fmt.Sprintf("site:%d:catalog:%d", site.ID, revision),
		Subject:    "上游可见分组或倍率发生变化",
		Body:       fmt.Sprintf("上游名称：%s\n站点URL：%s\n可见分组或倍率目录已更新到第 %d 个版本，请检查受影响的本地分组、成本事实和自动定价结果。", site.Name, site.BaseURL, revision),
		ObservedAt: observedAt,
	}
}

func (s *Service) runSiteDue(ctx context.Context, siteID int64) error {
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return err
	}
	defer free()
	site, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return err
	}
	defer release()
	if !site.Enabled {
		return nil
	}
	var syncErr error
	didSync := false
	if site.SessionCipher != "" && !site.NextSyncAt.After(s.now()) {
		didSync = true
		// Move this site out of the next batch before network work. In particular,
		// an interrupted collection must not stay oldest and starve other sites.
		next := addSeconds(s.now(), collectionIntervalSeconds(*site))
		if err = s.store.ObserveSite(ctx, site.ID, site.Status, site.LastError, time.Time{}, next); err != nil {
			return err
		}
		discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, syncErr = s.syncLockedWithAutoReauthorization(discoveryCtx, *site, true)
		cancel()
		// An individual discovery timeout still leaves the batch context available
		// for saving a failure and for checks using the existing inference key.
		if errors.Is(syncErr, context.DeadlineExceeded) && ctx.Err() == nil {
			if err = s.store.ObserveSite(ctx, site.ID, "error", "timeout", s.now(), next); err != nil {
				return err
			}
			if site.Status != "error" || site.LastError != "timeout" {
				if err = s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "sync_failed", After: "timeout", CreatedAt: s.now()}); err != nil {
					return err
				}
			}
		}
	}
	if site.SessionCipher != "" && (!didSync || syncErr == nil) {
		// Collection may have rotated the management session. The key inventory
		// must use that persisted session, not the pre-collection site snapshot.
		if didSync {
			fresh, readErr := s.store.GetSite(ctx, site.ID)
			if readErr != nil {
				return readErr
			}
			site = fresh
		}
		due, dueErr := s.keyAuditDue(ctx, site.ID, s.now())
		if dueErr != nil {
			return dueErr
		}
		if didSync || due {
			auditCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			auditErr := s.auditManagedKeysLocked(auditCtx, *site)
			cancel()
			if auditErr != nil && !errors.Is(auditErr, ErrUnsupported) {
				log.Printf("[UpstreamGovernance] scheduled key audit: %s", ErrorCode(auditErr))
			}
		}
	}
	bindings, err := s.store.ListBindings(ctx, siteID)
	if err != nil {
		return err
	}
	count := 0
	for i := range bindings {
		b := &bindings[i]
		if !b.ProbeEnabled || b.NextProbeAt.After(s.now()) {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !validProbeModel(b.ProbeModel) || b.AccountID <= 0 || b.KeyCipher == "" {
			continue
		}
		if _, err = s.probeLocked(ctx, *site, b, b.ProbeModel); err != nil {
			return err
		}
		count++
		if count == 5 {
			break
		}
	}
	return syncErr
}
