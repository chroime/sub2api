package upstreamgovernance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type drainingKeyNotifier struct{}

func (drainingKeyNotifier) Recipients(context.Context, []string) ([]string, error) {
	return []string{"admin@example.test"}, nil
}
func (drainingKeyNotifier) SendKey(ctx context.Context, _ string, _ KeyNotice) error {
	<-ctx.Done()
	return ctx.Err()
}

type twoKeyProtectionLocal struct {
	LocalAccounts
	first  *reconciliationLocalFixture
	second *reconciliationLocalFixture
}

func (l *twoKeyProtectionLocal) InspectManagedAccount(ctx context.Context, binding Binding) (*ManagedLocalAccount, error) {
	if binding.AccountID == l.first.account.ID {
		return l.first.InspectManagedAccount(ctx, binding)
	}
	return l.second.InspectManagedAccount(ctx, binding)
}

func (l *twoKeyProtectionLocal) ApplyManagedPatch(ctx context.Context, patch ManagedAccountPatch) (*ManagedLocalAccount, error) {
	if patch.Expected.ID == l.first.account.ID {
		return l.first.ApplyManagedPatch(ctx, patch)
	}
	return l.second.ApplyManagedPatch(ctx, patch)
}

func TestSlowFirstKeyMailDoesNotDelaySecondKeyPause(t *testing.T) {
	s, store, inventory, first, now := keyProtectionFixture(t, false)
	secondMarker := "owned-second"
	secondCipher, err := s.cipher.Encrypt(`{"id":"fixture-second","key":"fixture-second-key"}`)
	require.NoError(t, err)
	store.keys = append(store.keys, ManagedKey{ID: 2, SiteID: store.site.ID, RemoteGroupID: "9", Platform: "openai", Marker: secondMarker, OwnerUserID: 5, RemoteKeyID: "fixture-second", KeyCipher: secondCipher})
	store.bindings = append(store.bindings, Binding{ID: 3, SiteID: store.site.ID, AccountID: 4, Marker: secondMarker, RemoteGroupID: "9", Platform: "openai", LocalGroupID: 7, LocalGroupIDs: []int64{7}, KeyCipher: secondCipher})
	second := &reconciliationLocalFixture{account: ManagedLocalAccount{ID: 4, Identity: ManagedAccountIdentity(4, secondMarker, "openai", store.site.BaseURL, "fixture-second-key"), Name: "Second", Status: "active", Schedulable: true, CanRestore: true}}
	s.local = &twoKeyProtectionLocal{first: first, second: second}
	s.SetKeyNotifier(drainingKeyNotifier{})
	inventory.records = nil
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, _ = s.AuditKeys(ctx, store.site.ID)
	require.Equal(t, KeyHealthConfirmedMissing, store.keys[1].Health.Status)
	require.False(t, first.account.Schedulable)
	require.False(t, second.account.Schedulable, "SMTP for the first key must not prevent protection of later keys")
	require.NotNil(t, store.keys[1].Health.NextCheckAt, "an undelivered second notice must remain scheduled")
}

func TestConfirmedKeyIsPausedBeforeSlowEmailConsumesDeadline(t *testing.T) {
	s, store, inventory, local, now := keyProtectionFixture(t, false)
	s.SetKeyNotifier(drainingKeyNotifier{})
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, _ = s.AuditKeys(ctx, store.site.ID)
	require.False(t, local.account.Schedulable, "account protection cannot wait behind SMTP")
}

func TestFailedKeyPauseIsPersistedForScheduledRetry(t *testing.T) {
	s, store, inventory, local, now := keyProtectionFixture(t, false)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	local.failPatch = true
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.Error(t, err)
	require.Equal(t, KeyHealthConfirmedMissing, store.keys[0].Health.Status)
	require.True(t, local.account.Schedulable)
	require.NotNil(t, store.keys[0].Health.NextCheckAt)
	require.Equal(t, now.Add(time.Minute), *store.keys[0].Health.NextCheckAt)
	local.failPatch = false
	store.site.NextSyncAt = now.Add(15 * time.Minute)
	*now = now.Add(time.Minute)
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.False(t, local.account.Schedulable)
}

