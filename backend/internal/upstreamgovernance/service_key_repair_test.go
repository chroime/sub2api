package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type repairMemoryStore struct {
	*memoryStore
	repairs     map[string]KeyRepair
	failReserve bool
}

func (m *repairMemoryStore) SaveKeyHealth(ctx context.Context, key ManagedKey, health KeyHealth) error {
	return (&keyHealthFixtureStore{memoryStore: m.memoryStore}).SaveKeyHealth(ctx, key, health)
}

func (m *repairMemoryStore) ReserveKeyRepair(_ context.Context, repair *KeyRepair) error {
	if m.failReserve {
		return errors.New("fixture reservation failure")
	}
	if m.repairs == nil {
		m.repairs = map[string]KeyRepair{}
	}
	for _, existing := range m.repairs {
		if existing.ManagedKeyID == repair.ManagedKeyID && existing.Stage != KeyRepairCommitted && existing.Stage != KeyRepairAbandoned && !(existing.Stage == KeyRepairConflict && existing.PostIntentAt == nil) {
			return ErrConflict
		}
	}
	m.repairs[repair.ID] = *repair
	return nil
}

func (m *repairMemoryStore) GetKeyRepair(_ context.Context, siteID, keyID int64, repairID string) (*KeyRepair, error) {
	repair, ok := m.repairs[repairID]
	if !ok || repair.SiteID != siteID || repair.ManagedKeyID != keyID {
		return nil, ErrNotFound
	}
	return &repair, nil
}

func (m *repairMemoryStore) LatestKeyRepair(_ context.Context, siteID, keyID int64) (*KeyRepair, error) {
	for _, repair := range m.repairs {
		if repair.SiteID == siteID && repair.ManagedKeyID == keyID {
			return &repair, nil
		}
	}
	return nil, nil
}

func (m *repairMemoryStore) SaveKeyRepairProgress(_ context.Context, repair *KeyRepair, previousStage string) error {
	current, ok := m.repairs[repair.ID]
	if !ok || current.Stage != previousStage || current.OldKeyCipher != repair.OldKeyCipher || current.OwnerUserID != repair.OwnerUserID {
		return ErrConflict
	}
	m.repairs[repair.ID] = *repair
	return nil
}

type repairLocalFixture struct {
	*fakeLocal
	managed   ManagedLocalAccount
	commits   int
	store     *repairMemoryStore
	findErr   error
	patchErr  error
	patches   int
	commitErr error
}

func (l *repairLocalFixture) FindAccount(ctx context.Context, marker string) (*LocalAccount, error) {
	if l.findErr != nil {
		return nil, l.findErr
	}
	return l.fakeLocal.FindAccount(ctx, marker)
}

func (l *repairLocalFixture) AccountNames(_ context.Context, ids []int64) (map[int64]LocalAccountName, error) {
	names := map[int64]LocalAccountName{}
	for _, id := range ids {
		if id == l.managed.ID {
			names[id] = LocalAccountName{Name: l.managed.Name, Deleted: true}
		}
		for _, account := range l.accounts {
			if account.ID == id {
				names[id] = LocalAccountName{Name: account.Name}
			}
		}
	}
	return names, nil
}

func (l *repairLocalFixture) InspectManagedAccount(_ context.Context, binding Binding) (*ManagedLocalAccount, error) {
	if binding.AccountID != l.managed.ID {
		return nil, ErrConflict
	}
	a := l.managed
	return &a, nil
}

func (l *repairLocalFixture) ApplyManagedPatch(_ context.Context, patch ManagedAccountPatch) (*ManagedLocalAccount, error) {
	l.patches++
	if l.patchErr != nil {
		return nil, l.patchErr
	}
	if patch.Availability == "restore" {
		l.managed.Schedulable = true
		l.managed.PauseToken = ""
		l.managed.PauseMarker = ""
		l.managed.PauseIdentity = ""
		l.managed.PauseReason = ""
	}
	copy := l.managed
	return &copy, nil
}

