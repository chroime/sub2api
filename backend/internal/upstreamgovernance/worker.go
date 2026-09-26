package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
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
	if interval < 15 || interval > 1440 || (enabled && !validProbeModel(model)) {
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
	if enabled {
		a, err := s.local.FindAccount(ctx, b.Marker)
		if err != nil {
			return nil, err
		}
		if a == nil || a.ID != b.AccountID {
			return nil, ErrConflict
		}
		b.ProbeModel = model
		b.ProbeIntervalMinutes = interval
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
	previous, err := s.store.LatestCheck(ctx, site.ID, b.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Reserve the next time before a potentially billable operation. A crash or
	// cancelled request must not cause another worker to immediately bill it again.
	b.NextProbeAt = s.now().Add(time.Duration(b.ProbeIntervalMinutes) * time.Minute)
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
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if err := s.runDue(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[UpstreamGovernance] scheduled operation: %s", ErrorCode(err))
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
	sites, err := s.store.DueSites(ctx, s.now(), 20)
	if err != nil {
		return err
	}
	var firstErr error
	for _, candidate := range sites {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = s.runSiteDue(ctx, candidate.ID); err != nil && !errors.Is(err, ErrBusy) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
	if site.SessionCipher != "" && !site.NextSyncAt.After(s.now()) {
		// Move this site out of the next batch before network work. In particular,
		// an interrupted collection must not stay oldest and starve other sites.
		next := s.now().Add(time.Duration(site.IntervalMinutes) * time.Minute)
		if err = s.store.ObserveSite(ctx, site.ID, site.Status, site.LastError, time.Time{}, next); err != nil {
			return err
		}
		discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, syncErr = s.syncLocked(discoveryCtx, *site)
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
