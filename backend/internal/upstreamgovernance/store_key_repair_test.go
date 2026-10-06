package upstreamgovernance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func repairStoreFixture() KeyRepair {
	now := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	return KeyRepair{ID: "operation-1", SiteID: 1, ManagedKeyID: 4, BindingID: 6, AccountID: 10, AccountName: "Imported", SiteVersion: 3, OwnerUserID: 5, Marker: "stable", RemoteGroupID: "8", Platform: "openai", BaseURL: "https://upstream.example", OldRemoteKeyID: "old", OldKeyCipher: "encrypted-old", ExpectedAccountFingerprint: "fingerprint", ExpectedAccountIdentity: "identity", Plan: KeyCreationPlan{Name: "repair-1", ExistingIDs: []int64{}}, IdempotencyKey: "operation-idempotency", Stage: KeyRepairPrepared, CreatedAt: now, UpdatedAt: now}
}

func TestSQLKeyRepairReservationConflictDoesNotMutateOperation(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectExec(`INSERT INTO upstream_governance_key_repairs`).WillReturnResult(sqlmock.NewResult(0, 0))
	repair := repairStoreFixture()
	err := store.(KeyRepairStore).ReserveKeyRepair(t.Context(), &repair)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, KeyRepairPrepared, repair.Stage)
}

func TestSQLKeyOnlyRepairReservesAndAdvancesWithoutAccount(t *testing.T) {
	for _, bindingID := range []int64{0, 6} {
		t.Run(map[int64]string{0: "never imported", 6: "historical binding"}[bindingID], func(t *testing.T) {
			store, mock := storeFixture(t)
			repair := repairStoreFixture()
			repair.Mode = KeyRepairModeKeyOnly
			repair.BindingID, repair.AccountID = bindingID, 0
			if bindingID != 0 {
				repair.AccountID = 10
			}
			repair.ExpectedAccountFingerprint, repair.ExpectedAccountIdentity = "", ""
			mock.ExpectExec(`INSERT INTO upstream_governance_key_repairs`).WillReturnResult(sqlmock.NewResult(0, 1))
			require.NoError(t, store.(KeyRepairStore).ReserveKeyRepair(t.Context(), &repair))
			intent := repair.CreatedAt.Add(time.Minute)
			repair.Stage, repair.PostIntentAt = KeyRepairPostIntent, &intent
			mock.ExpectExec(`UPDATE upstream_governance_key_repairs`).WillReturnResult(sqlmock.NewResult(0, 1))
			require.NoError(t, store.(KeyRepairStore).SaveKeyRepairProgress(t.Context(), &repair, KeyRepairPrepared))
		})
	}
}

func TestSQLKeyRepairProgressRequiresPreviousStage(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectExec(`UPDATE upstream_governance_key_repairs`).WillReturnResult(sqlmock.NewResult(0, 0))
	repair := repairStoreFixture()
	repair.Stage = KeyRepairPostIntent
	intent := repair.CreatedAt.Add(time.Minute)
	repair.PostIntentAt = &intent
	err := store.(KeyRepairStore).SaveKeyRepairProgress(t.Context(), &repair, KeyRepairPrepared)
	require.ErrorIs(t, err, ErrConflict)
}

func TestSQLKeyRepairCanRecordConflictBeforePostIntent(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectExec(`UPDATE upstream_governance_key_repairs`).WillReturnResult(sqlmock.NewResult(0, 1))
	repair := repairStoreFixture()
	repair.Stage, repair.ErrorCode = KeyRepairConflict, "stale_preview"
	err := store.(KeyRepairStore).SaveKeyRepairProgress(t.Context(), &repair, KeyRepairPrepared)
	require.NoError(t, err)
	require.Nil(t, repair.PostIntentAt)
}

func TestSQLKeyRepairCanAbandonPostIntentWithoutCandidate(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectExec(`UPDATE upstream_governance_key_repairs`).WillReturnResult(sqlmock.NewResult(0, 1))
	repair := repairStoreFixture()
	intent := repair.CreatedAt.Add(time.Minute)
	repair.PostIntentAt = &intent
	repair.Stage, repair.ErrorCode = KeyRepairAbandoned, "manually_abandoned"
	err := store.(KeyRepairStore).SaveKeyRepairProgress(t.Context(), &repair, KeyRepairAwaitingVisibility)
	require.NoError(t, err)
}