func (l *repairLocalFixture) CommitKeyRepair(_ context.Context, request KeyRepairCommitRequest) error {
	if l.commitErr != nil {
		return l.commitErr
	}
	l.commits++
	repair := l.store.repairs[request.Repair.ID]
	repair.Stage = "committed"
	l.store.repairs[repair.ID] = repair
	for i := range l.store.keys {
		if l.store.keys[i].ID == repair.ManagedKeyID {
			l.store.keys[i].RemoteKeyID = request.CandidateKey.ID
			l.store.keys[i].KeyCipher = repair.CandidateKeyCipher
			verified := request.CandidateVerifiedAt
			l.store.keys[i].Health = KeyHealth{Status: KeyHealthPresent, LastCheckedAt: &verified, LastVerifiedAt: &verified, NotificationKind: "recovered", NotificationStatus: "pending"}
		}
	}
	for i := range l.store.bindings {
		if l.store.bindings[i].ID == repair.BindingID {
			l.store.bindings[i].KeyCipher = repair.CandidateKeyCipher
		}
	}
	if repair.Mode != KeyRepairModeKeyOnly {
		l.managed.Identity = ManagedAccountIdentity(repair.AccountID, repair.Marker, repair.Platform, repair.BaseURL, request.CandidateKey.Key)
		if l.managed.PauseReason == "upstream_key_missing" {
			l.managed.PauseIdentity = l.managed.Identity
		}
	}
	return nil
}

type repairConnectorFixture struct {
	*fakeConnector
	createCalls      int
	ensureCalls      int
	createErr        error
	recovered        *RemoteKey
	publishCandidate bool
	rejectEnsure     bool
	onRecover        func()
}

func (c *repairConnectorFixture) EnsureKey(_ context.Context, _ Site, _ Session, _ RemoteGroup, _ string, _ *KeyCreationPlan) (RemoteKey, error) {
	c.ensureCalls++
	if c.rejectEnsure {
		return RemoteKey{}, errors.New("repair must not call EnsureKey after post intent")
	}
	c.createCalls++
	if c.createErr != nil {
		return RemoteKey{}, c.createErr
	}
	if c.publishCandidate {
		c.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
	}
	return RemoteKey{ID: "replacement", Key: "replacement-secret"}, nil
}

func (c *repairConnectorFixture) PostPlannedKey(context.Context, Site, Session, RemoteGroup, string, KeyCreationPlan) error {
	c.createCalls++
	if c.createErr != nil {
		return c.createErr
	}
	c.recovered = &RemoteKey{ID: "replacement", Key: "replacement-secret"}
	if c.publishCandidate {
		c.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
	}
	return nil
}

func (c *repairConnectorFixture) RecoverPlannedKey(context.Context, Site, Session, RemoteGroup, KeyCreationPlan) (RemoteKey, bool, error) {
	if c.onRecover != nil {
		c.onRecover()
	}
	if c.recovered == nil {
		return RemoteKey{}, false, nil
	}
	return *c.recovered, true, nil
}

