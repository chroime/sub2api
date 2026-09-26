package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

type governanceImportFixtureCipher struct{}

func (governanceImportFixtureCipher) Encrypt(value string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(value)), nil
}
func (governanceImportFixtureCipher) Decrypt(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	return string(decoded), err
}

type governanceImportRacingRepository struct {
	service.AdminAccountRepository
	db             *sql.DB
	runtimeRefresh bool
}

func (r *governanceImportRacingRepository) UpdateWithAccountBillingSettings(ctx context.Context, account *service.Account, probe, sync *bool, rate *float64) error {
	if r.runtimeRefresh {
		// Simulate a probe/usage write after AdminService has read the account and
		// before the repository acquires the lock for the confirmed import.
		_, err := r.db.ExecContext(ctx, `UPDATE accounts SET rate_multiplier=1.25,extra=extra || '{"quota_used":42,"quota_daily_used":7,"quota_weekly_used":19,"upstream_billing_probe":{"status":"ok"}}'::jsonb WHERE id=$1`, account.ID)
		if err != nil {
			return err
		}
	}
	return r.AdminAccountRepository.UpdateWithAccountBillingSettings(ctx, account, probe, sync, rate)
}

func TestGovernancePostgresImportSettingsPreserveConcurrentUsage(t *testing.T) {
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
	defer db.Close()
	schema := fmt.Sprintf("governance_import_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer fixture.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, fixture)))
	ctx := t.Context()
	require.NoError(t, client.Schema.Create(ctx))
	_, err = fixture.Exec(`CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT NOT NULL,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT now()); CREATE UNIQUE INDEX fixture_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	for _, migration := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", migration))
		require.NoError(t, err)
		_, err = fixture.Exec(string(data))
		require.NoError(t, err)
	}
	group, err := client.Group.Create().SetName("fixture-group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	accounts := newAccountRepositoryWithSQL(client, fixture, nil)
	racing := &governanceImportRacingRepository{AdminAccountRepository: accounts, db: fixture}
	groups := newGroupRepositoryWithSQL(client, fixture)
	cfg := &config.Config{}
	cfg.Totp.EncryptionKeyConfigured = true
	admin := service.NewAdminService(cfg, nil, groups, racing, nil, nil, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cipher := governanceImportFixtureCipher{}
	engine := service.ProvideUpstreamGovernanceService(fixture, admin, nil, nil, cipher, cfg, nil, nil, nil)
	engine.Stop() // No worker or remote API call is part of this local fixture.
	store := gov.NewSQLStore(fixture)
	session, _ := cipher.Encrypt(`{"access_token":"fixture-session","user_id":5}`)
	site := &gov.Site{Name: "Fixture", Platform: "sub2api", BaseURL: "https://fixture.example", IntervalMinutes: 15, SessionCipher: session, NextSyncAt: time.Now()}
	require.NoError(t, store.CreateSite(ctx, site))
	rate := 0.8
	snapshot := &gov.Snapshot{SiteID: site.ID, SiteVersion: site.Version, Catalog: gov.Catalog{Groups: []gov.RemoteGroup{{ID: "8", Name: "Remote", Platform: "openai", ResolvedRateMultiplier: &rate, Models: []string{"gpt-fixture"}}}}}
	require.NoError(t, store.SaveSnapshot(ctx, snapshot, nil))
	selection := []gov.Selection{{RemoteGroupID: "8", Platform: "openai", LocalGroupID: group.ID, CostMultiplier: rate}}
	preview, err := engine.Preview(ctx, site.ID, selection)
	require.NoError(t, err)
	keyCipher, _ := cipher.Encrypt(`{"id":"fixture-key-id","key":"fixture-inference-key"}`)
	require.NoError(t, store.SaveManagedKey(ctx, &gov.ManagedKey{SiteID: site.ID, RemoteGroupID: "8", Platform: "openai", RemoteKeyID: "fixture-key-id", Marker: preview.Rows[0].Marker, OwnerUserID: 5, KeyCipher: keyCipher}))
	// A native create with only rate sync requested must persist both switches.
	enabled := true
	created, err := admin.CreateAccount(ctx, &service.CreateAccountInput{Name: "before", Platform: "openai", Type: "apikey", Credentials: map[string]any{"api_key": "fixture-inference-key", "base_url": site.BaseURL, "custom": "keep"}, Extra: map[string]any{"upstream_governance_marker": preview.Rows[0].Marker, "unrelated": "keep", "quota_used": 2.0}, Concurrency: 2, RateMultiplier: &rate, RateSyncEnabled: &enabled, GroupIDs: []int64{group.ID}, SkipMixedChannelCheck: true})
	require.NoError(t, err)
	persisted, err := accounts.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, true, persisted.Extra[service.UpstreamBillingProbeEnabledExtraKey])
	require.Equal(t, true, persisted.Extra[service.UpstreamBillingRateSyncEnabledExtraKey])
	preview, err = engine.Preview(ctx, site.ID, selection)
	require.NoError(t, err)
	racing.runtimeRefresh = true
	result, err := engine.Apply(ctx, site.ID, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status, "%+v", result.Items)
	persisted, err = accounts.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "https://fixture.example--0.8", persisted.Name)
	require.NotNil(t, persisted.Notes)
	require.Equal(t, "fixture-inference-key", *persisted.Notes)
	require.Equal(t, 5000, persisted.Concurrency)
	require.Equal(t, 10000.0, persisted.GetQuotaDailyLimit())
	require.Equal(t, 700000.0, persisted.GetQuotaWeeklyLimit())
	require.Equal(t, 10000000.0, persisted.GetQuotaLimit())
	require.Equal(t, 42.0, persisted.GetQuotaUsed())
	require.Equal(t, 7.0, persisted.GetQuotaDailyUsed())
	require.Equal(t, 19.0, persisted.GetQuotaWeeklyUsed())
	require.Equal(t, 1.25, persisted.BillingRateMultiplier())
	require.True(t, persisted.IsOpenAILongContextBillingEnabled())
	require.Equal(t, "keep", persisted.Extra["unrelated"])
	require.Equal(t, "keep", persisted.Credentials["custom"])
	require.Equal(t, map[string]any{"gpt-fixture": "gpt-fixture"}, persisted.Credentials["model_mapping"])
	serialized, err := json.Marshal(preview)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "fixture-inference-key")
	// Re-import with changed settings exercises the same atomic path again;
	// the upstream-owned rate is retained rather than reverting to the seed.
	selection[0].AccountConfig = preview.Rows[0].Selection.AccountConfig
	selection[0].AccountConfig.Concurrency = 6000
	selection[0].AccountConfig.QuotaDailyLimit = 0
	preview, err = engine.Preview(ctx, site.ID, selection)
	require.NoError(t, err)
	result, err = engine.Apply(ctx, site.ID, preview.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status, "%+v", result.Items)
	persisted, err = accounts.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, 6000, persisted.Concurrency)
	require.Zero(t, persisted.GetQuotaDailyLimit())
	require.Equal(t, 42.0, persisted.GetQuotaUsed())
	require.Equal(t, 1.25, persisted.BillingRateMultiplier())
}
