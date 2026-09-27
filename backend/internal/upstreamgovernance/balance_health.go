package upstreamgovernance

import (
	"context"
	"errors"
	"math"
	"time"
)

// Readiness checks configuration only; it never connects to SMTP or sends mail.
type BalanceDeliveryReadiness struct {
	Ready          bool
	RecipientCount int
	Reason         string
}

type balanceReadinessNotifier interface {
	Readiness(context.Context, []string) BalanceDeliveryReadiness
}

type BalanceHealthResult struct {
	CollectionEnabled bool       `json:"collection_enabled"`
	IntervalMinutes   int        `json:"interval_minutes"`
	LastAttemptAt     *time.Time `json:"last_attempt_at"`
	ObservedAt        *time.Time `json:"observed_at"`
	NextRunAt         *time.Time `json:"next_run_at"`
	Stale             bool       `json:"stale"`
	MonitorEnabled    bool       `json:"monitor_enabled"`
	State             string     `json:"state"`
	DeliveryReady     bool       `json:"delivery_ready"`
	RecipientCount    int        `json:"recipient_count"`
	Reason            string     `json:"reason"`
	DeliveryReason    string     `json:"delivery_reason"`
	LastNotifiedAt    *time.Time `json:"last_notified_at"`
	LastDeliveryError string     `json:"last_delivery_error"`
}

type balanceObservation struct {
	balance    *float64
	observedAt *time.Time
	unit       string
	stale      bool
	reason     string
}

func (s *Service) latestBalanceSnapshot(ctx context.Context, id int64) (*Snapshot, error) {
	snapshot, err := s.store.LatestSnapshot(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return snapshot, err
}

// A snapshot records the last successful observation. Failed collection attempts
// update LastSyncAt, so that field must never be used as a freshness timestamp.
func (s *Service) observeBalance(site Site, snapshot *Snapshot) balanceObservation {
	value := balanceObservation{stale: true, reason: "balance_unavailable", unit: balanceUnit(site.Platform)}
	if snapshot == nil {
		return value
	}
	observed := snapshot.CreatedAt
	value.observedAt = &observed
	if snapshot.SiteVersion != site.Version {
		value.reason = "snapshot_outdated"
		return value
	}
	maxAgeMinutes := max(2*int64(site.IntervalMinutes), 10)
	if observed.IsZero() || !s.now().Before(addMinutes(observed, maxAgeMinutes)) || observed.After(s.now().Add(time.Minute)) {
		value.reason = "balance_stale"
		return value
	}
	account := snapshot.Catalog.Account
	if account == nil || account.Balance == nil || math.IsNaN(*account.Balance) || math.IsInf(*account.Balance, 0) {
		return value
	}
	if account.Unit != value.unit {
		value.reason = "balance_unit_changed"
		return value
	}
	session, err := s.session(site)
	if err != nil {
		value.reason = "session_unavailable"
		return value
	}
	if account.UserID <= 0 || account.UserID != session.UserID {
		value.reason = "snapshot_outdated"
		return value
	}
	balance := *account.Balance
	value.balance, value.stale, value.reason = &balance, false, ""
	return value
}

func (s *Service) BalanceHealth(ctx context.Context, siteID int64) (*BalanceHealthResult, error) {
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.latestBalanceSnapshot(ctx, siteID)
	if err != nil {
		return nil, err
	}
	observation := s.observeBalance(*site, snapshot)
	result := &BalanceHealthResult{
		CollectionEnabled: site.Enabled, IntervalMinutes: site.IntervalMinutes,
		LastAttemptAt: site.LastSyncAt, ObservedAt: observation.observedAt,
		Stale: observation.stale, MonitorEnabled: site.BalanceMonitor.Enabled,
		State: "unknown", Reason: observation.reason,
		DeliveryReason: "email_unavailable", LastNotifiedAt: site.balanceState.Status.LastNotifiedAt,
	}
	if site.Enabled && site.SessionCipher != "" {
		next := site.NextSyncAt
		result.NextRunAt = &next
	}
	if site.Status == "reauth_required" {
		result.Reason, result.Stale = "reauth_required", true
	} else if site.Status == "error" {
		result.Reason, result.Stale = "collection_failed", true
	}
	if result.Reason == "" {
		result.State, result.Reason = "healthy", "healthy"
		if observation.balance != nil && *observation.balance <= site.BalanceMonitor.Threshold {
			result.State, result.Reason = "low", "low"
		}
	}
	if !site.Enabled {
		result.State, result.Reason = "unknown", "collection_disabled"
	}
	if !site.BalanceMonitor.Enabled {
		result.State, result.Reason = "disabled", "disabled"
	}
	if notifier, ok := s.balanceNotifier.(balanceReadinessNotifier); ok {
		ready := notifier.Readiness(ctx, site.BalanceMonitor.Recipients)
		result.DeliveryReady, result.RecipientCount, result.DeliveryReason = ready.Ready, ready.RecipientCount, ready.Reason
	}
	switch site.balanceState.Status.LastError {
	case "email_delivery_failed", "recipients_unavailable", "email_unavailable":
		result.LastDeliveryError = site.balanceState.Status.LastError
	}
	return result, nil
}