func keyRepairFixture(t *testing.T) (*Service, *repairMemoryStore, *repairConnectorFixture, *repairLocalFixture) {
	t.Helper()
	s, base, connector, local := setupEngine(t)
	connector.catalog.GroupsComplete = true
	_, err := s.Sync(t.Context(), base.site.ID)
	require.NoError(t, err)
	stable := marker(base.site.ID, "8", "openai")
	cipher, err := s.cipher.Encrypt(`{"id":"old","key":"old-secret"}`)
	require.NoError(t, err)
	firstMissing := base.snap.CreatedAt.Add(-time.Minute)
	base.keys = []ManagedKey{{ID: 4, SiteID: base.site.ID, RemoteGroupID: "8", Platform: "openai", Marker: stable, OwnerUserID: 5, RemoteKeyID: "old", KeyCipher: cipher, Health: KeyHealth{Status: KeyHealthConfirmedMissing, MissingCount: 2, FirstMissingAt: &firstMissing}}}
	base.bindings = []Binding{{ID: 6, SiteID: base.site.ID, RemoteGroupID: "8", Platform: "openai", Marker: stable, AccountID: 10, LocalGroupID: 7, LocalGroupIDs: []int64{7}, KeyCipher: cipher}}
	local.accounts[stable] = &LocalAccount{ID: 10, Name: "Imported", Fingerprint: "account-fingerprint"}
	store := &repairMemoryStore{memoryStore: base, repairs: map[string]KeyRepair{}}
	oldIdentity := ManagedAccountIdentity(10, stable, "openai", base.site.BaseURL, "old-secret")
	repairLocal := &repairLocalFixture{fakeLocal: local, store: store, managed: ManagedLocalAccount{ID: 10, Identity: oldIdentity, Name: "Imported", Status: "active", Schedulable: false, CanRestore: true, PauseToken: "pause-token", PauseMarker: stable, PauseIdentity: oldIdentity, PauseReason: "upstream_key_missing"}}
	repairConnector := &repairConnectorFixture{fakeConnector: connector, publishCandidate: true}
	s.store, s.local, s.connector = store, repairLocal, repairConnector
	s.now = func() time.Time { return base.snap.CreatedAt.Add(time.Minute) }
	return s, store, repairConnector, repairLocal
}

func TestKeyRepairPreparesKeyOnlyWhenLocalAccountAbsent(t *testing.T) {
	for _, historicalBinding := range []bool{true, false} {
		t.Run(map[bool]string{true: "deleted historical account", false: "never imported"}[historicalBinding], func(t *testing.T) {
			s, store, connector, local := keyRepairFixture(t)
			delete(local.accounts, store.keys[0].Marker)
			if !historicalBinding {
				store.bindings = nil
			}
			repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
			require.NoError(t, err, "a missing local account must allow a reviewed key-only repair")
			raw, err := json.Marshal(repair)
			require.NoError(t, err)
			var dto map[string]any
			require.NoError(t, json.Unmarshal(raw, &dto))
			require.Equal(t, "key_only", dto["mode"])
			require.Empty(t, repair.ExpectedAccountFingerprint)
			require.Empty(t, repair.ExpectedAccountIdentity)
			if historicalBinding {
				require.EqualValues(t, 6, repair.BindingID)
				require.EqualValues(t, 10, repair.AccountID)
			} else {
				require.Zero(t, repair.BindingID)
				require.Zero(t, repair.AccountID)
			}
			require.Zero(t, connector.createCalls)
			require.Zero(t, local.calls)
		})
	}
}

func TestConfirmedMissingAuditCreatesOnePreparedRepairIntent(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)

	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, KeyHealthConfirmedMissing, keys[0].Health.Status)
	require.Len(t, store.repairs, 1, "confirmed deletion should enqueue a durable repair intent")

	var first KeyRepair
	for _, candidate := range store.repairs {
		first = candidate
	}
	require.Equal(t, KeyRepairPrepared, first.Stage)
	require.Equal(t, store.keys[0].ID, first.ManagedKeyID)
	require.Equal(t, store.keys[0].RemoteKeyID, first.OldRemoteKeyID)
	require.Zero(t, connector.createCalls, "automatic repair intent must not create a remote key")

	// A repeated audit must observe the same intent instead of preparing a
	// second operation for the same missing remote key.
	_, err = s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, store.repairs, 1)
	require.Zero(t, connector.createCalls)
}

func TestConfirmedMissingTransitionUsesUpdatedHealthForRepairIntent(t *testing.T) {
	s, store, _, _ := keyRepairFixture(t)
	firstMissing := s.now().Add(-time.Minute)
	store.keys[0].Health = KeyHealth{Status: KeyHealthSuspectedMissing, MissingCount: 1, FirstMissingAt: &firstMissing, NextCheckAt: &firstMissing}

	keys, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Equal(t, KeyHealthConfirmedMissing, keys[0].Health.Status)
	require.Len(t, store.repairs, 1, "the transition from suspected to confirmed must enqueue repair intent")
}

