package upstreamgovernance

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ReadinessState is intentionally a small, administrator-facing vocabulary.
// It describes configuration/readiness only; it never implies that an action
// has been performed by this endpoint.
type ReadinessState string

const (
	ReadinessConfigured    ReadinessState = "configured"
	ReadinessNotConfigured ReadinessState = "not_configured"
	ReadinessNotEnabled    ReadinessState = "not_enabled"
	ReadinessPending       ReadinessState = "pending"
	ReadinessReadFailed    ReadinessState = "read_failed"
)

// ReadinessCheck is a non-secret summary of one governance capability.
// Count is an optional safe count (for example, imported bindings or managed
// keys); no credentials, key material, recipient addresses, or raw payloads
// are included here.
type ReadinessCheck struct {
	Key       string         `json:"key"`
	State     ReadinessState `json:"state"`
	Detail    string         `json:"detail,omitempty"`
	Count     int            `json:"count,omitempty"`
	TargetTab string         `json:"target_tab"`
}

// ReadinessOverview is deliberately read-only. Callers can use TargetTab to
// navigate to the relevant workbench panel, but this endpoint never triggers
// authorization, collection, key repair, pricing, mail, or payment work.
type ReadinessOverview struct {
	SiteID      int64            `json:"site_id"`
	SiteName    string           `json:"site_name"`
	BaseURL     string           `json:"base_url"`
	SiteStatus  string           `json:"site_status"`
	Version     int64            `json:"version"`
	Complete    bool             `json:"complete"`
	EvaluatedAt time.Time        `json:"evaluated_at"`
	Checks      []ReadinessCheck `json:"checks"`
}

type readinessAutomationReader interface {
	GetAutomation(context.Context, int64) (AutomationConfig, error)
}

type readinessPricingReader interface {
	ListPricingPolicies(context.Context, int64) (PricingPoliciesConfiguration, error)
}

func readinessCheck(key, target string, state ReadinessState, detail string, count int) ReadinessCheck {
	return ReadinessCheck{Key: key, TargetTab: target, State: state, Detail: detail, Count: count}
}

func readinessSessionCheck(site Site) ReadinessCheck {
	if site.Status == "reauth_required" {
		return readinessCheck("authorization", "connect", ReadinessPending, "reauthorization_required", 0)
	}
	if !site.HasCredential && strings.TrimSpace(site.SessionCipher) == "" {
		return readinessCheck("authorization", "connect", ReadinessNotConfigured, "session_missing", 0)
	}
	// SessionCipher is encrypted at rest. Do not include or expose it in the
	// DTO; the presence flag is enough for this read-only checklist. A site
	// marked disconnected while retaining a session still needs review.
	if site.Status == "disconnected" || site.Status == "error" {
		return readinessCheck("authorization", "connect", ReadinessPending, "authorization_state_unconfirmed", 0)
	}
	return readinessCheck("authorization", "connect", ReadinessConfigured, "", 0)
}

func readinessCatalogCheck(snapshot *Snapshot, site Site, err error) ReadinessCheck {
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return readinessCheck("catalog", "monitor", ReadinessNotConfigured, "catalog_missing", 0)
		}
		return readinessCheck("catalog", "monitor", ReadinessReadFailed, "catalog_read_failed", 0)
	}
	if snapshot == nil {
		return readinessCheck("catalog", "monitor", ReadinessNotConfigured, "catalog_missing", 0)
	}
	if snapshot.SiteVersion != site.Version {
		return readinessCheck("catalog", "monitor", ReadinessPending, "snapshot_outdated", len(snapshot.Catalog.Groups))
	}
	if !snapshot.Catalog.GroupsComplete {
		return readinessCheck("catalog", "monitor", ReadinessPending, "groups_incomplete", len(snapshot.Catalog.Groups))
	}
	return readinessCheck("catalog", "monitor", ReadinessConfigured, "", len(snapshot.Catalog.Groups))
}

func readinessBindingsCheck(bindings []Binding, err error) ReadinessCheck {
	if err != nil {
		return readinessCheck("bindings", "import", ReadinessReadFailed, "bindings_read_failed", 0)
	}
	if len(bindings) == 0 {
		return readinessCheck("bindings", "import", ReadinessNotConfigured, "bindings_missing", 0)
	}
	for _, binding := range bindings {
		if binding.AccountID <= 0 || binding.AccountDeleted {
			return readinessCheck("bindings", "import", ReadinessPending, "binding_account_pending", len(bindings))
		}
	}
	return readinessCheck("bindings", "import", ReadinessConfigured, "", len(bindings))
}

func readinessManagedKeysCheck(keys []ManagedKey, err error) ReadinessCheck {
	if err != nil {
		return readinessCheck("managed_keys", "keys", ReadinessReadFailed, "managed_keys_read_failed", 0)
	}
	if len(keys) == 0 {
		return readinessCheck("managed_keys", "keys", ReadinessNotConfigured, "managed_keys_missing", 0)
	}
	for _, key := range keys {
		if !key.HasKey || key.Health.Status != KeyHealthPresent {
			return readinessCheck("managed_keys", "keys", ReadinessPending, "key_health_pending", len(keys))
		}
	}
	return readinessCheck("managed_keys", "keys", ReadinessConfigured, "", len(keys))
}

