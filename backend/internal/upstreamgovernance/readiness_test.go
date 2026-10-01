package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type readinessStore struct {
	*memoryStore
	automation AutomationConfig
	pricing    PricingPoliciesConfiguration
	fail       map[string]error
}

func (s *readinessStore) GetAutomation(context.Context, int64) (AutomationConfig, error) {
	if err := s.fail["automation"]; err != nil {
		return AutomationConfig{}, err
	}
	return s.automation, nil
}

func (s *readinessStore) ListPricingPolicies(context.Context, int64) (PricingPoliciesConfiguration, error) {
	if err := s.fail["pricing"]; err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	return s.pricing, nil
}

func (s *readinessStore) SavePricingPolicies(context.Context, int64, PricingPoliciesConfiguration) (PricingPoliciesConfiguration, error) {
	return s.pricing, nil
}

func TestReadinessReportsConfiguredAndNotEnabledCapabilitiesWithoutSideEffects(t *testing.T) {
	s, base, _, _ := setupEngine(t)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	balance := 20.0
	base.snap = &Snapshot{ID: 9, SiteID: 1, SiteVersion: 1, CreatedAt: now.Add(-time.Minute), Catalog: Catalog{
		GroupsComplete: true,
		Groups:         []RemoteGroup{{ID: "g1", Name: "Claude", Platform: "openai"}},
		Account:        &RemoteAccount{UserID: 5, Balance: &balance, Unit: "usd"},
	}}
	base.bindings = []Binding{{ID: 1, SiteID: 1, RemoteGroupID: "g1", AccountID: 10}}
	base.keys = []ManagedKey{{ID: 1, SiteID: 1, RemoteGroupID: "g1", HasKey: true, Health: KeyHealth{Status: "present"}}}
	store := &readinessStore{memoryStore: base, automation: AutomationConfig{Version: 1, Policy: AutomationPolicy{Enabled: true}}, pricing: PricingPoliciesConfiguration{Version: 1, Policies: []PricingPolicyView{{PricingPolicy: PricingPolicy{Enabled: true}}}, Notifications: PricingNotificationPolicy{Enabled: false}}}
	s.store = store

	overview, err := s.Readiness(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, overview.Checks, 8)
	byKey := map[string]ReadinessCheck{}
	for _, check := range overview.Checks {
		byKey[check.Key] = check
	}
	require.Equal(t, ReadinessConfigured, byKey["authorization"].State)
	require.Equal(t, ReadinessConfigured, byKey["catalog"].State)
	require.Equal(t, ReadinessConfigured, byKey["bindings"].State)
	require.Equal(t, ReadinessConfigured, byKey["managed_keys"].State)
	require.Equal(t, ReadinessConfigured, byKey["automation"].State)
	require.Equal(t, ReadinessNotEnabled, byKey["balance_monitor"].State)
	require.Equal(t, ReadinessConfigured, byKey["pricing_protection"].State)
	require.Equal(t, ReadinessNotEnabled, byKey["notifications"].State)
	require.Equal(t, 1, byKey["bindings"].Count)
	require.Equal(t, 1, byKey["managed_keys"].Count)
}

func TestReadinessKeepsEachReadFailureExplicitAndNeverPromotesItToConfigured(t *testing.T) {
	s, base, _, _ := setupEngine(t)
	store := &readinessStore{
		memoryStore: base,
		automation:  AutomationConfig{Version: 1, Policy: AutomationPolicy{Enabled: true}},
		pricing:     PricingPoliciesConfiguration{Version: 1, Policies: []PricingPolicyView{{PricingPolicy: PricingPolicy{Enabled: true}}}, Notifications: PricingNotificationPolicy{Enabled: true}},
		fail:        map[string]error{"automation": errors.New("automation read failed"), "pricing": errors.New("pricing read failed")},
	}
	s.store = store
	base.snap = nil
	base.bindings = nil
	base.keys = nil

	overview, err := s.Readiness(t.Context(), 1)
	require.NoError(t, err)
	byKey := map[string]ReadinessCheck{}
	for _, check := range overview.Checks {
		byKey[check.Key] = check
	}
	require.Equal(t, ReadinessReadFailed, byKey["automation"].State)
	require.Equal(t, ReadinessReadFailed, byKey["pricing_protection"].State)
	require.Equal(t, ReadinessReadFailed, byKey["notifications"].State)
	require.Equal(t, ReadinessNotConfigured, byKey["catalog"].State)
	require.Equal(t, ReadinessNotConfigured, byKey["bindings"].State)
	require.Equal(t, ReadinessNotConfigured, byKey["managed_keys"].State)
	for _, check := range overview.Checks {
		require.NotEqual(t, ReadinessConfigured, check.State, check.Key)
	}
}

func TestReadinessMarksReauthorizationAndIncompleteCatalogAsPending(t *testing.T) {
	s, base, _, _ := setupEngine(t)
	base.site.Status = "reauth_required"
	base.snap = &Snapshot{ID: 3, SiteID: 1, SiteVersion: 1, CreatedAt: time.Now().UTC(), Catalog: Catalog{GroupsComplete: false}}
	s.store = &readinessStore{memoryStore: base, automation: DefaultAutomationConfig(), pricing: PricingPoliciesConfiguration{Notifications: PricingNotificationPolicy{}}}
	overview, err := s.Readiness(t.Context(), 1)
	require.NoError(t, err)
	byKey := map[string]ReadinessCheck{}
	for _, check := range overview.Checks {
		byKey[check.Key] = check
	}
	require.Equal(t, ReadinessPending, byKey["authorization"].State)
	require.Equal(t, ReadinessPending, byKey["catalog"].State)
	require.Equal(t, "reauthorization_required", byKey["authorization"].Detail)
	require.Equal(t, "groups_incomplete", byKey["catalog"].Detail)
}