func TestConfirmedMissingAuditUsesKeyOnlyRepairWhenLocalAccountWasDeleted(t *testing.T) {
	s, store, _, local := keyRepairFixture(t)
	delete(local.accounts, store.keys[0].Marker)

	_, err := s.AuditKeys(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.Len(t, store.repairs, 1)
	for _, repair := range store.repairs {
		require.Equal(t, KeyRepairModeKeyOnly, repair.Mode)
	}
}

func TestKeyOnlyRepairCommitsWithoutChangingOrRecreatingLocalAccount(t *testing.T) {
	for _, historicalBinding := range []bool{true, false} {
		t.Run(map[bool]string{true: "deleted historical account", false: "never imported"}[historicalBinding], func(t *testing.T) {
			s, store, connector, local := keyRepairFixture(t)
			delete(local.accounts, store.keys[0].Marker)
			if !historicalBinding {
				store.bindings = nil
			}
			originalAccount := local.managed
			repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
			require.NoError(t, err)
			repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
			require.NoError(t, err)
			require.Equal(t, KeyRepairCommitted, repair.Stage)
			require.Equal(t, KeyRepairModeKeyOnly, repair.Mode)
			require.Equal(t, "replacement", store.keys[0].RemoteKeyID)
			require.Equal(t, KeyHealthPresent, store.keys[0].Health.Status)
			if historicalBinding {
				require.Equal(t, store.keys[0].KeyCipher, store.bindings[0].KeyCipher)
			} else {
				require.Empty(t, store.bindings)
			}
			require.Empty(t, local.accounts)
			require.Equal(t, originalAccount, local.managed)
			require.Zero(t, local.calls)
			require.Zero(t, local.patches)
			repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
			require.NoError(t, err)
			require.Equal(t, KeyRepairCommitted, repair.Stage)
			require.Equal(t, 1, connector.createCalls)
			require.Equal(t, 1, local.commits)
			require.Zero(t, local.patches)
		})
	}
}

type repairReimportLocalFixture struct {
	*repairLocalFixture
}

func (l *repairReimportLocalFixture) ApplyAccount(ctx context.Context, change AccountChange) (*LocalAccount, error) {
	account, err := l.fakeLocal.ApplyAccount(ctx, change)
	if err == nil {
		account.ID = 11 // A fresh import receives a new local account identity.
	}
	return account, err
}

func TestKeyOnlyRepairAllowsFreshImportUsingReplacementKey(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	stable := store.keys[0].Marker
	delete(local.accounts, stable)
	oldAccount := local.managed
	s.local = &repairReimportLocalFixture{repairLocalFixture: local}
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Empty(t, local.accounts)

	preview, err := s.Preview(t.Context(), 1, selections())
	require.NoError(t, err)
	require.Len(t, preview.Rows, 1)
	require.Nil(t, preview.Rows[0].Existing)
	result, err := s.Apply(t.Context(), 1, preview.ID)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "applied", result.Items[0].Status)
	require.EqualValues(t, 11, result.Items[0].AccountID)
	require.EqualValues(t, 11, store.bindings[0].AccountID)
	require.Equal(t, store.keys[0].KeyCipher, store.bindings[0].KeyCipher)
	require.Equal(t, "replacement-secret", local.changes[0].APIKey)
	require.Equal(t, oldAccount, local.managed, "the deleted historical account must not be restored or changed")
	require.Zero(t, local.patches)
	require.Equal(t, 1, connector.createCalls)
	require.Zero(t, connector.ensureCalls, "normal import reuses the verified replacement key")
}

