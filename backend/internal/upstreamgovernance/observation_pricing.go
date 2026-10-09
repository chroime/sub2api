package upstreamgovernance

import (
	"context"
	"math"
	"time"
)

type ObservationPolicy struct {
	Enabled                  bool  `json:"enabled"`
	FastIntervalSeconds      int64 `json:"fast_interval_seconds"`
	FullIntervalSeconds      int64 `json:"full_interval_seconds"`
	DecreaseStabilitySeconds int64 `json:"decrease_stability_seconds"`
	MaxRateIncreasePercent   float64 `json:"max_rate_increase_percent"`
}

type ObservationPolicyStatus struct {
	LastFastObservedAt    *time.Time `json:"last_fast_observed_at"`
	LastFullCollectedAt   *time.Time `json:"last_full_collected_at"`
	NextFastObservationAt time.Time  `json:"next_fast_observation_at"`
	FastObserveStatus     string     `json:"fast_observe_status"`
	FastObserveError      string     `json:"fast_observe_error"`
	FastObserveRevision   int64      `json:"fast_observe_revision"`
}

type ObservationPolicyConfiguration struct {
	Version int64                    `json:"version"`
	Policy  ObservationPolicy        `json:"policy"`
	Status  ObservationPolicyStatus  `json:"status"`
}

func validateObservationPolicy(policy ObservationPolicy) error {
	if !validIntervalSeconds(policy.FastIntervalSeconds) || !validIntervalSeconds(policy.FullIntervalSeconds) || policy.DecreaseStabilitySeconds < 0 || math.IsNaN(policy.MaxRateIncreasePercent) || math.IsInf(policy.MaxRateIncreasePercent, 0) || policy.MaxRateIncreasePercent < 0 || policy.MaxRateIncreasePercent > 10000 {
		return ErrInvalid
	}
	return nil
}

func observationPolicyForSite(site Site) ObservationPolicyConfiguration {
	fast := site.FastIntervalSeconds
	full := site.FullIntervalSeconds
	if full <= 0 {
		full = int64(site.IntervalMinutes) * 60
	}
	if fast <= 0 {
		fast = full
	}
	return ObservationPolicyConfiguration{
		Version: site.Version,
		Policy: ObservationPolicy{Enabled: site.FastObserveEnabled && site.Enabled && site.SessionCipher != "", FastIntervalSeconds: fast, FullIntervalSeconds: full, DecreaseStabilitySeconds: 60, MaxRateIncreasePercent: 20},
		Status: ObservationPolicyStatus{LastFastObservedAt: site.LastFastObserveAt, LastFullCollectedAt: site.LastSyncAt, NextFastObservationAt: site.NextFastObserveAt, FastObserveStatus: site.FastObserveStatus, FastObserveError: site.FastObserveError, FastObserveRevision: site.FastObserveRevision},
	}
}

func (s *Service) ObservationPolicy(ctx context.Context, id int64) (ObservationPolicyConfiguration, error) {
	site, err := s.store.GetSite(ctx, id)
	if err != nil {
		return ObservationPolicyConfiguration{}, err
	}
	return observationPolicyForSite(*site), nil
}

func (s *Service) ConfigureObservationPolicy(ctx context.Context, id, version int64, policy ObservationPolicy) (ObservationPolicyConfiguration, error) {
	if err := validateObservationPolicy(policy); err != nil {
		return ObservationPolicyConfiguration{}, err
	}
	site, release, err := s.remoteSiteLock(ctx, id)
	if err != nil {
		return ObservationPolicyConfiguration{}, err
	}
	defer release()
	if site.Version != version {
		return ObservationPolicyConfiguration{}, ErrConflict
	}
	site.FastIntervalSeconds = policy.FastIntervalSeconds
	site.FullIntervalSeconds = policy.FullIntervalSeconds
	site.FastObserveEnabled = policy.Enabled
	// The legacy column is a ceiling alias only; exact seconds remain in the
	// BIGINT field and are used by the scheduler.
	site.IntervalMinutes = int((policy.FullIntervalSeconds + 59) / 60)
	if site.IntervalMinutes < 1 || site.IntervalMinutes > maxIntervalMinutes {
		return ObservationPolicyConfiguration{}, ErrInvalid
	}
	site.NextFastObserveAt = s.now()
	if err := validateSite(site); err != nil {
		return ObservationPolicyConfiguration{}, err
	}
	if err := s.store.UpdateSite(ctx, site, version); err != nil {
		return ObservationPolicyConfiguration{}, err
	}
	return observationPolicyForSite(*site), nil
}

type PricingNotificationPolicy struct {
	Enabled             bool     `json:"enabled"`
	Recipients          []string `json:"recipients"`
	GroupChanges        bool     `json:"group_changes"`
	RateChanges         bool     `json:"rate_changes"`
	PricingChanges      bool     `json:"pricing_changes"`
	ProtectionChanges   bool     `json:"protection_changes"`
}

type PricingPolicyView struct {
	PricingPolicy
	LocalGroupName string             `json:"local_group_name"`
	CurrentCost    *float64           `json:"current_cost"`
	CurrentSale    *float64           `json:"current_sale"`
	TargetSale     *float64           `json:"target_sale"`
	Status         string             `json:"status"`
	Sources        []CostObservation  `json:"sources"`
}

type PricingPoliciesConfiguration struct {
	Version       int64                    `json:"version"`
	Policies      []PricingPolicyView      `json:"policies"`
	Notifications PricingNotificationPolicy `json:"notifications"`
}

type PricingPolicyStore interface {
	PricingPersistence
	ListPricingPolicies(context.Context, int64) (PricingPoliciesConfiguration, error)
	SavePricingPolicies(context.Context, int64, PricingPoliciesConfiguration) (PricingPoliciesConfiguration, error)
}

func (s *Service) PricingPolicies(ctx context.Context, id int64) (PricingPoliciesConfiguration, error) {
	if _, err := s.store.GetSite(ctx, id); err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	store, ok := s.store.(PricingPolicyStore)
	if !ok {
		return PricingPoliciesConfiguration{}, ErrUnsupported
	}
	return store.ListPricingPolicies(ctx, id)
}

func (s *Service) ConfigurePricingPolicies(ctx context.Context, id int64, input PricingPoliciesConfiguration) (PricingPoliciesConfiguration, error) {
	if input.Version < 0 {
		return PricingPoliciesConfiguration{}, ErrInvalid
	}
	if _, release, err := s.remoteSiteLock(ctx, id); err != nil {
		return PricingPoliciesConfiguration{}, err
	} else {
		defer release()
	}
	store, ok := s.store.(PricingPolicyStore)
	if !ok {
		return PricingPoliciesConfiguration{}, ErrUnsupported
	}
	return store.SavePricingPolicies(ctx, id, input)
}
