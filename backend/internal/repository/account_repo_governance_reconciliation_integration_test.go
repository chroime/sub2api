package repository

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type reconciliationPostgresFixture struct {
	db         *sql.DB
	client     *dbent.Client
	repo       *accountRepository
	admin      service.AdminService
	engine     *gov.Service
	store      gov.Store
	management gov.ReconciliationStore
	site       *gov.Site
	binding    gov.Binding
	accountID  int64
	groups     []int64
}

func newReconciliationPostgresFixture(t *testing.T) *reconciliationPostgresFixture {
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
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("governance_reconcile_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); _ = db.Close() })
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fixture.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, fixture)))
	ctx := t.Context()
	require.NoError(t, client.Schema.Create(ctx))
	_, err = fixture.Exec(`CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT NOT NULL,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); CREATE UNIQUE INDEX fixture_reconcile_outbox ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	for _, name := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "253_upstream_governance_automation.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, e)
		_, e = fixture.Exec(string(raw))
		require.NoError(t, e)
	}
	first, err := client.Group.Create().SetName("first").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	second, err := client.Group.Create().SetName("second").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(client, fixture, nil)
	groups := newGroupRepositoryWithSQL(client, fixture)
	cfg := &config.Config{}
	cfg.Totp.EncryptionKeyConfigured = true
	admin := service.NewAdminService(cfg, nil, groups, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	engine := service.ProvideUpstreamGovernanceService(fixture, admin, nil, nil, governanceImportFixtureCipher{}, cfg, nil, nil, nil)
	engine.Stop()
	store := gov.NewSQLStore(fixture)
	site := &gov.Site{Name: "automation fixture", Platform: "sub2api", BaseURL: "https://reconcile.example", IntervalMinutes: 5, NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(ctx, site))
	rate := 1.0
	syncEnabled := true
	notes := "test-key"
	account, err := admin.CreateAccount(ctx, &service.CreateAccountInput{Name: site.BaseURL + "--1", Notes: &notes, Platform: "openai", Type: "apikey", Credentials: map[string]any{"api_key": "test-key", "base_url": site.BaseURL, "model_mapping": map[string]any{"gpt-fixture": "gpt-fixture"}}, Extra: map[string]any{"upstream_governance_marker": "owned-fixture", "quota_used": 42.0, "quota_daily_limit": 111.0, "unrelated": "keep"}, Concurrency: 5000, Priority: 9, RateMultiplier: &rate, RateSyncEnabled: &syncEnabled, GroupIDs: []int64{first.ID, second.ID}})
	require.NoError(t, err)
	binding := gov.Binding{SiteID: site.ID, AccountID: account.ID, RemoteGroupID: "8", Platform: "openai", LocalGroupID: first.ID, LocalGroupIDs: []int64{first.ID, second.ID}, Marker: "owned-fixture", KeyCipher: "cipher-must-stay", ProbeIntervalMinutes: 30, NextProbeAt: time.Now()}
	binding.KeyCipher, _ = governanceImportFixtureCipher{}.Encrypt(`{"id":"fixture","key":"test-key"}`)
	require.NoError(t, store.SaveBinding(ctx, &binding))
	f := &reconciliationPostgresFixture{db: fixture, client: client, repo: repo, admin: admin, engine: engine, store: store, management: store.(gov.ReconciliationStore), site: site, binding: binding, accountID: account.ID, groups: []int64{first.ID, second.ID}}
	f.snapshot(t, 1.1, "Remote")
	return f
}
func (f *reconciliationPostgresFixture) snapshot(t *testing.T, rate float64, name string) {
	t.Helper()
	require.NoError(t, f.store.SaveSnapshot(t.Context(), &gov.Snapshot{SiteID: f.site.ID, SiteVersion: f.site.Version, Catalog: gov.Catalog{GroupsComplete: true, Groups: []gov.RemoteGroup{{ID: "8", Name: name, Platform: "openai", ResolvedRateMultiplier: &rate}}}}, nil))
}
func (f *reconciliationPostgresFixture) account(t *testing.T) *service.Account {
	t.Helper()
	a, err := f.repo.GetByID(t.Context(), f.accountID)
	require.NoError(t, err)
	return a
}
func (f *reconciliationPostgresFixture) patch(t *testing.T, operation string) gov.ManagedAccountPatch {
	a := f.account(t)
	return gov.ManagedAccountPatch{BindingID: f.binding.ID, Marker: f.binding.Marker, OperationID: operation, Expected: service.GovernanceManagedAccount(a)}
}

func TestGovernancePostgresReconciliationPreservesAccountAndArbitratesNativeProbe(t *testing.T) {
	f := newReconciliationPostgresFixture(t)
	ctx := t.Context()
	inFlight := f.account(t)
	cfg, err := f.engine.GetAutomation(ctx, f.site.ID)
	require.NoError(t, err)
	require.False(t, cfg.Policy.Enabled)
	cfg.Policy.Enabled = true
	cfg, err = f.engine.ConfigureAutomation(ctx, f.site.ID, cfg)
	require.NoError(t, err)
	require.Equal(t, 1.0, f.account(t).BillingRateMultiplier(), "saving policy is metadata only")
	_, err = f.engine.ConfigureAutomation(ctx, f.site.ID, gov.DefaultAutomationConfig())
	require.ErrorIs(t, err, gov.ErrConflict)
	preview, err := f.engine.PreviewReconciliation(ctx, f.site.ID)
	require.NoError(t, err)
	require.Equal(t, "ready", preview.Rows[0].State)
	result, err := f.engine.ApplyReconciliation(ctx, f.site.ID, preview.ID, []int64{f.binding.ID})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	got := f.account(t)
	require.Equal(t, 1.1, got.BillingRateMultiplier())
	require.Equal(t, f.site.BaseURL+"--1.1", got.Name)
	require.Equal(t, 5000, got.Concurrency)
	require.Equal(t, 9, got.Priority)
	require.Equal(t, 42.0, got.GetQuotaUsed())
	require.Equal(t, 111.0, got.GetQuotaDailyLimit())
	require.Equal(t, "keep", got.Extra["unrelated"])
	require.Equal(t, "test-key", *got.Notes)
	require.Equal(t, inFlight.Credentials, got.Credentials)
	require.ElementsMatch(t, f.groups, got.GroupIDs)
	require.Equal(t, f.binding.Marker, got.GetExtraString(gov.GovernanceRateOwnerExtraKey))
	bindings, err := f.store.ListBindings(ctx, f.site.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, f.binding.KeyCipher, bindings[0].KeyCipher)
	states, err := f.management.ReconciliationStates(ctx, f.site.ID)
	require.NoError(t, err)
	require.Equal(t, 1.1, states[f.binding.ID].Rate)
	// A network probe started before governance took ownership must fail its CAS.
	probeRate := 9.0
	probe := &service.UpstreamBillingProbeSnapshot{Status: service.UpstreamBillingProbeStatusOK}
	require.ErrorIs(t, f.repo.UpdateUpstreamBillingProbeSnapshot(ctx, inFlight, probe, &probeRate), service.ErrUpstreamBillingProbeIdentityChanged)
	// Even a caller that proposes a rate while already owned cannot bypass SQL.
	require.NoError(t, f.repo.UpdateUpstreamBillingProbeSnapshot(ctx, got, probe, &probeRate))
	require.Equal(t, 1.1, f.account(t).BillingRateMultiplier())
	// Preserve the owner through ordinary updates that omit/forge reserved extra.
	_, err = f.admin.UpdateAccount(ctx, f.accountID, &service.UpdateAccountInput{Extra: map[string]any{"unrelated": "edited", gov.GovernanceRateOwnerExtraKey: "forged"}})
	require.NoError(t, err)
	require.Equal(t, f.binding.Marker, f.account(t).GetExtraString(gov.GovernanceRateOwnerExtraKey))
	// Clearing native sync and manually changing cost must become a conflict.
	off := false
	manual := 0.7
	_, err = f.admin.UpdateAccount(ctx, f.accountID, &service.UpdateAccountInput{RateSyncEnabled: &off, RateMultiplier: &manual})
	require.NoError(t, err)
	current, err := f.engine.Reconciliation(ctx, f.site.ID)
	require.NoError(t, err)
	require.Equal(t, "conflict", current.Rows[0].State)
	require.Equal(t, "managed_rate_changed", current.Rows[0].Reason)
}

func TestGovernancePostgresReconciliationOwnerAndTemplateCAS(t *testing.T) {
	f := newReconciliationPostgresFixture(t)
	ctx := t.Context()
	grant := f.patch(t, "grant")
	owner := f.binding.Marker
	grant.RateOwner = &owner
	_, err := f.db.Exec(`UPDATE accounts SET rate_multiplier=1.2 WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	_, err = f.repo.ReconcileGovernanceAccount(ctx, grant)
	require.ErrorIs(t, err, gov.ErrConflict, "owner-only grant must not adopt concurrent manual price")
	nameOnly := f.patch(t, "name-only")
	name := f.site.BaseURL + "--1.2"
	nameOnly.Name = &name
	_, err = f.db.Exec(`UPDATE accounts SET rate_multiplier=1.3 WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	_, err = f.repo.ReconcileGovernanceAccount(ctx, nameOnly)
	require.ErrorIs(t, err, gov.ErrConflict, "template must match its frozen account rate")
	// Rate zero remains a legitimate catalog value and bypasses no ownership CAS.
	zero := 0.0
	free := f.patch(t, "free")
	free.Rate = &zero
	free.RateOwner = &owner
	got, err := f.repo.ReconcileGovernanceAccount(ctx, free)
	require.NoError(t, err)
	require.Zero(t, got.Rate)
	// Current manual name is preserved by a patch that only releases rate ownership.
	clear := f.patch(t, "release")
	empty := ""
	clear.RateOwner = &empty
	_, err = f.db.Exec(`UPDATE accounts SET name='manual label' WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	_, err = f.repo.ReconcileGovernanceAccount(ctx, clear)
	require.NoError(t, err)
	require.Equal(t, "manual label", f.account(t).Name)
	require.Zero(t, f.account(t).BillingRateMultiplier())
}

func TestGovernancePostgresPauseOwnershipManualRevocationAndRollback(t *testing.T) {
	f := newReconciliationPostgresFixture(t)
	ctx := t.Context()
	pause := f.patch(t, "pause")
	pause.Availability = "pause"
	paused, err := f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err)
	require.False(t, paused.Schedulable)
	require.Equal(t, "pause", paused.PauseToken)
	_, err = f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err, "receipt makes same operation retry harmless")
	restore := f.patch(t, "restore")
	restore.Availability = "restore"
	require.NoError(t, f.repo.SetSchedulable(ctx, f.accountID, false)) // same false is still administrator intent
	_, err = f.repo.ReconcileGovernanceAccount(ctx, restore)
	require.ErrorIs(t, err, gov.ErrConflict)
	require.False(t, f.account(t).Schedulable)
	require.NoError(t, f.repo.SetSchedulable(ctx, f.accountID, true))
	pause = f.patch(t, "pause2")
	pause.Availability = "pause"
	_, err = f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err)
	// Explicit status updates revoke the token even if the status stays active.
	_, err = f.admin.UpdateAccount(ctx, f.accountID, &service.UpdateAccountInput{Status: service.StatusActive})
	require.NoError(t, err)
	require.Empty(t, service.GovernanceManagedAccount(f.account(t)).PauseToken)
	require.NoError(t, f.repo.SetSchedulable(ctx, f.accountID, true))
	pause = f.patch(t, "pause3")
	pause.Availability = "pause"
	_, err = f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err)
	disabled := false
	_, err = f.repo.BulkUpdate(ctx, []int64{f.accountID}, service.AccountBulkUpdate{Schedulable: &disabled})
	require.NoError(t, err)
	require.Empty(t, service.GovernanceManagedAccount(f.account(t)).PauseToken)
	require.NoError(t, f.repo.SetSchedulable(ctx, f.accountID, true))
	pause = f.patch(t, "pause4")
	pause.Availability = "pause"
	_, err = f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err)
	restore = f.patch(t, "restore4")
	restore.Availability = "restore"
	restored, err := f.repo.ReconcileGovernanceAccount(ctx, restore)
	require.NoError(t, err)
	require.True(t, restored.Schedulable)
	require.Empty(t, restored.PauseToken)
	// An outbox failure must roll back the account update and its ownership receipt.
	_, err = f.db.Exec(`ALTER TABLE scheduler_outbox ADD CONSTRAINT fixture_reject_reconcile_outbox CHECK(event_type <> 'account_changed') NOT VALID`)
	require.NoError(t, err)
	change := f.patch(t, "rollback")
	name := "must rollback"
	change.Name = &name
	before := f.account(t)
	_, err = f.repo.ReconcileGovernanceAccount(ctx, change)
	require.Error(t, err)
	after := f.account(t)
	require.Equal(t, before.Name, after.Name)
	require.Equal(t, before.Extra, after.Extra)
	require.ElementsMatch(t, f.groups, after.GroupIDs)
}