type keyProtectionStore struct{ *reconciliationMemoryStore }

type keyEventFailureStore struct{ *keyProtectionStore }

func (s *keyEventFailureStore) AddEvent(ctx context.Context, event *Event) error {
	if event.Kind == "key_missing_confirmed" {
		return ErrUnsupported
	}
	return s.keyProtectionStore.AddEvent(ctx, event)
}

func TestKeyEventWriteFailureCannotPreventConfirmedAccountPause(t *testing.T) {
	s, store, inventory, local, now := keyProtectionFixture(t, false)
	s.store = &keyEventFailureStore{keyProtectionStore: store}
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	_, _ = s.AuditKeys(t.Context(), store.site.ID)
	require.Equal(t, KeyHealthConfirmedMissing, store.keys[0].Health.Status)
	require.False(t, local.account.Schedulable)
}

func (s *keyProtectionStore) SaveKeyHealth(ctx context.Context, key ManagedKey, health KeyHealth) error {
	return (&keyHealthFixtureStore{memoryStore: s.memoryStore}).SaveKeyHealth(ctx, key, health)
}

func keyProtectionFixture(t *testing.T, automation bool) (*Service, *keyProtectionStore, *keyInventoryFixture, *reconciliationLocalFixture, *time.Time) {
	t.Helper()
	s, store, connector, local, now := reconciliationFixture(t)
	store.config.Policy.Enabled = automation
	keyCipher := store.bindings[0].KeyCipher
	store.keys = []ManagedKey{{ID: 1, SiteID: store.site.ID, RemoteGroupID: "8", Platform: "openai", Marker: store.bindings[0].Marker, OwnerUserID: 5, RemoteKeyID: "fixture", KeyCipher: keyCipher}}
	withHealth := &keyProtectionStore{reconciliationMemoryStore: store}
	inventory := &keyInventoryFixture{fakeConnector: connector, records: []RemoteKeyIdentity{{ID: "fixture", GroupID: "8"}}}
	s.store = withHealth
	s.connector = inventory
	_, err := s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, "present", store.keys[0].Health.Status)
	return s, withHealth, inventory, local, now
}

func TestConfirmedMissingKeyPausesAccountEvenWhenRateAutomationIsDisabled(t *testing.T) {
	s, store, inventory, local, now := keyProtectionFixture(t, false)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.True(t, local.account.Schedulable)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthConfirmedMissing, store.keys[0].Health.Status)
	require.False(t, local.account.Schedulable)
	require.Equal(t, "upstream_key_missing", local.account.PauseReason)
	pausedToken := local.account.PauseToken
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, pausedToken, local.account.PauseToken, "repeated audits must not pause twice")
}

func TestKeyPauseIsNotRestoredByGroupReconciliationAndOnlyRestoresWhenBothReturn(t *testing.T) {
	s, store, inventory, local, now := keyProtectionFixture(t, true)
	inventory.records = nil
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, local.account.Schedulable)
	_, err = s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, local.account.Schedulable, "a visible group cannot restore a key-missing pause")
	store.config.Policy.Enabled = false
	inventory.records = []RemoteKeyIdentity{{ID: "fixture", GroupID: "8"}}
	inventory.catalog.Groups = nil
	_, err = s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, local.account.Schedulable, "a restored key cannot restore while the group remains missing")
	inventory.catalog.Groups = []RemoteGroup{{ID: "8", Name: "Remote", Platform: "openai", ResolvedRateMultiplier: ptrRate(0.8)}}
	_, err = s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.True(t, local.account.Schedulable)
	require.Empty(t, local.account.PauseToken)
}

func ptrRate(value float64) *float64 { return &value }