func TestSQLKeyRepairReadbackKeepsPrivateFieldsOutOfAPI(t *testing.T) {
	store, mock := storeFixture(t)
	repair := repairStoreFixture()
	plan, err := json.Marshal(repair.Plan)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT .* FROM upstream_governance_key_repairs`).WillReturnRows(sqlmock.NewRows([]string{"id", "site_id", "managed_key_id", "binding_id", "account_id", "account_name", "site_version", "owner_user_id", "marker", "remote_group_id", "platform", "base_url", "old_remote_key_id", "old_key_cipher", "old_creation_plan", "expected_account_fingerprint", "expected_account_identity", "plan", "idempotency_key", "candidate_remote_key_id", "candidate_key_cipher", "stage", "error_code", "post_intent_at", "created_at", "updated_at", "mode"}).AddRow(repair.ID, repair.SiteID, repair.ManagedKeyID, repair.BindingID, repair.AccountID, repair.AccountName, repair.SiteVersion, repair.OwnerUserID, repair.Marker, repair.RemoteGroupID, repair.Platform, repair.BaseURL, repair.OldRemoteKeyID, repair.OldKeyCipher, nil, repair.ExpectedAccountFingerprint, repair.ExpectedAccountIdentity, string(plan), repair.IdempotencyKey, "", "", repair.Stage, "", nil, repair.CreatedAt, repair.UpdatedAt, "account"))
	got, err := store.(KeyRepairStore).GetKeyRepair(t.Context(), repair.SiteID, repair.ManagedKeyID, repair.ID)
	require.NoError(t, err)
	require.Equal(t, repair.ID, got.ID)
	require.Equal(t, repair.Plan, got.Plan)
	require.Equal(t, repair.Plan.Name, got.PlannedKeyName)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), repair.OldKeyCipher)
	require.NotContains(t, string(raw), repair.IdempotencyKey)
	require.NotContains(t, string(raw), repair.ExpectedAccountFingerprint)
	require.Contains(t, string(raw), `"mode":"account"`)
}

func TestSQLKeyOnlyRepairReadbackPreservesAbsentBinding(t *testing.T) {
	store, mock := storeFixture(t)
	repair := repairStoreFixture()
	plan, err := json.Marshal(repair.Plan)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT .* FROM upstream_governance_key_repairs`).WillReturnRows(sqlmock.NewRows(strings.Split(keyRepairColumns, ",")).AddRow(repair.ID, repair.SiteID, repair.ManagedKeyID, nil, 0, "", repair.SiteVersion, repair.OwnerUserID, repair.Marker, repair.RemoteGroupID, repair.Platform, repair.BaseURL, repair.OldRemoteKeyID, repair.OldKeyCipher, nil, "", "", string(plan), repair.IdempotencyKey, "", "", repair.Stage, "", nil, repair.CreatedAt, repair.UpdatedAt, KeyRepairModeKeyOnly))
	got, err := store.(KeyRepairStore).GetKeyRepair(t.Context(), repair.SiteID, repair.ManagedKeyID, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairModeKeyOnly, got.Mode)
	require.Zero(t, got.BindingID)
	require.Zero(t, got.AccountID)
	require.Empty(t, got.ExpectedAccountIdentity)
}

func TestSQLKeyRepairRejectsInvalidModeTargetBeforePersistence(t *testing.T) {
	for _, mutate := range []func(*KeyRepair){
		func(r *KeyRepair) { r.Mode = "unknown" },
		func(r *KeyRepair) { r.BindingID = 0 },
		func(r *KeyRepair) { r.ExpectedAccountFingerprint = "" },
		func(r *KeyRepair) { r.Mode = KeyRepairModeKeyOnly },
		func(r *KeyRepair) {
			r.Mode, r.BindingID, r.ExpectedAccountFingerprint, r.ExpectedAccountIdentity = KeyRepairModeKeyOnly, 0, "", ""
		},
	} {
		store, _ := storeFixture(t)
		repair := repairStoreFixture()
		mutate(&repair)
		require.ErrorIs(t, store.(KeyRepairStore).ReserveKeyRepair(t.Context(), &repair), ErrInvalid)
	}
}
