package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type governanceKeyRepairFixture struct {
	db      *sql.DB
	repo    *accountRepository
	store   gov.Store
	request gov.KeyRepairCommitRequest
	account *service.Account
	managed *gov.ManagedKey
	binding *gov.Binding
}

func newGovernanceKeyRepairFixture(t *testing.T) governanceKeyRepairFixture {
	t.Helper()
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set isolated loopback governance fixture DSN")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotEqual(t, "55479", u.Port(), "the live application database is not a test fixture")
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	require.Equal(t, "/postgres", u.Path)
	root, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("governance_key_repair_%d", time.Now().UnixNano())
	_, err = root.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = root.Close()
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	ctx := t.Context()
	require.NoError(t, client.Schema.Create(ctx))
	_, err = db.Exec(`CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT NOT NULL,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT now()); CREATE UNIQUE INDEX fixture_repair_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	for _, name := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql", "258_upstream_governance_key_health.sql", "259_upstream_governance_key_repairs.sql", "260_upstream_governance_key_only_repairs.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	group, err := client.Group.Create().SetName("repair-group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	store := gov.NewSQLStore(db)
	site := &gov.Site{Name: "Repair site", Platform: "newapi", BaseURL: "https://repair.example", Enabled: true, IntervalMinutes: 15, SessionCipher: "fixture-session", Status: "healthy", NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(ctx, site))
	oldKey := gov.RemoteKey{ID: "101", Key: "old-secret"}
	candidate := gov.RemoteKey{ID: "202", Key: "new-secret"}
	marker := fmt.Sprintf("site-%d-repair", site.ID)
	oldCipher := "cipher-old-secret"
	newCipher := "cipher-new-secret"
	identity := gov.ManagedAccountIdentity(1, marker, "openai", site.BaseURL, oldKey.Key)
	notes := oldKey.Key
	account := &service.Account{Name: "managed account", Notes: &notes, Platform: "openai", Type: "apikey", Status: service.StatusActive, Schedulable: false, Credentials: map[string]any{"api_key": oldKey.Key, "base_url": site.BaseURL, "custom": "keep"}, Extra: map[string]any{"upstream_governance_marker": marker, "unrelated": "keep"}, Concurrency: 1}
	repo := newAccountRepositoryWithSQL(client, db, nil)
	require.NoError(t, repo.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: group.ID, Priority: 1}}))
	identity = gov.ManagedAccountIdentity(account.ID, marker, "openai", site.BaseURL, oldKey.Key)
	pause, err := json.Marshal(map[string]any{"token": "missing-token", "marker": marker, "identity": identity, "reason": "upstream_key_missing"})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET schedulable=false,extra=jsonb_set(extra,'{upstream_governance_pause}',$2::jsonb,true) WHERE id=$1`, account.ID, string(pause))
	require.NoError(t, err)
	account, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	managed := &gov.ManagedKey{SiteID: site.ID, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: oldKey.ID, Marker: marker, OwnerUserID: 5, KeyCipher: oldCipher}
	require.NoError(t, store.SaveManagedKey(ctx, managed))
	require.NoError(t, store.(gov.KeyHealthStore).SaveKeyHealth(ctx, *managed, gov.KeyHealth{Status: gov.KeyHealthConfirmedMissing, MissingCount: 2}))
	binding := &gov.Binding{SiteID: site.ID, RemoteGroupID: "8", Platform: "openai", LocalGroupID: group.ID, AccountID: account.ID, Marker: marker, KeyCipher: oldCipher, ProbeIntervalMinutes: 30, NextProbeAt: time.Now()}
	require.NoError(t, store.SaveBinding(ctx, binding))
	repair := gov.KeyRepair{ID: "repair-1", SiteID: site.ID, ManagedKeyID: managed.ID, BindingID: binding.ID, AccountID: account.ID, AccountName: account.Name, SiteVersion: site.Version, OwnerUserID: 5, Marker: marker, RemoteGroupID: "8", Platform: "openai", BaseURL: site.BaseURL, OldRemoteKeyID: oldKey.ID, OldKeyCipher: oldCipher, ExpectedAccountFingerprint: service.GovernanceAccountFingerprint(account), ExpectedAccountIdentity: identity, Plan: gov.KeyCreationPlan{Name: "repair-unique", ExistingIDs: []int64{}}, IdempotencyKey: "repair-1", CandidateRemoteKeyID: candidate.ID, CandidateKeyCipher: newCipher, Stage: "candidate_ready"}
	plan, err := json.Marshal(repair.Plan)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_governance_key_repairs(id,site_id,managed_key_id,binding_id,account_id,account_name,site_version,owner_user_id,marker,remote_group_id,platform,base_url,old_remote_key_id,old_key_cipher,expected_account_fingerprint,expected_account_identity,plan,idempotency_key,candidate_remote_key_id,candidate_key_cipher,stage,post_intent_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb,$18,$19,$20,$21,$22)`, repair.ID, repair.SiteID, repair.ManagedKeyID, repair.BindingID, repair.AccountID, repair.AccountName, repair.SiteVersion, repair.OwnerUserID, repair.Marker, repair.RemoteGroupID, repair.Platform, repair.BaseURL, repair.OldRemoteKeyID, repair.OldKeyCipher, repair.ExpectedAccountFingerprint, repair.ExpectedAccountIdentity, string(plan), repair.IdempotencyKey, repair.CandidateRemoteKeyID, repair.CandidateKeyCipher, repair.Stage, time.Now())
	require.NoError(t, err)
	return governanceKeyRepairFixture{db: db, repo: repo, store: store, request: gov.KeyRepairCommitRequest{Repair: repair, OldKey: oldKey, CandidateKey: candidate, CandidateVerifiedAt: time.Now().UTC()}, account: account, managed: managed, binding: binding}
}

func TestCommitKeyRepairPromotesAtomicallyAndKeepsAccountPaused(t *testing.T) {
	f := newGovernanceKeyRepairFixture(t)
	ctx := t.Context()
	require.NoError(t, f.repo.CommitKeyRepair(ctx, f.request))
	account, err := f.repo.GetByID(ctx, f.account.ID)
	require.NoError(t, err)
	require.Equal(t, "new-secret", account.GetCredential("api_key"))
	require.Equal(t, "keep", account.Credentials["custom"])
	require.Equal(t, "keep", account.Extra["unrelated"])
	require.NotNil(t, account.Notes)
	require.Equal(t, "new-secret", *account.Notes)
	require.False(t, account.Schedulable)
	managedAccount := service.GovernanceManagedAccount(account)
	require.Equal(t, "missing-token", managedAccount.PauseToken)
	require.Equal(t, managedAccount.Identity, managedAccount.PauseIdentity)
	key, err := f.store.GetManagedKey(ctx, f.managed.SiteID, f.managed.ID)
	require.NoError(t, err)
	require.Equal(t, "202", key.RemoteKeyID)
	require.Equal(t, "cipher-new-secret", key.KeyCipher)
	require.Equal(t, gov.KeyHealthPresent, key.Health.Status)
	require.NotNil(t, key.Health.LastVerifiedAt)
	require.Nil(t, key.Health.NextCheckAt)
	require.Equal(t, "recovered", key.Health.NotificationKind)
	require.Equal(t, "pending", key.Health.NotificationStatus)
	bindings, err := f.store.ListBindings(ctx, f.binding.SiteID)
	require.NoError(t, err)
	require.Equal(t, "cipher-new-secret", bindings[0].KeyCipher)
	var stage string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT stage FROM upstream_governance_key_repairs WHERE id=$1`, f.request.Repair.ID).Scan(&stage))
	require.Equal(t, "committed", stage)
	var events int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM scheduler_outbox WHERE event_type=$1 AND account_id=$2 AND payload->>'repair_id'=$3`, service.SchedulerOutboxEventAccountChanged, f.account.ID, f.request.Repair.ID).Scan(&events))
	require.Equal(t, 1, events)
	require.NoError(t, f.repo.CommitKeyRepair(ctx, f.request), "committed retry must not rewrite account")
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM scheduler_outbox WHERE event_type=$1 AND account_id=$2 AND payload->>'repair_id'=$3`, service.SchedulerOutboxEventAccountChanged, f.account.ID, f.request.Repair.ID).Scan(&events))
	require.Equal(t, 1, events)
}