func TestKeyRepairExplicitModeMustMatchLiveAccount(t *testing.T) {
	for _, tc := range []struct {
		name, requestedMode, errorCode string
		removeAccount                  bool
	}{
		{"account repair without account", KeyRepairModeAccount, "repair_account_missing", true},
		{"key-only repair with account", KeyRepairModeKeyOnly, "repair_account_present", false},
		{"invalid repair mode", "unrecognized", "invalid_input", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, connector, local := keyRepairFixture(t)
			if tc.removeAccount {
				delete(local.accounts, store.keys[0].Marker)
			}
			_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1, Mode: tc.requestedMode})
			require.Error(t, err)
			require.Equal(t, tc.errorCode, ErrorCode(err))
			require.Empty(t, store.repairs)
			require.Zero(t, connector.createCalls)
		})
	}
}

func TestKeyOnlyRepairRejectsLiveHistoricalAccountWithChangedMarker(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	stable := store.keys[0].Marker
	local.accounts["manually-detached-marker"] = local.accounts[stable]
	delete(local.accounts, stable)
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrRepairAccountPresent)
	require.Equal(t, "repair_account_present", ErrorCode(err))
	require.Empty(t, store.repairs)
	require.Zero(t, connector.createCalls)
}

func TestKeyOnlyRepairRejectsLocalChangesBeforePost(t *testing.T) {
	for _, change := range []string{"account appears", "account appears during remote read", "binding appears", "binding account changes", "binding marker changes", "binding cipher changes", "managed owner changes", "old key changes"} {
		t.Run(change, func(t *testing.T) {
			s, store, connector, local := keyRepairFixture(t)
			stable := store.keys[0].Marker
			delete(local.accounts, stable)
			binding := store.bindings[0]
			if change == "binding appears" {
				store.bindings = nil
			}
			repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
			require.NoError(t, err)
			switch change {
			case "account appears":
				local.accounts[stable] = &LocalAccount{ID: 11, Name: "New import", Fingerprint: "new-account"}
			case "account appears during remote read":
				connector.onRecover = func() { local.accounts[stable] = &LocalAccount{ID: 11, Name: "New import", Fingerprint: "new-account"} }
			case "binding appears":
				store.bindings = []Binding{binding}
			case "binding account changes":
				store.bindings[0].AccountID = 11
			case "binding marker changes":
				store.bindings[0].Marker = "changed-marker"
			case "binding cipher changes":
				store.bindings[0].KeyCipher = "manual-cipher"
			case "managed owner changes":
				store.keys[0].OwnerUserID = 11
			case "old key changes":
				store.keys[0].RemoteKeyID = "manual-remote-key"
			}
			repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
			require.NoError(t, err)
			require.Equal(t, KeyRepairConflict, repair.Stage)
			require.Contains(t, []string{"repair_context_changed", "repair_account_present"}, repair.ErrorCode)
			require.Nil(t, repair.PostIntentAt)
			require.True(t, repair.CanReprepare)
			require.Zero(t, connector.createCalls)
			require.Zero(t, local.commits)
		})
	}
}

func TestKeyOnlyRepairDoesNotInterpretReadFailureAsAccountAbsence(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	local.findErr = errors.New("local read unavailable")
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, local.findErr)
	require.Empty(t, store.repairs)
	require.Zero(t, connector.createCalls)
}

func TestKeyOnlyRepairUncertainPostCanRecoverWithoutSecondCreate(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	delete(local.accounts, store.keys[0].Marker)
	store.bindings = nil
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairAwaitingVisibility, repair.Stage)
	require.NotNil(t, repair.PostIntentAt)
	require.True(t, repair.CanAbandon)
	_, err = s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrConflict)
	connector.recovered = &RemoteKey{ID: "replacement", Key: "replacement-secret"}
	connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
	require.Empty(t, local.accounts)
}

