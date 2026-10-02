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

type readinessChangeNotifier struct {
	readiness BalanceDeliveryReadiness
	calls     int
	override  [][]string
}

func (n *readinessChangeNotifier) Recipients(_ context.Context, override []string) ([]string, error) {
	n.override = append(n.override, append([]string(nil), override...))
	return []string{"admin@example.com"}, nil
}

func (*readinessChangeNotifier) SendChange(context.Context, string, ChangeNotice) error { return nil }

func (n *readinessChangeNotifier) Readiness(_ context.Context, overrides []string) BalanceDeliveryReadiness {
	n.calls++
	n.override = append(n.override, append([]string(nil), overrides...))
	return n.readiness
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
	require.Equal(t, "overview", byKey["authorization"].TargetTab)
	require.Equal(t, "overview", byKey["catalog"].TargetTab)
	require.Equal(t, "import", byKey["bindings"].TargetTab)
	require.Equal(t, "import", byKey["managed_keys"].TargetTab)
	require.Equal(t, "monitor", byKey["automation"].TargetTab)
	require.Equal(t, "monitor", byKey["balance_monitor"].TargetTab)
	require.Equal(t, "monitor", byKey["pricing_protection"].TargetTab)
	require.Equal(t, "monitor", byKey["notifications"].TargetTab)
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

func TestReadinessDoesNotCheckDisabledNotificationEvents(t *testing.T) {
	s, base, _, _ := setupEngine(t)
	base.snap = &Snapshot{ID: 9, SiteID: 1, SiteVersion: 1, CreatedAt: time.Now().UTC(), Catalog: Catalog{GroupsComplete: true}}
	store := &readinessStore{
		memoryStore: base,
		pricing:     PricingPoliciesConfiguration{Version: 1, Notifications: PricingNotificationPolicy{Enabled: true}},
	}
	s.store = store
	notifier := &readinessChangeNotifier{readiness: BalanceDeliveryReadiness{Ready: true, RecipientCount: 1, Reason: "ready"}}
	s.SetChangeNotifier(notifier)
	// The fixture store does not implement the optional durable queue. Keep the
	// runtime wiring present so this test reaches the event-policy branch.
	s.changeQueue = NewChangeNotificationQueue(nil, changeNotificationSender{notifier: notifier}, nil)

	overview, err := s.Readiness(t.Context(), 1)
	require.NoError(t, err)
	check := readinessCheckByKey(overview.Checks, "notifications")
	require.Equal(t, ReadinessNotConfigured, check.State)
	require.Equal(t, "notifications_events_missing", check.Detail)
	require.Zero(t, notifier.calls, "a policy with no event kinds must not inspect mail readiness")
}

func TestReadinessUsesChangeNotifierDeliveryReadinessAndAllowsFallbackRecipients(t *testing.T) {
	s, base, _, _ := setupEngine(t)
	base.snap = &Snapshot{ID: 9, SiteID: 1, SiteVersion: 1, CreatedAt: time.Now().UTC(), Catalog: Catalog{GroupsComplete: true}}
	store := &readinessStore{
		memoryStore: base,
		pricing:     PricingPoliciesConfiguration{Version: 1, Notifications: PricingNotificationPolicy{Enabled: true, RateChanges: true}},
	}
	s.store = store
	notifier := &readinessChangeNotifier{readiness: BalanceDeliveryReadiness{Ready: true, RecipientCount: 1, Reason: "ready"}}
	s.SetChangeNotifier(notifier)
	s.changeQueue = NewChangeNotificationQueue(nil, changeNotificationSender{notifier: notifier}, nil)

	overview, err := s.Readiness(t.Context(), 1)
	require.NoError(t, err)
	check := readinessCheckByKey(overview.Checks, "notifications")
	require.Equal(t, ReadinessConfigured, check.State)
	require.Empty(t, check.Detail)
	require.Equal(t, 1, notifier.calls)
	// An empty site override is intentional: the runtime resolver may fall back
	// to the configured administrator recipient.
	require.Len(t, notifier.override, 1)
	require.Empty(t, notifier.override[0])
}

func TestReadinessReportsNotificationDeliveryConfigurationFailures(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		state  ReadinessState
		detail string
	}{
		{name: "recipients", reason: "recipients_unavailable", state: ReadinessPending, detail: "notifications_recipients_unavailable"},
		{name: "smtp missing", reason: "smtp_not_configured", state: ReadinessPending, detail: "notifications_smtp_not_configured"},
		{name: "smtp invalid", reason: "smtp_invalid", state: ReadinessPending, detail: "notifications_smtp_invalid"},
		{name: "mail read failure", reason: "email_unavailable", state: ReadinessReadFailed, detail: "notifications_email_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, base, _, _ := setupEngine(t)
			base.snap = &Snapshot{ID: 9, SiteID: 1, SiteVersion: 1, CreatedAt: time.Now().UTC(), Catalog: Catalog{GroupsComplete: true}}
			store := &readinessStore{memoryStore: base, pricing: PricingPoliciesConfiguration{Version: 1, Notifications: PricingNotificationPolicy{Enabled: true, GroupChanges: true}}}
			s.store = store
			notifier := &readinessChangeNotifier{readiness: BalanceDeliveryReadiness{Ready: false, RecipientCount: 1, Reason: tc.reason}}
			s.SetChangeNotifier(notifier)
			s.changeQueue = NewChangeNotificationQueue(nil, changeNotificationSender{notifier: notifier}, nil)

			overview, err := s.Readiness(t.Context(), 1)
			require.NoError(t, err)
			check := readinessCheckByKey(overview.Checks, "notifications")
			require.Equal(t, tc.state, check.State)
			require.Equal(t, tc.detail, check.Detail)
			require.Equal(t, 1, notifier.calls)
		})
	}
}

func TestReadinessDoesNotCallUnavailableNotificationRuntime(t *testing.T) {
	s, base, _, _ := setupEngine(t)
	base.snap = &Snapshot{ID: 9, SiteID: 1, SiteVersion: 1, CreatedAt: time.Now().UTC(), Catalog: Catalog{GroupsComplete: true}}
	s.store = &readinessStore{memoryStore: base, pricing: PricingPoliciesConfiguration{Version: 1, Notifications: PricingNotificationPolicy{Enabled: true, GroupChanges: true}}}

	overview, err := s.Readiness(t.Context(), 1)
	require.NoError(t, err)
	check := readinessCheckByKey(overview.Checks, "notifications")
	require.Equal(t, ReadinessReadFailed, check.State)
	require.Equal(t, "notifications_runtime_unavailable", check.Detail)
}

func TestReadinessBalanceMonitorRequiresNotificationDelivery(t *testing.T) {
	check := readinessBalanceCheck(Site{BalanceMonitor: BalanceMonitorConfig{Enabled: true}}, &BalanceHealthResult{State: "healthy", Reason: "healthy", DeliveryReady: false, DeliveryReason: "smtp_invalid"}, nil)
	require.Equal(t, ReadinessPending, check.State)
	require.Equal(t, "balance_notification_smtp_invalid", check.Detail)
}

func readinessCheckByKey(checks []ReadinessCheck, key string) ReadinessCheck {
	for _, check := range checks {
		if check.Key == key {
			return check
		}
	}
	return ReadinessCheck{Key: key}
}
