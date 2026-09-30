package repository

import (
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
	for _, name := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql", "258_upstream_governance_key_health.sql", "259_upstream_governance_key_repairs.sql"} {
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