func TestCommitKeyRepairRejectsFrozenRecordDrift(t *testing.T) {
	tests := []struct {
		name  string
		query string
		id    func(governanceKeyRepairFixture) any
	}{
		{"site version", `UPDATE upstream_governance_sites SET version=version+1 WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.request.Repair.SiteID }},
		{"site origin", `UPDATE upstream_governance_sites SET base_url='https://changed.example' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.request.Repair.SiteID }},
		{"binding ciphertext", `UPDATE upstream_governance_bindings SET key_cipher='manual-cipher' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.binding.ID }},
		{"binding owner", `UPDATE upstream_governance_bindings SET account_id=0 WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.binding.ID }},
		{"managed owner", `UPDATE upstream_governance_keys SET owner_user_id=7 WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.managed.ID }},
		{"managed remote ID", `UPDATE upstream_governance_keys SET remote_key_id='303' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.managed.ID }},
		{"repair candidate", `UPDATE upstream_governance_key_repairs SET candidate_remote_key_id='303' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.request.Repair.ID }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newGovernanceKeyRepairFixture(t)
			ctx := t.Context()
			_, err := f.db.ExecContext(ctx, tt.query, tt.id(f))
			require.NoError(t, err)
			require.ErrorIs(t, f.repo.CommitKeyRepair(ctx, f.request), gov.ErrConflict)
			account, err := f.repo.GetByID(ctx, f.account.ID)
			require.NoError(t, err)
			require.Equal(t, "old-secret", account.GetCredential("api_key"))
		})
	}
}

func TestCommitKeyRepairRejectsAccountEditWithoutPartialWrites(t *testing.T) {
	f := newGovernanceKeyRepairFixture(t)
	ctx := t.Context()
	_, err := f.db.ExecContext(ctx, `UPDATE accounts SET name='manually renamed' WHERE id=$1`, f.account.ID)
	require.NoError(t, err)
	require.ErrorIs(t, f.repo.CommitKeyRepair(ctx, f.request), gov.ErrConflict)
	key, err := f.store.GetManagedKey(ctx, f.managed.SiteID, f.managed.ID)
	require.NoError(t, err)
	require.Equal(t, "101", key.RemoteKeyID)
	var stage string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT stage FROM upstream_governance_key_repairs WHERE id=$1`, f.request.Repair.ID).Scan(&stage))
	require.Equal(t, "candidate_ready", stage)
}

