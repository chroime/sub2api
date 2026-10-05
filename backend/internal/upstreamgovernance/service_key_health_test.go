package upstreamgovernance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type keyHealthFixtureStore struct{ *memoryStore }

type keyHealthRuntimeStore struct{ *runtimeMemoryStore }

func (s *keyHealthRuntimeStore) SaveKeyHealth(ctx context.Context, key ManagedKey, health KeyHealth) error {
	return (&keyHealthFixtureStore{memoryStore: s.memoryStore}).SaveKeyHealth(ctx, key, health)
}

type recordingLifecycleInventory struct {
	*lifecycleConnector
	accessTokens []string
}

func (c *recordingLifecycleInventory) ListKeyInventory(_ context.Context, _ Site, session Session) ([]RemoteKeyIdentity, error) {
	c.accessTokens = append(c.accessTokens, session.AccessToken)
	return []RemoteKeyIdentity{{ID: "1", GroupID: "8"}}, nil
}

func (s *keyHealthFixtureStore) SaveKeyHealth(ctx context.Context, key ManagedKey, health KeyHealth) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := range s.keys {
		stored := &s.keys[i]
		if stored.ID == key.ID && stored.SiteID == key.SiteID && stored.RemoteKeyID == key.RemoteKeyID && stored.Marker == key.Marker && stored.OwnerUserID == key.OwnerUserID && stored.KeyCipher == key.KeyCipher {
			stored.Health = health
			return nil
		}
	}
	return ErrConflict
}

type keyInventoryFixture struct {
	*fakeConnector
	records []RemoteKeyIdentity
	err     error
	calls   int
}

func (c *keyInventoryFixture) ListKeyInventory(_ context.Context, _ Site, session Session) ([]RemoteKeyIdentity, error) {
	c.calls++
	if session.UserID != 5 {
		return nil, ErrReauth
	}
	return append([]RemoteKeyIdentity(nil), c.records...), c.err
}

func keyHealthFixture(t *testing.T) (*Service, *keyHealthFixtureStore, *keyInventoryFixture, *fakeLocal, *time.Time) {
	t.Helper()
	s, store, connector, local := setupEngine(t)
	_, err := s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	created, err := s.CreateKeys(t.Context(), store.site.ID, CreateKeysInput{SnapshotID: store.snap.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "created", created.Items[0].Status)
	observations := &keyHealthFixtureStore{memoryStore: store}
	inventory := &keyInventoryFixture{fakeConnector: connector}
	s.store = observations
	s.connector = inventory
	now := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, observations, inventory, local, &now
}

func TestKeyHealthRequiresTwoSpacedCompleteMissingInventoriesAndRecovers(t *testing.T) {
	s, store, inventory, _, now := keyHealthFixture(t)
	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, "suspected_missing", keys[0].Health.Status)
	require.Equal(t, 1, keys[0].Health.MissingCount)
	require.NotNil(t, keys[0].Health.NextCheckAt)
	require.Equal(t, now.Add(time.Minute), *keys[0].Health.NextCheckAt)

	keys, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, 1, keys[0].Health.MissingCount, "repeated reads before the follow-up deadline cannot confirm deletion")

	*now = now.Add(time.Minute)
	keys, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, "confirmed_missing", keys[0].Health.Status)
	require.Equal(t, 2, keys[0].Health.MissingCount)
	require.Equal(t, 3, inventory.calls)

	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: "8"}}
	*now = now.Add(time.Minute)
	keys, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, "present", keys[0].Health.Status)
	require.Zero(t, keys[0].Health.MissingCount)
	require.NotNil(t, keys[0].Health.LastVerifiedAt)
}

func TestKeyHealthIncompleteInventoryDoesNotConfirmOrClearMissing(t *testing.T) {
	s, store, inventory, _, now := keyHealthFixture(t)
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	inventory.err = ErrUnsupported
	*now = now.Add(time.Minute)
	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err, "key inventory failure must not rewrite a successfully collected catalog")
	require.Equal(t, "suspected_missing", keys[0].Health.Status)
	require.Equal(t, 1, keys[0].Health.MissingCount)
	require.Equal(t, "unsupported_contract", keys[0].Health.ErrorCode)
}