func readinessAutomationCheck(config AutomationConfig, err error) ReadinessCheck {
	if err != nil {
		return readinessCheck("automation", "monitor", ReadinessReadFailed, "automation_read_failed", 0)
	}
	if config.Version == 0 {
		return readinessCheck("automation", "monitor", ReadinessNotConfigured, "automation_missing", 0)
	}
	if !config.Policy.Enabled {
		return readinessCheck("automation", "monitor", ReadinessNotEnabled, "automation_disabled", 0)
	}
	return readinessCheck("automation", "monitor", ReadinessConfigured, "", 0)
}

func readinessBalanceCheck(site Site, health *BalanceHealthResult, err error) ReadinessCheck {
	if !site.BalanceMonitor.Enabled {
		return readinessCheck("balance_monitor", "monitor", ReadinessNotEnabled, "balance_monitor_disabled", 0)
	}
	if err != nil || health == nil {
		return readinessCheck("balance_monitor", "monitor", ReadinessReadFailed, "balance_monitor_read_failed", 0)
	}
	if health.State == "unknown" || health.Stale || health.Reason == "reauth_required" || health.Reason == "collection_failed" {
		detail := health.Reason
		if detail == "" {
			detail = "balance_observation_pending"
		}
		return readinessCheck("balance_monitor", "monitor", ReadinessPending, detail, 0)
	}
	return readinessCheck("balance_monitor", "monitor", ReadinessConfigured, "", 0)
}

func readinessPricingChecks(config PricingPoliciesConfiguration, err error) (ReadinessCheck, ReadinessCheck) {
	if err != nil {
		return readinessCheck("pricing_protection", "monitor", ReadinessReadFailed, "pricing_read_failed", 0), readinessCheck("notifications", "monitor", ReadinessReadFailed, "notifications_read_failed", 0)
	}
	pricing := readinessCheck("pricing_protection", "monitor", ReadinessNotConfigured, "pricing_policy_missing", len(config.Policies))
	if len(config.Policies) > 0 {
		pricing.State = ReadinessNotEnabled
		pricing.Detail = "pricing_protection_disabled"
		for _, policy := range config.Policies {
			if policy.Enabled {
				pricing.State = ReadinessConfigured
				pricing.Detail = ""
				break
			}
		}
	}
	notifications := readinessCheck("notifications", "monitor", ReadinessNotEnabled, "notifications_disabled", 0)
	if config.Notifications.Enabled {
		notifications.State = ReadinessConfigured
		notifications.Detail = ""
	}
	return pricing, notifications
}

// Readiness returns a per-capability, non-mutating integration checklist.
// Individual reads are intentionally represented as read_failed states so a
// single optional store/API failure does not make the entire overview unusable.
func (s *Service) Readiness(ctx context.Context, siteID int64) (ReadinessOverview, error) {
	if siteID <= 0 || s == nil || s.store == nil {
		return ReadinessOverview{}, ErrInvalid
	}
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return ReadinessOverview{}, err
	}
	if site == nil {
		return ReadinessOverview{}, ErrNotFound
	}

	snapshot, snapshotErr := s.store.LatestSnapshot(ctx, siteID)
	authorization := readinessSessionCheck(*site)
	if authorization.State == ReadinessConfigured && strings.TrimSpace(site.SessionCipher) != "" {
		if _, sessionErr := s.session(*site); sessionErr != nil {
			if errors.Is(sessionErr, ErrReauth) {
				authorization.State = ReadinessPending
				authorization.Detail = "session_unavailable"
			} else {
				authorization.State = ReadinessReadFailed
				authorization.Detail = "authorization_read_failed"
			}
		}
	}
	// A connected session with no successful catalog read is not yet a
	// complete integration. Keep the authorization row actionable until the
	// first trusted snapshot exists; this avoids presenting a half-configured
	// site as fully ready after a partial setup or a failed initial collection.
	if authorization.State == ReadinessConfigured && errors.Is(snapshotErr, ErrNotFound) {
		authorization.State = ReadinessPending
		authorization.Detail = "catalog_missing"
	}
	checks := make([]ReadinessCheck, 0, 8)
	checks = append(checks, authorization)
	checks = append(checks, readinessCatalogCheck(snapshot, *site, snapshotErr))

	bindings, bindingsErr := s.store.ListBindings(ctx, siteID)
	checks = append(checks, readinessBindingsCheck(bindings, bindingsErr))

	keys, keysErr := s.store.ListManagedKeys(ctx, siteID)
	checks = append(checks, readinessManagedKeysCheck(keys, keysErr))

	var automation AutomationConfig
	var automationErr error
	if reader, ok := s.store.(readinessAutomationReader); ok {
		automation, automationErr = reader.GetAutomation(ctx, siteID)
	} else {
		automationErr = ErrUnsupported
	}
	checks = append(checks, readinessAutomationCheck(automation, automationErr))

	var health *BalanceHealthResult
	var healthErr error
	if site.BalanceMonitor.Enabled {
		health, healthErr = s.BalanceHealth(ctx, siteID)
	}
	checks = append(checks, readinessBalanceCheck(*site, health, healthErr))

	var pricing PricingPoliciesConfiguration
	var pricingErr error
	if reader, ok := s.store.(readinessPricingReader); ok {
		pricing, pricingErr = reader.ListPricingPolicies(ctx, siteID)
	} else {
		pricingErr = ErrUnsupported
	}
	pricingCheck, notificationCheck := readinessPricingChecks(pricing, pricingErr)
	checks = append(checks, pricingCheck, notificationCheck)

	complete := true
	for _, check := range checks {
		if check.State != ReadinessConfigured {
			complete = false
			break
		}
	}
	return ReadinessOverview{SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL, SiteStatus: site.Status, Version: site.Version, Complete: complete, EvaluatedAt: s.now().UTC(), Checks: checks}, nil
}