func TestCommitKeyRepairRejectsInventoryVerifiedBeforePostIntent(t *testing.T) {
	f := newGovernanceKeyRepairFixture(t)
	f.request.CandidateVerifiedAt = time.Now().Add(-time.Hour)
	require.ErrorIs(t, f.repo.CommitKeyRepair(t.Context(), f.request), gov.ErrConflict)
	account, err := f.repo.GetByID(t.Context(), f.account.ID)
	require.NoError(t, err)
	require.Equal(t, "old-secret", account.GetCredential("api_key"))
}

func TestCommitKeyRepairRollsBackWhenOutboxFails(t *testing.T) {
	f := newGovernanceKeyRepairFixture(t)
	ctx := t.Context()
	_, err := f.db.ExecContext(ctx, `DROP TABLE scheduler_outbox`)
	require.NoError(t, err)
	err = f.repo.CommitKeyRepair(ctx, f.request)
	require.Error(t, err)
	require.False(t, errors.Is(err, gov.ErrConflict))
	account, err := f.repo.GetByID(ctx, f.account.ID)
	require.NoError(t, err)
	require.Equal(t, "old-secret", account.GetCredential("api_key"))
	key, err := f.store.GetManagedKey(ctx, f.managed.SiteID, f.managed.ID)
	require.NoError(t, err)
	require.Equal(t, "101", key.RemoteKeyID)
	bindings, err := f.store.ListBindings(ctx, f.binding.SiteID)
	require.NoError(t, err)
	require.Equal(t, "cipher-old-secret", bindings[0].KeyCipher)
	var stage string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT stage FROM upstream_governance_key_repairs WHERE id=$1`, f.request.Repair.ID).Scan(&stage))
	require.Equal(t, "candidate_ready", stage)
}

func newGovernanceKeyOnlyRepairFixture(t *testing.T, state string) governanceKeyRepairFixture {
	t.Helper()
	f := newGovernanceKeyRepairFixture(t)
	ctx := t.Context()
	f.request.Repair.Mode = "key_only"
	f.request.Repair.ExpectedAccountFingerprint = ""
	f.request.Repair.ExpectedAccountIdentity = ""
	_, err := f.db.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW() WHERE id=$1`, f.account.ID)
	require.NoError(t, err)
	if state != "deleted" {
		f.request.Repair.AccountID = 0
		f.request.Repair.AccountName = ""
	}
	if state == "never_imported" {
		f.request.Repair.BindingID = 0
	}
	_, err = f.db.ExecContext(ctx, `UPDATE upstream_governance_key_repairs
SET mode='key_only',binding_id=NULLIF($2,0),account_id=$3,account_name=$4,expected_account_fingerprint='',expected_account_identity='' WHERE id=$1`,
		f.request.Repair.ID, f.request.Repair.BindingID, f.request.Repair.AccountID, f.request.Repair.AccountName)
	require.NoError(t, err)
	switch state {
	case "deleted":
	case "never_imported":
		_, err = f.db.ExecContext(ctx, `DELETE FROM upstream_governance_bindings WHERE id=$1`, f.binding.ID)
		require.NoError(t, err)
	case "pending":
		_, err = f.db.ExecContext(ctx, `UPDATE upstream_governance_bindings SET account_id=0 WHERE id=$1`, f.binding.ID)
		require.NoError(t, err)
	default:
		t.Fatalf("unknown key-only fixture state %q", state)
	}
	if state != "deleted" {
		_, err = f.db.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id=$1`, f.account.ID)
		require.NoError(t, err)
		_, err = f.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, f.account.ID)
		require.NoError(t, err)
	}
	return f
}

func governanceRepairNativeSnapshot(t *testing.T, f governanceKeyRepairFixture) []string {
	t.Helper()
	var accounts, groups, outbox string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM accounts a`).Scan(&accounts))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY account_id,group_id),'[]'::jsonb)::text FROM account_groups g`).Scan(&groups))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(o) ORDER BY id),'[]'::jsonb)::text FROM scheduler_outbox o`).Scan(&outbox))
	return []string{accounts, groups, outbox}
}