func TestKeyInventoryFailureSchedulesIndependentRetryFromPresentState(t *testing.T) {
	s, store, inventory, _, now := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: "8"}}
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthPresent, store.keys[0].Health.Status)
	require.Nil(t, store.keys[0].Health.NextCheckAt)

	inventory.err = ErrUnsupported
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthPresent, store.keys[0].Health.Status)
	require.Equal(t, "unsupported_contract", store.keys[0].Health.ErrorCode)
	require.NotNil(t, store.keys[0].Health.NextCheckAt)
	require.Equal(t, now.Add(5*time.Minute), *store.keys[0].Health.NextCheckAt)
	store.site.NextSyncAt = now.Add(time.Hour)
	before := inventory.calls
	*now = now.Add(5 * time.Minute)
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.Equal(t, before+1, inventory.calls)
	require.NotNil(t, store.keys[0].Health.NextCheckAt)
	require.Equal(t, now.Add(5*time.Minute), *store.keys[0].Health.NextCheckAt)
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.Equal(t, before+1, inventory.calls, "a failed retry must not hot-loop the worker")
}

func TestKeyHealthIgnoresUngroupedUnrelatedKeyInCompleteInventory(t *testing.T) {
	s, store, inventory, _, _ := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "unrelated", GroupID: ""}, {ID: "1", GroupID: "8"}}
	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthPresent, keys[0].Health.Status)
	require.Empty(t, keys[0].Health.ErrorCode)
}

func TestKeyHealthDoesNotTreatUngroupedManagedKeyAsConfirmedGroupChange(t *testing.T) {
	s, store, inventory, _, now := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: ""}}
	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthUnknown, keys[0].Health.Status)
	require.Zero(t, keys[0].Health.MissingCount)
	require.Equal(t, "upstream_key_unverifiable", keys[0].Health.ErrorCode)
	require.NotNil(t, keys[0].Health.NextCheckAt)
	require.Equal(t, now.Add(5*time.Minute), *keys[0].Health.NextCheckAt)
}

func TestUnverifiableUngroupedKeyCannotBeReused(t *testing.T) {
	require.ErrorIs(t, checkedKeyInventory(map[string]string{"1": ""}, "1", "8"), ErrUpstreamKeyUnverifiable)
}

func TestKeyMovedToAnotherRemoteGroupNeedsConfirmation(t *testing.T) {
	s, store, inventory, _, now := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: "9"}}
	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthGroupChanged, keys[0].Health.Status)
	require.Equal(t, 1, keys[0].Health.MissingCount)
	require.NotNil(t, keys[0].Health.NextCheckAt)
	*now = now.Add(time.Minute)
	keys, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthGroupChanged, keys[0].Health.Status)
	require.Equal(t, 2, keys[0].Health.MissingCount)
	require.Equal(t, "missing", keys[0].Health.NotificationKind)
}

func TestSuccessfulCollectionAuditsKeysWithoutTreatingInventoryFailureAsCatalogFailure(t *testing.T) {
	s, store, inventory, _, _ := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: "8"}}
	before := store.snap.ID
	result, err := s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Greater(t, result.ID, before)
	require.Equal(t, "present", store.keys[0].Health.Status)
	require.Equal(t, 1, inventory.calls)

	inventory.err = ErrUnsupported
	result, err = s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Greater(t, result.ID, before)
	require.Equal(t, "present", store.keys[0].Health.Status)
	require.Equal(t, "unsupported_contract", store.keys[0].Health.ErrorCode)
}

func TestManualCollectionAuditsWithPersistedRotatedSession(t *testing.T) {
	s, store, lifecycle, _ := lifecycleEngine(t)
	keyCipher, err := s.cipher.Encrypt(`{"id":"1","key":"fixture-key"}`)
	require.NoError(t, err)
	store.keys = []ManagedKey{{ID: 1, SiteID: 1, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: "1", Marker: marker(1, "8", "openai"), OwnerUserID: 5, KeyCipher: keyCipher}}
	s.store = &keyHealthRuntimeStore{runtimeMemoryStore: store}
	inventory := &recordingLifecycleInventory{lifecycleConnector: lifecycle}
	s.connector = inventory
	refreshes := 0
	lifecycle.refresh = func(_ context.Context, _ Site, old Session) (Session, error) {
		refreshes++
		if refreshes > 1 {
			return Session{}, ErrReauth
		}
		old.AccessToken = "fixture-new"
		old.RefreshToken = "fixture-rotated"
		expires := s.now().Add(time.Hour)
		old.ExpiresAt = &expires
		return old, nil
	}
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, refreshes)
	require.Equal(t, []string{"fixture-new"}, inventory.accessTokens)
	require.Equal(t, KeyHealthPresent, store.keys[0].Health.Status)
}