func TestGovernancePostgresGenericExtraCannotRestoreRevokedPauseOrOwner(t *testing.T) {
	f := newReconciliationPostgresFixture(t)
	ctx := t.Context()
	grant := f.patch(t, "grant")
	owner := f.binding.Marker
	grant.RateOwner = &owner
	_, err := f.repo.ReconcileGovernanceAccount(ctx, grant)
	require.NoError(t, err)
	pause := f.patch(t, "pause")
	pause.Availability = "pause"
	_, err = f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err)
	stale := f.account(t).Extra
	require.NoError(t, f.repo.SetSchedulable(ctx, f.accountID, false))
	_, err = f.repo.BulkUpdate(ctx, []int64{f.accountID}, service.AccountBulkUpdate{Extra: stale})
	require.NoError(t, err)
	require.Empty(t, service.GovernanceManagedAccount(f.account(t)).PauseToken, "stale bulk Extra must not restore manual-pause recovery ownership")
	require.NoError(t, f.repo.UpdateExtra(ctx, f.accountID, map[string]any{gov.GovernancePauseExtraKey: stale[gov.GovernancePauseExtraKey], gov.GovernanceReceiptExtraKey: stale[gov.GovernanceReceiptExtraKey], gov.GovernanceRateOwnerExtraKey: nil, "unrelated": "updated"}))
	got := f.account(t)
	require.Empty(t, service.GovernanceManagedAccount(got).PauseToken)
	require.Equal(t, owner, got.GetExtraString(gov.GovernanceRateOwnerExtraKey))
	require.Equal(t, "updated", got.Extra["unrelated"])
}