func TestCommitKeyOnlyRepairPreservesNativeAccountState(t *testing.T) {
	for _, state := range []string{"deleted", "never_imported", "pending"} {
		t.Run(state, func(t *testing.T) {
			f := newGovernanceKeyOnlyRepairFixture(t, state)
			before := governanceRepairNativeSnapshot(t, f)
			require.NoError(t, f.repo.CommitKeyRepair(t.Context(), f.request))
			require.Equal(t, before, governanceRepairNativeSnapshot(t, f), "repair must not write accounts, account groups, or scheduler outbox")
			key, err := f.store.GetManagedKey(t.Context(), f.managed.SiteID, f.managed.ID)
			require.NoError(t, err)
			require.Equal(t, "202", key.RemoteKeyID)
			require.Equal(t, "cipher-new-secret", key.KeyCipher)
			require.Equal(t, gov.KeyHealthPresent, key.Health.Status)
			bindings, err := f.store.ListBindings(t.Context(), f.binding.SiteID)
			require.NoError(t, err)
			if state == "never_imported" {
				require.Empty(t, bindings, "key-only repair must not create a binding")
			} else {
				require.Len(t, bindings, 1)
				require.Equal(t, f.request.Repair.AccountID, bindings[0].AccountID)
				require.Equal(t, "cipher-new-secret", bindings[0].KeyCipher)
			}
			var stage string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT stage FROM upstream_governance_key_repairs WHERE id=$1`, f.request.Repair.ID).Scan(&stage))
			require.Equal(t, "committed", stage)
			require.NoError(t, f.repo.CommitKeyRepair(t.Context(), f.request), "committed key-only retry must be idempotent")
			require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
		})
	}
}

func TestCommitKeyOnlyRepairDoesNotRequireSchedulerOutbox(t *testing.T) {
	f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
	_, err := f.db.ExecContext(t.Context(), `DROP TABLE scheduler_outbox`)
	require.NoError(t, err)
	require.NoError(t, f.repo.CommitKeyRepair(t.Context(), f.request))
}

func TestCommitKeyOnlyRepairRejectsLiveAccountBeforeCommit(t *testing.T) {
	for _, scenario := range []string{"restored", "restored_changed_marker", "new_account"} {
		t.Run(scenario, func(t *testing.T) {
			f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
			ctx := t.Context()
			var err error
			switch scenario {
			case "restored":
				_, err = f.db.ExecContext(ctx, `UPDATE accounts SET deleted_at=NULL WHERE id=$1`, f.account.ID)
			case "restored_changed_marker":
				_, err = f.db.ExecContext(ctx, `UPDATE accounts SET deleted_at=NULL,extra='{}'::jsonb WHERE id=$1`, f.account.ID)
			case "new_account":
				_, err = f.db.ExecContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,created_at,updated_at) VALUES ('new account','openai','apikey','{}'::jsonb,jsonb_build_object('upstream_governance_marker',$1::text),NOW(),NOW())`, f.request.Repair.Marker)
			}
			require.NoError(t, err)
			before := governanceRepairNativeSnapshot(t, f)
			require.ErrorIs(t, f.repo.CommitKeyRepair(ctx, f.request), gov.ErrConflict)
			require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
			key, err := f.store.GetManagedKey(ctx, f.managed.SiteID, f.managed.ID)
			require.NoError(t, err)
			require.Equal(t, "101", key.RemoteKeyID)
		})
	}
}