func TestKeyOnlyRepairPreservesCommitRaceReason(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	delete(local.accounts, store.keys[0].Marker)
	local.commitErr = ErrRepairAccountPresent
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairConflict, repair.Stage)
	require.Equal(t, "repair_account_present", repair.ErrorCode)
	require.False(t, repair.CanReprepare)
	require.NotEmpty(t, repair.CandidateKeyCipher)
	require.Equal(t, "old", store.keys[0].RemoteKeyID)
	require.Equal(t, 1, connector.createCalls)
	require.Zero(t, local.commits)
}

func TestKeyOnlyRepairRetriesBusyCommitWithoutSecondPost(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	delete(local.accounts, store.keys[0].Marker)
	local.commitErr = ErrBusy
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.ErrorIs(t, err, ErrBusy)
	require.Equal(t, KeyRepairCandidateReady, repair.Stage)
	require.Equal(t, KeyRepairCandidateReady, store.repairs[repair.ID].Stage)
	local.commitErr = nil
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestKeyRepairReservesBeforeOneCreateAndCommitsOnExplicitConfirmation(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	require.Equal(t, "prepared", repair.Stage)
	require.Zero(t, connector.createCalls)
	require.NotNil(t, repair.Plan.ExistingIDs)
	require.Equal(t, repair.Plan.Name, repair.PlannedKeyName)
	stored := store.repairs[repair.ID]
	require.Equal(t, *repair, stored)

	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, "committed", repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
	again, err := s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, "committed", again.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestKeyRepairUsesCreateOnlyAfterDurablePostIntent(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	connector.rejectEnsure = true
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Zero(t, connector.ensureCalls)
	require.Equal(t, 1, local.commits)
	require.NotNil(t, store.repairs[repair.ID].PostIntentAt)
}

func TestKeyRepairUncertainCreateNeverPostsAgainAndRecoversReadOnly(t *testing.T) {
	s, _, connector, local := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_visibility", repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_visibility", repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	connector.recovered = &RemoteKey{ID: "replacement", Key: "replacement-secret"}
	connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, "committed", repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestKeyRepairDoesNotPromoteUnlistedCandidate(t *testing.T) {
	s, _, connector, local := keyRepairFixture(t)
	connector.publishCandidate = false
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCandidateReady, repair.Stage)
	require.Equal(t, "upstream_key_unverifiable", repair.ErrorCode)
	require.Equal(t, 1, connector.createCalls)
	require.Zero(t, local.commits)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCandidateReady, repair.Stage, "a list lag must not permanently invalidate the stored candidate")
	require.Equal(t, 1, connector.createCalls)
	connector.recovered = &RemoteKey{ID: "replacement", Key: "replacement-secret"}
	connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestKeyRepairReservationFailureNeverPosts(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	store.failReserve = true
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.Error(t, err)
	require.Zero(t, connector.createCalls)
	require.Empty(t, store.repairs)
}

func TestKeyRepairRejectsSchedulableAccountBeforeRemoteEffects(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	local.managed.Schedulable = true
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, store.repairs)
	require.Zero(t, connector.createCalls)
}

func TestKeyRepairDoesNotCreateAfterOldRemoteKeyReturns(t *testing.T) {
	s, _, connector, local := keyRepairFixture(t)
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "old", GroupID: "8"}}
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairConflict, repair.Stage)
	require.Zero(t, connector.createCalls)
	require.Zero(t, local.commits)
}

func TestKeyRepairRejectsDisabledSiteBeforeRemoteEffects(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	store.site.Enabled = false
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, store.repairs)
	require.Zero(t, connector.createCalls)
}

func TestKeyRepairRejectsUnconfirmedMissingCountBeforeRemoteEffects(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	store.keys[0].Health.MissingCount = 1
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, store.repairs)
	require.Zero(t, connector.createCalls)
}

func TestKeyRepairPrePostConflictMayBeReviewedAgain(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	stable := store.keys[0].Marker
	local.accounts[stable].Fingerprint = "manually-edited"
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairConflict, repair.Stage)
	require.Nil(t, repair.PostIntentAt)
	require.True(t, repair.CanReprepare)
	require.Zero(t, connector.createCalls)
	local.accounts[stable].Fingerprint = "account-fingerprint"
	next, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	require.NotEqual(t, repair.ID, next.ID)
}