func TestScheduledWorkerRechecksSuspectedKeyWithoutWaitingForNextCatalog(t *testing.T) {
	s, store, inventory, _, now := keyHealthFixture(t)
	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	store.site.NextSyncAt = now.Add(15 * time.Minute)
	before := inventory.discoveryCalls
	*now = now.Add(time.Minute)
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.Equal(t, before, inventory.discoveryCalls, "confirmation must not force an early catalog collection")
	require.Equal(t, 2, inventory.calls)
	require.Equal(t, "confirmed_missing", store.keys[0].Health.Status)
}

func TestCreateKeysAndApplyDoNotReuseADeletedRemoteKey(t *testing.T) {
	s, store, inventory, local, _ := keyHealthFixture(t)
	created, err := s.CreateKeys(t.Context(), store.site.ID, CreateKeysInput{SnapshotID: store.snap.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "failed", created.Items[0].Status)
	require.Equal(t, "upstream_key_missing", created.Items[0].Error)
	require.Equal(t, 1, inventory.fakeConnector.keyCalls, "a missing saved key must not trigger another create POST")

	preview, err := s.Preview(t.Context(), store.site.ID, selections())
	require.NoError(t, err)
	applied, err := s.Apply(t.Context(), store.site.ID, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", applied.Items[0].Status)
	require.Equal(t, "upstream_key_missing", applied.Items[0].Error)
	require.Zero(t, local.calls, "a deleted upstream key must never enter a newly created local account")

	inventory.err = ErrUnsupported
	result, err := s.CreateKeys(t.Context(), store.site.ID, CreateKeysInput{SnapshotID: store.snap.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
	require.NoError(t, err)
	require.Equal(t, "upstream_key_unverifiable", result.Items[0].Error)
}

func TestUnverifiableExistingKeyBlocksNewRemoteCreatesInMixedBatch(t *testing.T) {
	s, store, inventory, local, _ := keyHealthFixture(t)
	inventory.catalog.Groups = append(inventory.catalog.Groups, RemoteGroup{ID: "9", Name: "Another", Platform: "openai"})
	snapshot, err := s.Sync(t.Context(), store.site.ID)
	require.NoError(t, err)
	inventory.err = ErrUnsupported
	request := CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}, {RemoteGroupID: "9", Platform: "openai"}}}
	result, err := s.CreateKeys(t.Context(), store.site.ID, request)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	for _, item := range result.Items {
		require.Equal(t, "failed", item.Status)
		require.Equal(t, "upstream_key_unverifiable", item.Error)
	}
	require.Equal(t, 1, inventory.keyCalls)
	require.Len(t, store.keys, 1)

	selected := append(selections(), Selection{RemoteGroupID: "9", Platform: "openai", LocalGroupID: 7, CostMultiplier: 1})
	preview, err := s.Preview(t.Context(), store.site.ID, selected)
	require.NoError(t, err)
	applied, err := s.Apply(t.Context(), store.site.ID, preview.ID)
	require.NoError(t, err)
	for _, item := range applied.Items {
		require.Equal(t, "failed", item.Status)
		require.Equal(t, "upstream_key_unverifiable", item.Error)
	}
	require.Equal(t, 1, inventory.keyCalls)
	require.Zero(t, local.calls)
}

func TestAppliedPreviewReplayChecksCurrentRemoteKeyWithoutReapplyingAccount(t *testing.T) {
	s, store, inventory, local, _ := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: "8"}}
	preview, err := s.Preview(t.Context(), store.site.ID, selections())
	require.NoError(t, err)
	result, err := s.Apply(t.Context(), store.site.ID, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, 1, local.calls)
	inventory.records = nil
	_, err = s.Apply(t.Context(), store.site.ID, preview.ID)
	require.ErrorIs(t, err, ErrUpstreamKeyMissing)
	require.Equal(t, 1, local.calls, "replay must not perform another local write")
}

func TestAppliedPreviewReplayPersistsReauthorizationNeeded(t *testing.T) {
	s, store, inventory, _, _ := keyHealthFixture(t)
	inventory.records = []RemoteKeyIdentity{{ID: "1", GroupID: "8"}}
	preview, err := s.Preview(t.Context(), store.site.ID, selections())
	require.NoError(t, err)
	result, err := s.Apply(t.Context(), store.site.ID, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	inventory.err = ErrReauth
	_, err = s.Apply(t.Context(), store.site.ID, preview.ID)
	require.ErrorIs(t, err, ErrReauth)
	require.Equal(t, "reauth_required", store.site.Status)
}