func TestCommitKeyOnlyRepairRejectsFrozenBindingDrift(t *testing.T) {
	for _, scenario := range []string{"ciphertext", "account", "marker", "new_binding"} {
		t.Run(scenario, func(t *testing.T) {
			state := "deleted"
			if scenario == "new_binding" {
				state = "never_imported"
			}
			f := newGovernanceKeyOnlyRepairFixture(t, state)
			ctx := t.Context()
			var err error
			switch scenario {
			case "ciphertext":
				_, err = f.db.ExecContext(ctx, `UPDATE upstream_governance_bindings SET key_cipher='manual-cipher' WHERE id=$1`, f.binding.ID)
			case "account":
				_, err = f.db.ExecContext(ctx, `UPDATE upstream_governance_bindings SET account_id=0 WHERE id=$1`, f.binding.ID)
			case "marker":
				_, err = f.db.ExecContext(ctx, `UPDATE upstream_governance_bindings SET marker='changed-marker' WHERE id=$1`, f.binding.ID)
			case "new_binding":
				_, err = f.db.ExecContext(ctx, `INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,marker,key_cipher) VALUES ($1,$2,$3,$4,$5,$6)`, f.binding.SiteID, f.binding.RemoteGroupID, f.binding.Platform, f.binding.LocalGroupID, f.binding.Marker, f.binding.KeyCipher)
			}
			require.NoError(t, err)
			before := governanceRepairNativeSnapshot(t, f)
			require.ErrorIs(t, f.repo.CommitKeyRepair(ctx, f.request), gov.ErrConflict)
			require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
			key, err := f.store.GetManagedKey(ctx, f.managed.SiteID, f.managed.ID)
			require.NoError(t, err)
			require.Equal(t, "101", key.RemoteKeyID)
		})
	}
}

func TestCommitKeyOnlyRepairRejectsFrozenContextDrift(t *testing.T) {
	tests := []struct {
		name  string
		query string
		id    func(governanceKeyRepairFixture) any
	}{
		{"site version", `UPDATE upstream_governance_sites SET version=version+1 WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.request.Repair.SiteID }},
		{"site disabled", `UPDATE upstream_governance_sites SET enabled=false WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.request.Repair.SiteID }},
		{"managed owner", `UPDATE upstream_governance_keys SET owner_user_id=7 WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.managed.ID }},
		{"managed remote ID", `UPDATE upstream_governance_keys SET remote_key_id='303' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.managed.ID }},
		{"managed ciphertext", `UPDATE upstream_governance_keys SET key_cipher='manual-cipher' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.managed.ID }},
		{"repair candidate", `UPDATE upstream_governance_key_repairs SET candidate_remote_key_id='303' WHERE id=$1`, func(f governanceKeyRepairFixture) any { return f.request.Repair.ID }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
			_, err := f.db.ExecContext(t.Context(), tt.query, tt.id(f))
			require.NoError(t, err)
			before := governanceRepairNativeSnapshot(t, f)
			require.ErrorIs(t, f.repo.CommitKeyRepair(t.Context(), f.request), gov.ErrRepairContextChanged)
			require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
			bindings, err := f.store.ListBindings(t.Context(), f.binding.SiteID)
			require.NoError(t, err)
			require.Equal(t, "cipher-old-secret", bindings[0].KeyCipher)
		})
	}
}

func TestCommitKeyOnlyRepairRejectsSwitchingPersistedAccountMode(t *testing.T) {
	f := newGovernanceKeyRepairFixture(t)
	f.request.Repair.Mode = "key_only"
	f.request.Repair.ExpectedAccountFingerprint = ""
	f.request.Repair.ExpectedAccountIdentity = ""
	require.ErrorIs(t, f.repo.CommitKeyRepair(t.Context(), f.request), gov.ErrRepairContextChanged)
}

func TestCommitKeyOnlyRepairRollsBackBindingWhenManagedKeyWriteFails(t *testing.T) {
	f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
	ctx := t.Context()
	_, err := f.db.ExecContext(ctx, `ALTER TABLE upstream_governance_keys ADD CONSTRAINT fixture_reject_candidate CHECK (remote_key_id<>'202')`)
	require.NoError(t, err)
	before := governanceRepairNativeSnapshot(t, f)
	require.Error(t, f.repo.CommitKeyRepair(ctx, f.request))
	require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
	key, err := f.store.GetManagedKey(ctx, f.managed.SiteID, f.managed.ID)
	require.NoError(t, err)
	require.Equal(t, "101", key.RemoteKeyID)
	require.Equal(t, "cipher-old-secret", key.KeyCipher)
	bindings, err := f.store.ListBindings(ctx, f.binding.SiteID)
	require.NoError(t, err)
	require.Equal(t, "cipher-old-secret", bindings[0].KeyCipher)
	var stage string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT stage FROM upstream_governance_key_repairs WHERE id=$1`, f.request.Repair.ID).Scan(&stage))
	require.Equal(t, "candidate_ready", stage)
}