func TestKeyRepairPostIntentConflictBlocksAnotherCreate(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairAwaitingVisibility, repair.Stage)
	stable := store.keys[0].Marker
	local.accounts[stable].Fingerprint = "manually-edited"
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairConflict, repair.Stage)
	require.NotNil(t, repair.PostIntentAt)
	require.False(t, repair.CanReprepare)
	local.accounts[stable].Fingerprint = "account-fingerprint"
	_, err = s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, 1, connector.createCalls)
}

func TestKeyRepairWaitsForFreshCatalogWithoutDiscardingPendingPost(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairAwaitingVisibility, repair.Stage)
	late := store.snap.CreatedAt.Add(2 * time.Hour)
	s.now = func() time.Time { return late }
	_, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, KeyRepairAwaitingVisibility, store.repairs[repair.ID].Stage)
	require.Equal(t, 1, connector.createCalls)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	connector.recovered = &RemoteKey{ID: "replacement", Key: "replacement-secret"}
	connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestKeyRepairRetainsPendingPostOnTransientLocalReadFailure(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairAwaitingVisibility, repair.Stage)
	transient := errors.New("local database temporarily unavailable")
	local.findErr = transient
	_, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.ErrorIs(t, err, transient)
	require.Equal(t, KeyRepairAwaitingVisibility, store.repairs[repair.ID].Stage)
	require.Equal(t, 1, connector.createCalls)
}

func TestKeyRepairRequiresReauthorizationBeforeReservation(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	connector.fakeConnector.err = ErrReauth
	_, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrReauth)
	require.Empty(t, store.repairs)
	require.Zero(t, connector.createCalls)
}

func TestKeyRepairCommitAttemptsRecoveredNoticeImmediately(t *testing.T) {
	s, _, _, _ := keyRepairFixture(t)
	notifier := &keyNoticeFixture{to: []string{"admin@example.test"}}
	s.SetKeyNotifier(notifier)
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Len(t, notifier.notices, 1)
	require.Equal(t, "recovered", notifier.notices[0].Kind)
}

func TestCommittedKeyRepairReplayRetriesGuardedRestorationWithoutAnotherPost(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	local.patchErr = errors.New("fixture restoration write failure")
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.False(t, local.managed.Schedulable)
	require.Equal(t, 1, local.patches)
	require.Equal(t, "operation_failed", store.keys[0].Health.ProtectionError)
	require.NotNil(t, store.keys[0].Health.NextCheckAt)
	require.Equal(t, s.now().Add(time.Minute), *store.keys[0].Health.NextCheckAt)
	local.patchErr = nil
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.True(t, local.managed.Schedulable)
	require.Equal(t, 2, local.patches)
	require.Empty(t, store.keys[0].Health.ProtectionError)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestCommittedKeyRepairReplayRetriesRecoveredNoticeAfterCooldown(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	notifier := &keyNoticeFixture{to: []string{"admin@example.test"}, fail: true}
	s.SetKeyNotifier(notifier)
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Len(t, notifier.notices, 1)
	require.Equal(t, "failed", store.keys[0].Health.NotificationStatus)
	notifier.fail = false
	later := store.snap.CreatedAt.Add(20 * time.Minute)
	s.now = func() time.Time { return later }
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.Len(t, notifier.notices, 2)
	require.Equal(t, "sent", store.keys[0].Health.NotificationStatus)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestCommittedKeyRepairFailedRestoreRetriesOnScheduledAudit(t *testing.T) {
	s, store, connector, local := keyRepairFixture(t)
	local.patchErr = errors.New("fixture restoration write failure")
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCommitted, repair.Stage)
	require.False(t, local.managed.Schedulable)
	require.NotNil(t, store.keys[0].Health.NextCheckAt)
	store.site.NextSyncAt = s.now().Add(15 * time.Minute)
	later := *store.keys[0].Health.NextCheckAt
	s.now = func() time.Time { return later }
	local.patchErr = nil
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.True(t, local.managed.Schedulable)
	require.Empty(t, store.keys[0].Health.ProtectionError)
	require.Equal(t, 1, connector.createCalls)
	require.Equal(t, 1, local.commits)
}

func TestKeyRepairUncertainPostRequiresExplicitVerifiedAbandon(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairAwaitingVisibility, repair.Stage)
	_, err = s.AbandonKeyRepair(t.Context(), 1, 4, repair.ID, AbandonKeyRepairInput{})
	require.ErrorIs(t, err, ErrInvalid)
	require.Equal(t, KeyRepairAwaitingVisibility, store.repairs[repair.ID].Stage)
	_, err = s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.ErrorIs(t, err, ErrConflict)
	repair, err = s.AbandonKeyRepair(t.Context(), 1, 4, repair.ID, AbandonKeyRepairInput{AcknowledgeUncertainCreate: true})
	require.NoError(t, err)
	require.Equal(t, KeyRepairAbandoned, repair.Stage)
	require.True(t, repair.CanReprepare)
	require.Equal(t, 1, connector.createCalls)
	next, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	require.NotEqual(t, repair.ID, next.ID)
}

