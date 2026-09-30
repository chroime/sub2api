package upstreamgovernance

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSQLKeyRepairPostgresReservationAndProgress(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer base.Close()
	schema := fmt.Sprintf("governance_repair_store_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ); INSERT INTO groups(id) VALUES(1)`)
	require.NoError(t, err)
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql", "258_upstream_governance_key_health.sql", "259_upstream_governance_key_repairs.sql", "260_upstream_governance_key_only_repairs.sql"} {
		raw, readErr := os.ReadFile("../../migrations/" + migration)
		require.NoError(t, readErr)
		_, err = db.Exec(string(raw))
		require.NoError(t, err, migration)
	}
	store := NewSQLStore(db)
	repairStore := store.(KeyRepairStore)
	site := &Site{Name: "Repair fixture", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true, IntervalMinutes: 15, NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(t.Context(), site))
	key := &ManagedKey{SiteID: site.ID, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: "old", Marker: "owned-marker", OwnerUserID: 5, KeyCipher: "encrypted-old"}
	require.NoError(t, store.SaveManagedKey(t.Context(), key))
	binding := &Binding{SiteID: site.ID, RemoteGroupID: "8", Platform: "openai", LocalGroupID: 1, AccountID: 10, Marker: "owned-marker", KeyCipher: "encrypted-old", ProbeIntervalMinutes: 30}
	require.NoError(t, store.SaveBinding(t.Context(), binding))
	repair := repairStoreFixture()
	repair.SiteID, repair.SiteVersion, repair.ManagedKeyID, repair.BindingID = site.ID, site.Version, key.ID, binding.ID
	repair.Marker = key.Marker
	require.NoError(t, repairStore.ReserveKeyRepair(t.Context(), &repair))
	reloaded, err := NewSQLStore(db).(KeyRepairStore).GetKeyRepair(t.Context(), site.ID, key.ID, repair.ID)
	require.NoError(t, err)
	require.Equal(t, repair.OldKeyCipher, reloaded.OldKeyCipher)
	require.Equal(t, repair.Plan, reloaded.Plan)
	second := repair
	second.ID = "operation-2"
	require.ErrorIs(t, repairStore.ReserveKeyRepair(t.Context(), &second), ErrConflict)
	previous := repair.Stage
	repair.Stage, repair.ErrorCode = KeyRepairConflict, "stale_preview"
	repair.UpdatedAt = repair.UpdatedAt.Add(time.Second)
	require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &repair, previous))
	require.NoError(t, repairStore.ReserveKeyRepair(t.Context(), &second), "a conflict before POST may be reviewed again")
	repair = second

	previous = repair.Stage
	repair.Stage = KeyRepairPostIntent
	intent := repair.CreatedAt.Add(time.Minute)
	repair.PostIntentAt, repair.UpdatedAt = &intent, intent
	require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &repair, previous))
	previous = repair.Stage
	repair.Stage = KeyRepairCandidateReady
	repair.CandidateRemoteKeyID, repair.CandidateKeyCipher = "new", "encrypted-new"
	repair.UpdatedAt = intent.Add(time.Second)
	require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &repair, previous))
	reloaded, err = NewSQLStore(db).(KeyRepairStore).GetKeyRepair(t.Context(), site.ID, key.ID, repair.ID)
	require.NoError(t, err)
	require.Equal(t, KeyRepairCandidateReady, reloaded.Stage)
	require.Equal(t, "encrypted-new", reloaded.CandidateKeyCipher)
	stale := repair
	stale.Stage = KeyRepairPostIntent
	require.ErrorIs(t, repairStore.SaveKeyRepairProgress(t.Context(), &stale, KeyRepairPrepared), ErrConflict)
	unchanged, err := store.GetManagedKey(t.Context(), site.ID, key.ID)
	require.NoError(t, err)
	require.Equal(t, "old", unchanged.RemoteKeyID)
	require.Equal(t, "encrypted-old", unchanged.KeyCipher)
	bindings, err := store.ListBindings(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, "encrypted-old", bindings[0].KeyCipher)
	otherKey := &ManagedKey{SiteID: site.ID, RemoteGroupID: "9", Platform: "openai", RemoteKeyID: "other-old", Marker: "other-marker", OwnerUserID: 5, KeyCipher: "other-encrypted-old"}
	require.NoError(t, store.SaveManagedKey(t.Context(), otherKey))
	otherBinding := &Binding{SiteID: site.ID, RemoteGroupID: "9", Platform: "openai", LocalGroupID: 1, AccountID: 11, Marker: "other-marker", KeyCipher: "other-encrypted-old", ProbeIntervalMinutes: 30}
	require.NoError(t, store.SaveBinding(t.Context(), otherBinding))
	abandoned := repairStoreFixture()
	abandoned.ID, abandoned.ManagedKeyID, abandoned.BindingID, abandoned.AccountID = "abandon-operation", otherKey.ID, otherBinding.ID, 11
	abandoned.SiteID, abandoned.SiteVersion, abandoned.Marker = site.ID, site.Version, otherKey.Marker
	abandoned.RemoteGroupID, abandoned.OldRemoteKeyID, abandoned.OldKeyCipher = "9", otherKey.RemoteKeyID, otherKey.KeyCipher
	require.NoError(t, repairStore.ReserveKeyRepair(t.Context(), &abandoned))
	previous = abandoned.Stage
	abandoned.Stage, abandoned.PostIntentAt = KeyRepairPostIntent, &intent
	require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &abandoned, previous))
	previous = abandoned.Stage
	abandoned.Stage, abandoned.ErrorCode = KeyRepairAbandoned, "manually_abandoned"
	require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &abandoned, previous))
	reopened := abandoned
	reopened.ID, reopened.Stage, reopened.ErrorCode, reopened.PostIntentAt = "reopened-operation", KeyRepairPrepared, "", nil
	require.NoError(t, repairStore.ReserveKeyRepair(t.Context(), &reopened))
	stillOld, err := store.GetManagedKey(t.Context(), site.ID, otherKey.ID)
	require.NoError(t, err)
	require.Equal(t, "other-old", stillOld.RemoteKeyID)
	require.Equal(t, "other-encrypted-old", stillOld.KeyCipher)

	for _, historicalBinding := range []bool{false, true} {
		t.Run(fmt.Sprintf("key_only_historical_binding_%t", historicalBinding), func(t *testing.T) {
			groupID, marker := "10", "never-imported-marker"
			if historicalBinding {
				groupID, marker = "11", "deleted-account-marker"
			}
			managed := &ManagedKey{SiteID: site.ID, RemoteGroupID: groupID, Platform: "openai", RemoteKeyID: "missing-" + groupID, Marker: marker, OwnerUserID: 5, KeyCipher: "old-cipher-" + groupID}
			require.NoError(t, store.SaveManagedKey(t.Context(), managed))
			keyOnly := repairStoreFixture()
			keyOnly.Mode = KeyRepairModeKeyOnly
			keyOnly.ID, keyOnly.SiteID, keyOnly.SiteVersion, keyOnly.ManagedKeyID = "key-only-"+groupID, site.ID, site.Version, managed.ID
			keyOnly.BindingID, keyOnly.AccountID, keyOnly.AccountName = 0, 0, ""
			keyOnly.ExpectedAccountFingerprint, keyOnly.ExpectedAccountIdentity = "", ""
			keyOnly.Marker, keyOnly.RemoteGroupID, keyOnly.OldRemoteKeyID, keyOnly.OldKeyCipher = marker, groupID, managed.RemoteKeyID, managed.KeyCipher
			if historicalBinding {
				historical := &Binding{SiteID: site.ID, RemoteGroupID: groupID, Platform: "openai", LocalGroupID: 1, AccountID: 23, Marker: marker, KeyCipher: managed.KeyCipher, ProbeIntervalMinutes: 30}
				require.NoError(t, store.SaveBinding(t.Context(), historical))
				keyOnly.BindingID, keyOnly.AccountID = historical.ID, historical.AccountID
			}
			require.NoError(t, repairStore.ReserveKeyRepair(t.Context(), &keyOnly))
			reloaded, err := repairStore.GetKeyRepair(t.Context(), site.ID, managed.ID, keyOnly.ID)
			require.NoError(t, err)
			require.Equal(t, KeyRepairModeKeyOnly, reloaded.Mode)
			require.Equal(t, keyOnly.BindingID, reloaded.BindingID)
			require.Equal(t, keyOnly.AccountID, reloaded.AccountID)
			keyOnly.Stage, keyOnly.PostIntentAt = KeyRepairPostIntent, &intent
			require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &keyOnly, KeyRepairPrepared))
			keyOnly.Stage, keyOnly.CandidateRemoteKeyID, keyOnly.CandidateKeyCipher = KeyRepairCandidateReady, "candidate-"+groupID, "candidate-cipher-"+groupID
			require.NoError(t, repairStore.SaveKeyRepairProgress(t.Context(), &keyOnly, KeyRepairPostIntent))
			reloaded, err = repairStore.GetKeyRepair(t.Context(), site.ID, managed.ID, keyOnly.ID)
			require.NoError(t, err)
			require.Equal(t, KeyRepairCandidateReady, reloaded.Stage)
			require.Equal(t, KeyRepairModeKeyOnly, reloaded.Mode)
			unchanged, err := store.GetManagedKey(t.Context(), site.ID, managed.ID)
			require.NoError(t, err)
			require.Equal(t, managed.KeyCipher, unchanged.KeyCipher)
		})
	}
}