func TestCommitKeyOnlyRepairRejectsConcurrentAccountAppearance(t *testing.T) {
	for _, scenario := range []string{"restore", "insert"} {
		t.Run(scenario, func(t *testing.T) {
			f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
			ctx := t.Context()
			held, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer held.Rollback()
			if scenario == "restore" {
				_, err = held.ExecContext(ctx, `UPDATE accounts SET deleted_at=NULL WHERE id=$1`, f.account.ID)
			} else {
				_, err = held.ExecContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,created_at,updated_at) VALUES ('new account','openai','apikey','{}'::jsonb,jsonb_build_object('upstream_governance_marker',$1::text),NOW(),NOW())`, f.request.Repair.Marker)
			}
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() { result <- f.repo.CommitKeyRepair(ctx, f.request) }()
			require.Eventually(t, func() bool {
				var waiting bool
				err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='accounts'::regclass AND mode='ShareLock' AND NOT granted)`).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond, "key-only commit must wait for in-flight native account writers")
			require.NoError(t, held.Commit())
			before := governanceRepairNativeSnapshot(t, f)
			select {
			case err := <-result:
				require.ErrorIs(t, err, gov.ErrConflict)
			case <-time.After(5 * time.Second):
				t.Fatal("key-only commit did not finish after account writer committed")
			}
			require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
		})
	}
}