func TestKeyRepairAbandonRefusesVisibleOrUnverifiableCandidate(t *testing.T) {
	for _, mode := range []string{"visible", "list_failed", "old_returned"} {
		t.Run(mode, func(t *testing.T) {
			s, store, connector, _ := keyRepairFixture(t)
			connector.createErr = errConnectorUncertain
			repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
			require.NoError(t, err)
			repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
			require.NoError(t, err)
			switch mode {
			case "visible":
				connector.recovered = &RemoteKey{ID: "replacement", Key: "replacement-secret"}
				connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "replacement", GroupID: "8"}}
			case "list_failed":
				connector.fakeConnector.err = ErrUnsupported
			case "old_returned":
				connector.fakeConnector.remoteKeys = []RemoteKeyIdentity{{ID: "old", GroupID: "8"}}
			}
			_, err = s.AbandonKeyRepair(t.Context(), 1, 4, repair.ID, AbandonKeyRepairInput{AcknowledgeUncertainCreate: true})
			require.Error(t, err)
			require.Equal(t, KeyRepairAwaitingVisibility, store.repairs[repair.ID].Stage)
			require.Equal(t, 1, connector.createCalls)
		})
	}
}

func TestKeyRepairCanAbandonPostIntentConflictAfterSiteVersionChange(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairAwaitingVisibility, repair.Stage)
	store.site.Version++
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairConflict, repair.Stage)
	require.True(t, repair.CanAbandon)
	repair, err = s.AbandonKeyRepair(t.Context(), 1, 4, repair.ID, AbandonKeyRepairInput{AcknowledgeUncertainCreate: true})
	require.NoError(t, err)
	require.Equal(t, KeyRepairAbandoned, repair.Stage)
	require.Equal(t, 1, connector.createCalls)
}

func TestKeyRepairAbandonRejectsChangedOrigin(t *testing.T) {
	s, store, connector, _ := keyRepairFixture(t)
	connector.createErr = errConnectorUncertain
	repair, err := s.PrepareKeyRepair(t.Context(), 1, 4, PrepareKeyRepairInput{SiteVersion: 1})
	require.NoError(t, err)
	repair, err = s.ConfirmKeyRepair(t.Context(), 1, 4, repair.ID)
	require.NoError(t, err)
	store.site.BaseURL = "https://different.example"
	_, err = s.AbandonKeyRepair(t.Context(), 1, 4, repair.ID, AbandonKeyRepairInput{AcknowledgeUncertainCreate: true})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, KeyRepairAwaitingVisibility, store.repairs[repair.ID].Stage)
}
