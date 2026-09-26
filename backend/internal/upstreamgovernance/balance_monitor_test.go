package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBalanceMonitorDisabledDefaultsArePublic(t *testing.T) {
	s, _, _, _ := setupEngine(t)
	site, err := s.CreateSite(t.Context(), Site{Name: "fixture", Platform: "newapi", BaseURL: "https://example.com", Enabled: true, IntervalMinutes: 15})
	require.NoError(t, err)
	raw, err := json.Marshal(site)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.NotNil(t, payload["balance_monitor"], "every site must expose its balance-monitor configuration")
	monitor := payload["balance_monitor"].(map[string]any)
	require.Equal(t, false, monitor["enabled"])
	require.Equal(t, "quota", monitor["unit"])
	require.Equal(t, float64(10), monitor["threshold"])
	require.Equal(t, float64(1440), monitor["cooldown_minutes"])
}

type balanceTestStore struct {
	*memoryStore
	failState bool
	saveCalls int
	failAt    int
}

func (m *balanceTestStore) SaveBalanceMonitorState(_ context.Context, _ int64, state BalanceMonitorState, events []Event) error {
	m.saveCalls++
	if m.failState || m.saveCalls == m.failAt {
		return errors.New("state unavailable")
	}
	raw, _ := json.Marshal(state)
	var persisted BalanceMonitorState
	_ = json.Unmarshal(raw, &persisted)
	m.site.balanceState = persisted
	m.site.BalanceMonitorStatus = persisted.Status
	m.events = append(m.events, events...)
	return nil
}

type balanceTestNotifier struct {
	addresses []string
	sent      map[string][]BalanceNotice
	failing   map[string]bool
}

func (n *balanceTestNotifier) Recipients(_ context.Context, override []string) ([]string, error) {
	if len(override) > 0 {
		return override, nil
	}
	return n.addresses, nil
}
func (n *balanceTestNotifier) Send(_ context.Context, recipient string, notice BalanceNotice) error {
	n.sent[recipient] = append(n.sent[recipient], notice)
	if n.failing[recipient] {
		return errors.New("SMTP fixture-sensitive error")
	}
	return nil
}

func balanceEngine(t *testing.T) (*Service, *balanceTestStore, *fakeConnector, *balanceTestNotifier, *time.Time) {
	t.Helper()
	s, m, c, _ := setupEngine(t)
	store := &balanceTestStore{memoryStore: m}
	s.store = store
	n := &balanceTestNotifier{addresses: []string{"admin@example.com"}, sent: map[string][]BalanceNotice{}, failing: map[string]bool{}}
	s.SetBalanceNotifier(n)
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	m.site.BalanceMonitor = defaultBalanceMonitor("sub2api")
	m.site.BalanceMonitor.Enabled = true
	balance, frozen := 10.0, 5.0
	c.catalog.Account = &RemoteAccount{UserID: 5, Balance: &balance, FrozenBalance: &frozen, Unit: "usd"}
	return s, store, c, n, &now
}

func TestBalanceMonitorFirstLowBoundaryCooldownAndRecovery(t *testing.T) {
	s, m, c, n, now := balanceEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 1)
	require.Equal(t, 10.0, n.sent["admin@example.com"][0].Balance, "frozen balance is already excluded upstream")
	require.Equal(t, "low", m.site.BalanceMonitorStatus.State)
	*now = now.Add(time.Hour)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 1)
	balance := 11.0
	c.catalog.Account.Balance = &balance
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "healthy", m.site.BalanceMonitorStatus.State)
	require.Len(t, n.sent["admin@example.com"], 1, "recovery only records an event")
	balance = 9
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 1, "flapping preserves cooldown")
	*now = now.Add(23 * time.Hour)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 2)
	kinds := []string{}
	for _, event := range m.events {
		if event.Kind == "balance_low" || event.Kind == "balance_recovered" {
			kinds = append(kinds, event.Kind)
		}
	}
	require.Equal(t, []string{"balance_low", "balance_recovered", "balance_low"}, kinds)
}

func TestBalanceMonitorRetriesOnlyFailedRecipientsAndPersistsAcrossRestart(t *testing.T) {
	s, m, _, n, now := balanceEngine(t)
	n.addresses = []string{"ok@example.com", "fail@example.com"}
	n.failing["fail@example.com"] = true
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err, "SMTP failure does not fail successful collection")
	require.Equal(t, "healthy", m.site.Status)
	require.Equal(t, "email_delivery_failed", m.site.BalanceMonitorStatus.LastError)
	*now = now.Add(14 * time.Minute)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["fail@example.com"], 1)
	require.Equal(t, "email_delivery_failed", m.site.BalanceMonitorStatus.LastError, "a pending retry remains visible")
	*now = now.Add(time.Minute)
	n.failing["fail@example.com"] = false
	restarted := NewService(m, s.connector, s.local, s.cipher, true)
	restarted.now = s.now
	restarted.SetBalanceNotifier(n)
	_, err = restarted.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["ok@example.com"], 1)
	require.Len(t, n.sent["fail@example.com"], 2)
	require.Empty(t, m.site.BalanceMonitorStatus.LastError)
}