func TestGovernancePostgresFirstAdoptionValidatesSavedKeyAndOrigin(t *testing.T) {
	for _, field := range []string{"api_key", "base_url"} {
		t.Run(field, func(t *testing.T) {
			f := newReconciliationPostgresFixture(t)
			_, err := f.db.Exec(`UPDATE accounts SET credentials=jsonb_set(credentials,$2::text[],'"different"'::jsonb) WHERE id=$1`, f.accountID, "{"+field+"}")
			require.NoError(t, err)
			row, err := f.engine.Reconciliation(t.Context(), f.site.ID)
			require.NoError(t, err)
			require.Equal(t, "conflict", row.Rows[0].State)
			require.Equal(t, "account_identity_changed", row.Rows[0].Reason)
			states, err := f.management.ReconciliationStates(t.Context(), f.site.ID)
			require.NoError(t, err)
			require.Empty(t, states)
		})
	}
}

func TestGovernancePostgresPreviewPruningRetainsUnfinishedAccountReceipt(t *testing.T) {
	f := newReconciliationPostgresFixture(t)
	ctx := t.Context()
	cfg, err := f.engine.GetAutomation(ctx, f.site.ID)
	require.NoError(t, err)
	cfg.Policy.Enabled = true
	_, err = f.engine.ConfigureAutomation(ctx, f.site.ID, cfg)
	require.NoError(t, err)
	a := service.GovernanceManagedAccount(f.account(t))
	require.NoError(t, f.management.SaveReconciliationState(ctx, f.site.ID, gov.ReconciliationState{BindingID: f.binding.ID, Identity: a.Identity, Name: a.Name, Rate: a.Rate, NativeRateSync: a.NativeRateSync, RemoteName: "Remote"}))
	_, err = f.db.Exec(`ALTER TABLE upstream_governance_reconciliation_state ADD CONSTRAINT fixture_state_write_fail CHECK(false) NOT VALID`)
	require.NoError(t, err)
	preview, err := f.engine.PreviewReconciliation(ctx, f.site.ID)
	require.NoError(t, err)
	result, err := f.engine.ApplyReconciliation(ctx, f.site.ID, preview.ID, []int64{f.binding.ID})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	require.Equal(t, 1.1, f.account(t).BillingRateMultiplier())
	for i := 0; i < 105; i++ {
		_, err = f.engine.PreviewReconciliation(ctx, f.site.ID)
		require.NoError(t, err)
	}
	retained, err := f.management.GetReconciliationPreview(ctx, f.site.ID, preview.ID)
	require.NoError(t, err, "referenced receipt must survive newer preview traffic")
	require.Equal(t, preview.ID, retained.ID)
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM upstream_governance_reconcile_previews WHERE site_id=$1`, f.site.ID).Scan(&count))
	require.LessOrEqual(t, count, 101)
	_, err = f.db.Exec(`ALTER TABLE upstream_governance_reconciliation_state DROP CONSTRAINT fixture_state_write_fail`)
	require.NoError(t, err)
	result, err = f.engine.ApplyReconciliation(ctx, f.site.ID, preview.ID, []int64{f.binding.ID})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	states, err := f.management.ReconciliationStates(ctx, f.site.ID)
	require.NoError(t, err)
	require.Equal(t, 1.1, states[f.binding.ID].Rate)
}

func TestGovernancePostgresUnrelatedStaleEditCannotUndoGovernancePause(t *testing.T) {
	f := newReconciliationPostgresFixture(t)
	ctx := t.Context()
	stale := f.account(t)
	require.True(t, stale.Schedulable)
	pause := f.patch(t, "pause-before-admin-lock")
	pause.Availability = "pause"
	_, err := f.repo.ReconcileGovernanceAccount(ctx, pause)
	require.NoError(t, err)
	notes := "administrator note"
	stale.Notes = &notes
	stale.Priority = 7
	require.NoError(t, f.repo.UpdateWithAccountBillingSettings(ctx, stale, nil, nil, nil))
	got := f.account(t)
	require.False(t, got.Schedulable, "unrelated stale edit must preserve committed governance pause")
	require.Equal(t, 7, got.Priority)
	require.Equal(t, notes, *got.Notes)
	require.Equal(t, pause.OperationID, service.GovernanceManagedAccount(got).PauseToken)
	stalePaused := f.account(t)
	restore := f.patch(t, "restore-before-admin-lock")
	restore.Availability = "restore"
	_, err = f.repo.ReconcileGovernanceAccount(ctx, restore)
	require.NoError(t, err)
	stalePaused.Priority = 8
	require.NoError(t, f.repo.UpdateWithAccountBillingSettings(ctx, stalePaused, nil, nil, nil))
	got = f.account(t)
	require.True(t, got.Schedulable, "unrelated stale edit must preserve committed governance restore")
	require.Equal(t, 8, got.Priority)
	require.Empty(t, service.GovernanceManagedAccount(got).PauseToken)
}
