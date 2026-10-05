package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"
)

type BalanceMonitorConfig struct {
	Enabled         bool     `json:"enabled"`
	Threshold       float64  `json:"threshold"`
	Unit            string   `json:"unit"`
	Recipients      []string `json:"recipients"`
	CooldownMinutes int      `json:"cooldown_minutes"`
}

type BalanceMonitorStatus struct {
	State          string     `json:"state"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
	LastNotifiedAt *time.Time `json:"last_notified_at"`
	LastError      string     `json:"last_error"`
}

// BalanceMonitorState is persisted separately from public policy. Recipient
// hashes and reservations are never included in the administrator API response.
type BalanceMonitorState struct {
	Status          BalanceMonitorStatus             `json:"status"`
	Low             bool                             `json:"low"`
	RecoveryPending bool                             `json:"recovery_pending,omitempty"`
	Recipients      map[string]BalanceRecipientState `json:"recipients,omitempty"`
}

type BalanceRecipientState struct {
	NextAttemptAt   time.Time  `json:"next_attempt_at"`
	LastSentAt      *time.Time `json:"last_sent_at,omitempty"`
	Failed          bool       `json:"failed,omitempty"`
	RecoveryPending bool       `json:"recovery_pending,omitempty"`
}

type BalanceNotice struct {
	SiteID     int64
	SiteName   string
	BaseURL    string
	Platform   string
	Balance    float64
	Threshold  float64
	Unit       string
	Recovered  bool
	ObservedAt time.Time
}

type BalanceNotifier interface {
	Recipients(context.Context, []string) ([]string, error)
	Send(context.Context, string, BalanceNotice) error
}

// SetBalanceNotifier is wired before Start so the worker never observes a
// partially initialized mail adapter.
func (s *Service) SetBalanceNotifier(notifier BalanceNotifier) { s.balanceNotifier = notifier }

func balanceUnit(platform string) string {
	if platform == "newapi" {
		return "quota"
	}
	return "usd"
}

func defaultBalanceMonitor(platform string) BalanceMonitorConfig {
	return BalanceMonitorConfig{Threshold: 10, Unit: balanceUnit(platform), Recipients: []string{}, CooldownMinutes: 1440}
}

func normalizeBalanceRecipients(recipients []string) ([]string, error) {
	if len(recipients) > 10 {
		return nil, ErrInvalid
	}
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range recipients {
		address := strings.TrimSpace(raw)
		parsed, err := mail.ParseAddress(address)
		if err != nil || len(address) > 320 || parsed.Address != address || strings.ContainsAny(address, "\r\n") {
			return nil, ErrInvalid
		}
		key := strings.ToLower(address)
		if !seen[key] {
			out = append(out, address)
			seen[key] = true
		}
	}
	return out, nil
}

func (s *Service) ConfigureBalanceMonitor(ctx context.Context, id, version int64, config BalanceMonitorConfig) (*Site, error) {
	if math.IsNaN(config.Threshold) || math.IsInf(config.Threshold, 0) || config.Threshold < 0 || !validIntervalMinutes(config.CooldownMinutes) {
		return nil, ErrInvalid
	}
	recipients, err := normalizeBalanceRecipients(config.Recipients)
	if err != nil {
		return nil, err
	}
	config.Recipients = recipients
	site, release, err := s.siteLock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	if version != site.Version {
		return nil, ErrConflict
	}
	if config.Unit != balanceUnit(site.Platform) {
		return nil, ErrInvalid
	}
	site.BalanceMonitor = config
	site.balanceState.Status.State = "disabled"
	if config.Enabled {
		site.balanceState.Status.State = "unknown"
	}
	site.balanceState.Status.LastError = ""
	site.BalanceMonitorStatus = site.balanceState.Status
	// Preserve all per-recipient cooldowns even when policy is disabled or edited.
	if err = s.store.UpdateSite(ctx, site, version); err != nil {
		return nil, err
	}
	return site, nil
}

func balanceRecipientHash(recipient string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(recipient))))
	return hex.EncodeToString(digest[:])
}

func (s *Service) saveBalanceState(ctx context.Context, siteID int64, state BalanceMonitorState, events ...Event) bool {
	return s.store.SaveBalanceMonitorState(ctx, siteID, state, events) == nil
}

// checkBalanceMonitor is called only with the site's advisory lock held and a
// freshly persisted successful snapshot. Unknown readings do not end incidents.
func (s *Service) checkBalanceMonitor(ctx context.Context, site Site, snapshot *Snapshot) {
	config := site.BalanceMonitor
	if !site.Enabled || !config.Enabled || snapshot == nil {
		return
	}
	state := site.balanceState
	// Copy the map: stores and tests may return shallow Site values.
	state.Recipients = make(map[string]BalanceRecipientState, len(site.balanceState.Recipients))
	for key, value := range site.balanceState.Recipients {
		state.Recipients[key] = value
	}
	observation := s.observeBalance(site, snapshot)
	if observation.reason != "" {
		state.Status.State = "unknown"
		state.Status.LastError = observation.reason
		s.saveBalanceState(ctx, site.ID, state)
		return
	}
	account := snapshot.Catalog.Account
	if account == nil || account.Balance == nil || math.IsNaN(*account.Balance) || math.IsInf(*account.Balance, 0) || account.Unit != config.Unit || config.Unit != balanceUnit(site.Platform) {
		state.Status.State = "unknown"
		state.Status.LastError = "balance_unavailable"
		if account != nil && account.Unit != config.Unit {
			state.Status.LastError = "balance_unit_changed"
		}
		s.saveBalanceState(ctx, site.ID, state)
		return
	}
	now := s.now()
	low := *account.Balance <= config.Threshold
	wasLow := state.Low
	wasRecoveryPending := state.RecoveryPending
	recovered := !low && (wasLow || wasRecoveryPending)
	events := []Event{}
	if low && !state.Low {
		events = append(events, Event{SiteID: site.ID, Kind: "balance_low", After: fmt.Sprintf("%g %s", *account.Balance, config.Unit), CreatedAt: now})
	} else if !low && state.Low {
		events = append(events, Event{SiteID: site.ID, Kind: "balance_recovered", After: fmt.Sprintf("%g %s", *account.Balance, config.Unit), CreatedAt: now})
	}
	state.Low = low
	if low {
		// A new low-balance incident supersedes a recovery notification that
		// may still be waiting for SMTP delivery.
		state.RecoveryPending = false
		for key, recipient := range state.Recipients {
			recipient.RecoveryPending = false
			state.Recipients[key] = recipient
		}
	} else if recovered {
		// Keep the recovery incident durable until every recipient has received
		// the recovery message. This survives process restarts and SMTP failures.
		state.RecoveryPending = true
	}
	state.Status.State = "healthy"
	if low {
		state.Status.State = "low"
	} else {
		state.Status.LastError = ""
	}
	if !s.saveBalanceState(ctx, site.ID, state, events...) || !low && !recovered {
		return
	}
	if s.balanceNotifier == nil {
		state.Status.LastError = "email_unavailable"
		s.saveBalanceState(ctx, site.ID, state)
		return
	}
	recipients, err := s.balanceNotifier.Recipients(ctx, config.Recipients)
	if err == nil {
		recipients, err = normalizeBalanceRecipients(recipients)
	}
	if err != nil || len(recipients) == 0 {
		state.Status.LastError = "recipients_unavailable"
		s.saveBalanceState(ctx, site.ID, state)
		return
	}
	hasPendingRecoveryRecipient := false
	if recovered {
		for _, recipient := range recipients {
			if state.Recipients[balanceRecipientHash(recipient)].RecoveryPending {
				hasPendingRecoveryRecipient = true
				break
			}
		}
	}
	if recovered && (!wasRecoveryPending || !hasPendingRecoveryRecipient) {
		for _, recipient := range recipients {
			key := balanceRecipientHash(recipient)
			delivery := state.Recipients[key]
			delivery.RecoveryPending = true
			delivery.NextAttemptAt = now
			state.Recipients[key] = delivery
		}
		if !s.saveBalanceState(ctx, site.ID, state) {
			return
		}
	}
	notice := BalanceNotice{SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL, Platform: site.Platform, Balance: *account.Balance, Threshold: config.Threshold, Unit: account.Unit, Recovered: recovered, ObservedAt: snapshot.CreatedAt}
	refreshDeliveryError := func() {
		state.Status.LastError = ""
		for _, recipient := range recipients {
			if state.Recipients[balanceRecipientHash(recipient)].Failed {
				state.Status.LastError = "email_delivery_failed"
			}
		}
	}
	refreshDeliveryError()
	for _, recipient := range recipients {
		if ctx.Err() != nil {
			return
		}
		key := balanceRecipientHash(recipient)
		delivery := state.Recipients[key]
		if recovered && !delivery.RecoveryPending {
			continue
		}
		if delivery.NextAttemptAt.After(now) {
			continue
		}
		state.Status.LastAttemptAt = &now
		// Reserve before SMTP. A process interruption or an ambiguous post-send
		// database error cannot cause immediate duplicate messages after restart.
		delivery.NextAttemptAt = addMinutes(now, int64(config.CooldownMinutes))
		state.Recipients[key] = delivery
		if !s.saveBalanceState(ctx, site.ID, state) {
			return
		}
		err = s.balanceNotifier.Send(ctx, recipient, notice)
		delivery.Failed = err != nil
		if err != nil {
			// Delivery failures use a separate retry backoff. The configured
			// interval controls reminders after successful or ambiguous sends.
			delivery.NextAttemptAt = now.Add(15 * time.Minute)
		} else {
			delivery.LastSentAt = &now
			state.Status.LastNotifiedAt = &now
			if recovered {
				delivery.RecoveryPending = false
			}
		}
		state.Recipients[key] = delivery
		refreshDeliveryError()
		// SMTP may have consumed the request deadline. Complete the reservation
		// using a small independent persistence deadline before releasing the lock.
		persistCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ok := s.saveBalanceState(persistCtx, site.ID, state)
		cancel()
		if !ok {
			return
		}
	}
	if recovered {
		state.RecoveryPending = false
		for _, recipient := range recipients {
			if state.Recipients[balanceRecipientHash(recipient)].RecoveryPending {
				state.RecoveryPending = true
				break
			}
		}
	}
	s.saveBalanceState(ctx, site.ID, state)
}