func TestBalanceMonitorUnknownAndFailedSyncDoNotRecover(t *testing.T) {
	s, m, c, n, _ := balanceEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	for _, unit := range []string{"usd", "quota"} {
		c.catalog.Account.Unit = unit
		c.catalog.Account.Balance = nil
		_, err = s.Sync(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, "unknown", m.site.BalanceMonitorStatus.State)
		require.True(t, m.site.balanceState.Low)
	}
	c.err = ErrReauth
	_, err = s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	require.True(t, m.site.balanceState.Low)
	require.Len(t, n.sent["admin@example.com"], 1)
	for _, event := range m.events {
		require.NotEqual(t, "balance_recovered", event.Kind)
	}
}

func TestBalanceMonitorReservationFailureAndDisabledPolicyNeverSend(t *testing.T) {
	s, m, _, n, _ := balanceEngine(t)
	m.failState = true
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, n.sent)
	m.failState = false
	m.site.BalanceMonitor.Enabled = false
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, n.sent)
}

func TestConfigureBalanceMonitorValidatesAndDoesNotSend(t *testing.T) {
	s, m, _, n, _ := balanceEngine(t)
	config := m.site.BalanceMonitor
	for _, change := range []func(*BalanceMonitorConfig){
		func(c *BalanceMonitorConfig) { c.Threshold = -1 },
		func(c *BalanceMonitorConfig) { c.Unit = "quota" },
		func(c *BalanceMonitorConfig) { c.CooldownMinutes = 14 },
		func(c *BalanceMonitorConfig) { c.Recipients = []string{"Admin <admin@example.com>"} },
	} {
		bad := config
		change(&bad)
		_, err := s.ConfigureBalanceMonitor(t.Context(), 1, 1, bad)
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err := s.ConfigureBalanceMonitor(t.Context(), 1, 9, config)
	require.ErrorIs(t, err, ErrConflict)
	config.Recipients = []string{" admin@example.com ", "ADMIN@example.com"}
	site, err := s.ConfigureBalanceMonitor(t.Context(), 1, 1, config)
	require.NoError(t, err)
	require.Equal(t, int64(2), site.Version)
	require.Equal(t, []string{"admin@example.com"}, site.BalanceMonitor.Recipients)
	require.Equal(t, "unknown", site.BalanceMonitorStatus.State)
	require.Empty(t, n.sent)
}

func TestBalanceMonitorReservationSurvivesAmbiguousPostSendWrite(t *testing.T) {
	s, m, _, n, now := balanceEngine(t)
	m.failAt = 3 // observed state and SMTP reservation persist; delivery result fails.
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 1)
	require.Nil(t, m.site.BalanceMonitorStatus.LastNotifiedAt)
	*now = now.Add(15 * time.Minute)
	restarted := NewService(m, s.connector, s.local, s.cipher, true)
	restarted.now = s.now
	restarted.SetBalanceNotifier(n)
	_, err = restarted.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 1, "ambiguous delivery remains reserved for the full cooldown")
}

func TestBalanceMonitorOrdinarySiteEditPreservesPolicyAndCooldown(t *testing.T) {
	s, m, _, n, _ := balanceEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	site, err := s.UpdateSite(t.Context(), 1, Site{Version: 1, Name: "Renamed", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true, IntervalMinutes: 30})
	require.NoError(t, err)
	require.True(t, site.BalanceMonitor.Enabled)
	require.Equal(t, 10.0, site.BalanceMonitor.Threshold)
	require.NotNil(t, site.BalanceMonitorStatus.LastNotifiedAt)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, n.sent["admin@example.com"], 1)
	require.NotEmpty(t, m.site.balanceState.Recipients)
}

func TestBalanceMonitorDifferentUpstreamResetsWalletPolicy(t *testing.T) {
	s, m, _, _, _ := balanceEngine(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.NotEmpty(t, m.site.balanceState.Recipients)
	site, err := s.UpdateSite(t.Context(), 1, Site{Version: 1, Name: "Another upstream", Platform: "newapi", BaseURL: "https://another.example", Enabled: true, IntervalMinutes: 30})
	require.NoError(t, err)
	require.False(t, site.BalanceMonitor.Enabled)
	require.Equal(t, "quota", site.BalanceMonitor.Unit)
	require.Equal(t, "disabled", site.BalanceMonitorStatus.State)
	require.Nil(t, site.BalanceMonitorStatus.LastNotifiedAt)
	require.Empty(t, site.balanceState.Recipients)
	require.Nil(t, site.LastSyncAt)
}