func TestCommitKeyOnlyRepairRejectsConcurrentBindingAppearance(t *testing.T) {
	f := newGovernanceKeyOnlyRepairFixture(t, "never_imported")
	ctx := t.Context()
	held, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer held.Rollback()
	_, err = held.ExecContext(ctx, `INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,marker,key_cipher) VALUES ($1,$2,$3,$4,$5,$6)`, f.binding.SiteID, f.binding.RemoteGroupID, f.binding.Platform, f.binding.LocalGroupID, f.binding.Marker, f.binding.KeyCipher)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- f.repo.CommitKeyRepair(ctx, f.request) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='upstream_governance_bindings'::regclass AND mode='ShareRowExclusiveLock' AND NOT granted)`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "key-only commit must wait for in-flight binding writers")
	require.NoError(t, held.Commit())
	select {
	case err := <-result:
		require.ErrorIs(t, err, gov.ErrConflict)
	case <-time.After(5 * time.Second):
		t.Fatal("key-only commit did not finish after binding writer committed")
	}
}

func TestCommittedKeyOnlyRepairRejectsRestoredAccount(t *testing.T) {
	f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
	ctx := t.Context()
	require.NoError(t, f.repo.CommitKeyRepair(ctx, f.request))
	_, err := f.db.ExecContext(ctx, `UPDATE accounts SET deleted_at=NULL WHERE id=$1`, f.account.ID)
	require.NoError(t, err)
	before := governanceRepairNativeSnapshot(t, f)
	require.ErrorIs(t, f.repo.CommitKeyRepair(ctx, f.request), gov.ErrConflict)
	require.Equal(t, before, governanceRepairNativeSnapshot(t, f))
}

func TestAccountAndKeyOnlyRepairsSerializeWithoutDeadlock(t *testing.T) {
	f := newGovernanceKeyRepairFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	keyOnly := f.request
	p := &keyOnly.Repair
	p.ID = "repair-2"
	p.Mode = "key_only"
	p.BindingID, p.AccountID, p.AccountName = 0, 0, ""
	p.ExpectedAccountFingerprint, p.ExpectedAccountIdentity = "", ""
	p.RemoteGroupID, p.Marker = "9", "key-only-marker"
	p.IdempotencyKey = p.ID
	p.Plan.Name = "repair-unique-other"
	p.CandidateRemoteKeyID, keyOnly.CandidateKey.ID = "303", "303"
	key := &gov.ManagedKey{SiteID: p.SiteID, RemoteGroupID: p.RemoteGroupID, Platform: p.Platform, RemoteKeyID: p.OldRemoteKeyID, Marker: p.Marker, OwnerUserID: p.OwnerUserID, KeyCipher: p.OldKeyCipher}
	require.NoError(t, f.store.SaveManagedKey(ctx, key))
	require.NoError(t, f.store.(gov.KeyHealthStore).SaveKeyHealth(ctx, *key, gov.KeyHealth{Status: gov.KeyHealthConfirmedMissing, MissingCount: 2}))
	p.ManagedKeyID = key.ID
	p.Stage = gov.KeyRepairPrepared
	candidateID, candidateCipher := p.CandidateRemoteKeyID, p.CandidateKeyCipher
	p.CandidateRemoteKeyID, p.CandidateKeyCipher = "", ""
	require.NoError(t, f.store.(gov.KeyRepairStore).ReserveKeyRepair(ctx, p))
	p.Stage = gov.KeyRepairCandidateReady
	p.CandidateRemoteKeyID, p.CandidateKeyCipher = candidateID, candidateCipher
	_, err := f.db.ExecContext(ctx, `UPDATE upstream_governance_key_repairs SET stage='candidate_ready',candidate_remote_key_id=$2,candidate_key_cipher=$3,post_intent_at=NOW() WHERE id=$1`, p.ID, p.CandidateRemoteKeyID, p.CandidateKeyCipher)
	require.NoError(t, err)
	keyOnly.CandidateVerifiedAt = time.Now().UTC()
	held, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer held.Rollback()
	var lockedID int64
	require.NoError(t, held.QueryRowContext(ctx, `SELECT id FROM upstream_governance_keys WHERE id=$1 FOR UPDATE`, f.managed.ID).Scan(&lockedID))
	accountResult := make(chan error, 1)
	go func() { accountResult <- f.repo.CommitKeyRepair(ctx, f.request) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
WHERE l.relation='accounts'::regclass AND l.granted AND a.wait_event_type='Lock' AND a.query LIKE 'SELECT key_health%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "account repair must reach its managed-key lock while holding the site lock")
	keyOnlyResult := make(chan error, 1)
	go func() { keyOnlyResult <- f.repo.CommitKeyRepair(ctx, keyOnly) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
WHERE l.relation='accounts'::regclass AND l.mode='ShareLock' AND
 (NOT l.granted OR (a.wait_event_type='Lock' AND a.query LIKE 'SELECT version,base_url,enabled%')))`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "both repairs must overlap before releasing the account repair")
	require.NoError(t, held.Commit())
	for _, result := range []chan error{accountResult, keyOnlyResult} {
		select {
		case err := <-result:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("concurrent account and key-only repairs did not finish")
		}
	}
}

func TestCommitKeyOnlyRepairSiteContentionRemainsRetryable(t *testing.T) {
	f := newGovernanceKeyOnlyRepairFixture(t, "deleted")
	held, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer held.Rollback()
	var lockedID int64
	require.NoError(t, held.QueryRowContext(t.Context(), `SELECT id FROM upstream_governance_sites WHERE id=$1 FOR UPDATE`, f.request.Repair.SiteID).Scan(&lockedID))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.ErrorIs(t, f.repo.CommitKeyRepair(ctx, f.request), gov.ErrBusy)
	var stage string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT stage FROM upstream_governance_key_repairs WHERE id=$1`, f.request.Repair.ID).Scan(&stage))
	require.Equal(t, "candidate_ready", stage, "site contention must not strand a verified candidate")
	require.NoError(t, held.Rollback())
	require.NoError(t, f.repo.CommitKeyRepair(t.Context(), f.request))
}
