//go:build integration

package accountquality

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// An explicitly selected local installation supplies connection settings only.
// Every fixture connection has a dedicated search_path without public, so all
// writes, migrations and cleanup are confined to the uniquely named schema.
func newQualityPostgresFixture(t *testing.T) *qualityPostgresFixture {
	t.Helper()
	path := os.Getenv("ACCOUNT_QUALITY_TEST_CONFIG")
	if path == "" {
		t.Skip("set ACCOUNT_QUALITY_TEST_CONFIG to enable isolated PostgreSQL persistence fixtures")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg struct {
		Database struct {
			Host     string `yaml:"host"`
			Port     int    `yaml:"port"`
			User     string `yaml:"user"`
			Password string `yaml:"password"`
			DBName   string `yaml:"dbname"`
			SSLMode  string `yaml:"sslmode"`
		} `yaml:"database"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &cfg))
	require.NotEmpty(t, cfg.Database.Host)
	require.NotEmpty(t, cfg.Database.User)
	require.NotEmpty(t, cfg.Database.DBName)
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "prefer"
	}
	u := &url.URL{Scheme: "postgres", Host: cfg.Database.Host + ":" + strconv.Itoa(cfg.Database.Port), Path: cfg.Database.DBName, User: url.UserPassword(cfg.Database.User, cfg.Database.Password)}
	q := u.Query()
	q.Set("sslmode", cfg.Database.SSLMode)
	q.Set("connect_timeout", "5")
	q.Set("application_name", "account_quality_isolated_fixture")
	u.RawQuery = q.Encode()
	admin, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	admin.SetMaxOpenConns(1)
	schema := "aq_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	if err != nil {
		_ = admin.Close()
		t.Fatal("create isolated quality schema:", err)
	}
	var db *sql.DB
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, cleanupErr := admin.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		if cleanupErr != nil {
			t.Errorf("cleanup isolated quality schema %s: %v", schema, cleanupErr)
		}
		_ = admin.Close()
	})
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err = sql.Open("postgres", u.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	var current, searchPath string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT current_schema(), current_setting('search_path')").Scan(&current, &searchPath))
	require.Equal(t, schema, current)
	require.Equal(t, schema, searchPath)
	_, err = db.ExecContext(t.Context(), `
		CREATE TABLE accounts (
		 id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, platform TEXT NOT NULL DEFAULT 'openai',
		 type TEXT NOT NULL DEFAULT 'apikey', status TEXT NOT NULL DEFAULT 'active',
		 credentials JSONB NOT NULL DEFAULT '{}'::jsonb, extra JSONB NOT NULL DEFAULT '{}'::jsonb,
		 proxy_id BIGINT, parent_account_id BIGINT, schedulable BOOLEAN NOT NULL DEFAULT TRUE,
		 temp_unschedulable_until TIMESTAMPTZ, temp_unschedulable_reason TEXT NOT NULL DEFAULT '',
		 deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE proxies (
		 id BIGSERIAL PRIMARY KEY, protocol TEXT NOT NULL DEFAULT 'http', host TEXT NOT NULL,
		 port INTEGER NOT NULL DEFAULT 8080, username TEXT NOT NULL DEFAULT '', password TEXT NOT NULL DEFAULT '',
		 status TEXT NOT NULL DEFAULT 'active', expires_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ
		);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE scheduler_outbox (
		 id BIGSERIAL PRIMARY KEY, event_type TEXT NOT NULL, account_id BIGINT,
		 group_id BIGINT, payload JSONB, dedup_key TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "274_account_quality_monitoring.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(migration))
	require.NoError(t, err)
	f := &qualityPostgresFixture{db: db, store: NewStore(db)}
	f.store.Capabilities = []Capability{{Platform: "openai", APIMode: "responses", AccountTypes: []string{"apikey", "oauth"}, AutomaticControl: true, OutputLimitMode: "upstream"}}
	f.store.ResolveModel = func(_ context.Context, _ int64, model string) (string, error) {
		if model == "fixture-model" {
			return "fixture-upstream", nil
		}
		return model, nil
	}
	f.store.Refresh = func(_ context.Context, id int64) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.refreshed = append(f.refreshed, id)
	}
	return f
}

type qualityPostgresFixture struct {
	db        *sql.DB
	store     *Store
	mu        sync.Mutex
	refreshed []int64
}

func (f *qualityPostgresFixture) account(t *testing.T) int64 {
	t.Helper()
	var id int64
	err := f.db.QueryRowContext(t.Context(), `INSERT INTO accounts(name,credentials,extra) VALUES('quality fixture','{"api_key":"fixture-key","base_url":"https://fixture.invalid","model_mapping":{"fixture-model":"fixture-upstream"}}','{"unrelated":"preserve"}') RETURNING id`).Scan(&id)
	require.NoError(t, err)
	return id
}

func (f *qualityPostgresFixture) policy(t *testing.T, edit func(*Policy)) Policy {
	t.Helper()
	p := DefaultPolicy()
	p.Name, p.Platform, p.Model, p.Enabled, p.ControlMode = "fixture policy", "openai", "fixture-model", true, "auto_quarantine"
	if edit != nil {
		edit(&p)
	}
	p, err := f.store.SavePolicy(t.Context(), p, 42)
	require.NoError(t, err)
	return p
}

func (f *qualityPostgresFixture) bind(t *testing.T, account int64, p Policy) Binding {
	t.Helper()
	bindings, err := f.store.Bind(t.Context(), BindInput{PolicyID: p.ID, AccountIDs: []int64{account}, AutoIsolate: true, Source: "integration_fixture"}, 42)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	return bindings[0]
}

func (f *qualityPostgresFixture) binding(t *testing.T, id int64) Binding {
	t.Helper()
	bindings, err := f.store.Bindings(t.Context(), 0)
	require.NoError(t, err)
	for _, b := range bindings {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("missing fixture binding %d", id)
	return Binding{}
}

func fixtureEstimate(Policy, string) *float64 { value := 0.001; return &value }

func (f *qualityPostgresFixture) claim(t *testing.T, b Binding) Run {
	t.Helper()
	b = f.binding(t, b.ID)
	r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err)
	require.Equal(t, "queued", r.ExecutionStatus)
	claimed, err := f.store.Claim(t.Context())
	require.NoError(t, err)
	require.Equal(t, r.ID, claimed.ID)
	require.NotEmpty(t, claimed.Lease)
	return claimed
}

func fixtureResult(r Run, verdict string) Run {
	r.ExecutionStatus, r.QualityVerdict, r.AutoControlEligible = "completed", verdict, true
	r.RequestedModel, r.MappedModel, r.ReturnedModel = "fixture-model", "fixture-upstream", "fixture-upstream"
	r.CredentialAccountID, r.FinishReason, r.DurationMS = r.AccountID, "stop", 12
	r.ResponseText, r.Reason = "synthetic fixture evidence", "fixture verdict "+verdict
	r.InputTokens, r.OutputTokens = 11, 7
	value := 0.001
	r.CostUSD = &value
	return r
}

func (f *qualityPostgresFixture) finish(t *testing.T, b Binding, verdict string) Run {
	t.Helper()
	r := fixtureResult(f.claim(t, b), verdict)
	require.NoError(t, f.store.Complete(t.Context(), r))
	return r
}

func (f *qualityPostgresFixture) gates(t *testing.T, account int64) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT COALESCE(extra->'account_quality_gates','{}'::jsonb) FROM accounts WHERE id=$1", account).Scan(&raw))
	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func (f *qualityPostgresFixture) outboxes(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM scheduler_outbox").Scan(&count))
	return count
}

func TestQualityPostgresIsolationRecoveryAndEvidence(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, nil))
	f.finish(t, b, "fail")
	got := f.binding(t, b.ID)
	require.Equal(t, 1, got.Failures)
	require.False(t, got.Isolated)
	r := f.claim(t, got)
	r.ExecutionStatus, r.QualityVerdict = "timeout", "not_evaluated"
	require.NoError(t, f.store.Complete(t.Context(), r))
	require.Equal(t, 1, f.binding(t, b.ID).Failures)
	f.finish(t, f.binding(t, b.ID), "fail")
	got = f.binding(t, b.ID)
	require.True(t, got.Isolated)
	require.Equal(t, "quarantined", got.State)
	require.Contains(t, f.gates(t, account), strconv.FormatInt(b.ID, 10))
	require.True(t, got.NextRunAt.After(time.Now().Add(14*time.Minute)), "cooldown is for retesting, not automatic gate expiry")
	f.finish(t, got, "pass")
	got = f.binding(t, b.ID)
	require.True(t, got.Isolated)
	require.Equal(t, 1, got.RecoveryPasses)
	f.finish(t, got, "pass")
	got = f.binding(t, b.ID)
	require.False(t, got.Isolated)
	require.Equal(t, "healthy", got.State)
	require.Empty(t, f.gates(t, account))
	runs, err := f.store.Runs(t.Context(), account, b.ID, 20)
	require.NoError(t, err)
	require.Len(t, runs, 5)
	require.Equal(t, "synthetic fixture evidence", runs[0].ResponseText)
	require.Equal(t, "fixture-upstream", runs[0].MappedModel)
	var scheduled bool
	var unrelated string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT schedulable,extra->>'unrelated' FROM accounts WHERE id=$1", account).Scan(&scheduled, &unrelated))
	require.True(t, scheduled)
	require.Equal(t, "preserve", unrelated)
	var notifications int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM account_quality_notifications").Scan(&notifications))
	require.Zero(t, notifications, "notification transports are disabled by default")
}

func TestQualityPostgresGateAndOutboxRollbackTogether(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, nil))
	f.finish(t, b, "fail")
	r := fixtureResult(f.claim(t, f.binding(t, b.ID)), "fail")
	before := f.outboxes(t)
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION reject_quality_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture outbox failure'; END $$; CREATE TRIGGER fixture_outbox_failure BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION reject_quality_outbox()`)
	require.NoError(t, err)
	require.ErrorContains(t, f.store.Complete(t.Context(), r), "fixture outbox failure")
	got := f.binding(t, b.ID)
	require.False(t, got.Isolated)
	require.Equal(t, 1, got.Failures)
	require.Empty(t, f.gates(t, account))
	require.Equal(t, before, f.outboxes(t))
	stored, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.Equal(t, "running", stored.ExecutionStatus, "evidence and gate changes must roll back together")
	_, err = f.db.ExecContext(t.Context(), "DROP TRIGGER fixture_outbox_failure ON scheduler_outbox")
	require.NoError(t, err)
	require.NoError(t, f.store.Complete(t.Context(), r))
	require.True(t, f.binding(t, b.ID).Isolated)
	require.Len(t, f.gates(t, account), 1)
	require.Equal(t, before+1, f.outboxes(t))
	require.NoError(t, f.store.Complete(t.Context(), r))
	require.Equal(t, before+1, f.outboxes(t), "a repeated completion cannot emit a duplicate isolation action")
}

func TestQualityPostgresStaleCompletionsNeverApply(t *testing.T) {
	for _, mutation := range []string{"policy", "binding", "credential", "proxy", "account_deleted"} {
		t.Run(mutation, func(t *testing.T) {
			f := newQualityPostgresFixture(t)
			account := f.account(t)
			var proxy int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO proxies(host) VALUES('fixture.invalid') RETURNING id").Scan(&proxy))
			_, err := f.db.ExecContext(t.Context(), "UPDATE accounts SET proxy_id=$2 WHERE id=$1", account, proxy)
			require.NoError(t, err)
			p := f.policy(t, func(p *Policy) { p.FailureThreshold = 1 })
			b := f.bind(t, account, p)
			r := fixtureResult(f.claim(t, b), "fail")
			switch mutation {
			case "policy":
				p.Name = "new policy version"
				_, err = f.store.SavePolicy(t.Context(), p, 42)
			case "binding":
				v := false
				_, err = f.store.Control(t.Context(), b.ID, ControlInput{Version: b.Version, Action: "set_auto", Reason: "fixture configuration changed", AutoIsolate: &v}, 42)
			case "credential":
				_, err = f.db.ExecContext(t.Context(), `UPDATE accounts SET credentials=jsonb_set(credentials,'{api_key}','"changed-fixture-key"') WHERE id=$1`, account)
			case "proxy":
				_, err = f.db.ExecContext(t.Context(), "UPDATE proxies SET host='changed.fixture.invalid' WHERE id=$1", proxy)
			case "account_deleted":
				_, err = f.db.ExecContext(t.Context(), "UPDATE accounts SET deleted_at=NOW() WHERE id=$1", account)
			}
			require.NoError(t, err)
			require.False(t, f.store.Authorize(t.Context(), r), "obsolete identity or configuration must be rejected before probe dispatch")
			before := f.outboxes(t)
			require.NoError(t, f.store.Complete(t.Context(), r))
			stored, err := f.store.Run(t.Context(), r.ID)
			require.NoError(t, err)
			require.True(t, stored.Stale)
			require.Equal(t, "not_evaluated", stored.QualityVerdict)
			require.Equal(t, r.ResponseText, stored.ResponseText)
			require.Empty(t, f.gates(t, account))
			require.Equal(t, before, f.outboxes(t))
		})
	}
}

func TestQualityPostgresOAuthRotationPreservesIdentity(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE accounts SET type='oauth',credentials='{"account_id":"fixture-identity","access_token":"old-token","refresh_token":"old-refresh","model_mapping":{"fixture-model":"fixture-upstream"}}' WHERE id=$1`, account)
	require.NoError(t, err)
	b := f.bind(t, account, f.policy(t, nil))
	r := fixtureResult(f.claim(t, b), "pass")
	_, err = f.db.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials||'{"access_token":"new-token","refresh_token":"new-refresh","expires_at":2000000000}'::jsonb WHERE id=$1`, account)
	require.NoError(t, err)
	require.True(t, f.store.Authorize(t.Context(), r), "routine token rotation preserves a known upstream identity")
	require.NoError(t, f.store.Complete(t.Context(), r))
	stored, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.False(t, stored.Stale)
	require.Equal(t, "healthy", f.binding(t, b.ID).State)
}

func TestQualityPostgresQueueIdempotencyAndAccountExclusion(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, nil))
	other := f.bind(t, account, f.policy(t, nil))
	request := uuid.NewString()
	first, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, request, fixtureEstimate)
	require.NoError(t, err)
	retried, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, request, fixtureEstimate)
	require.NoError(t, err)
	require.Equal(t, first.ID, retried.ID)
	_, err = f.store.Queue(t.Context(), other.ID, other.Version, 42, request, fixtureEstimate)
	require.ErrorIs(t, err, ErrConflict)
	_, err = f.store.Queue(t.Context(), other.ID, other.Version, 42, uuid.NewString(), fixtureEstimate)
	require.ErrorIs(t, err, ErrBusy, "different policies cannot probe the same account concurrently")
	claimed, err := f.store.Claim(t.Context())
	require.NoError(t, err)
	require.Equal(t, first.ID, claimed.ID)
	_, err = f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.ErrorIs(t, err, ErrBusy)
	require.NoError(t, f.store.Complete(t.Context(), fixtureResult(claimed, "pass")))
	_, err = f.store.Queue(t.Context(), other.ID, other.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err)
}

func TestQualityPostgresConcurrentClaimsRespectGlobalSlots(t *testing.T) {
	f := newQualityPostgresFixture(t)
	settings := DefaultSettings()
	settings.Concurrency = 1
	_, err := f.store.SaveSettings(t.Context(), settings, 42)
	require.NoError(t, err)
	p := f.policy(t, nil)
	for i := 0; i < 2; i++ {
		b := f.bind(t, f.account(t), p)
		_, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
		require.NoError(t, err)
	}
	type result struct {
		run Run
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := f.store.Claim(t.Context()); results <- result{r, e} }()
	}
	wg.Wait()
	close(results)
	var active Run
	successes, busy := 0, 0
	for result := range results {
		if result.err == nil {
			successes++
			active = result.run
		} else {
			require.ErrorIs(t, result.err, ErrBusy)
			busy++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, busy)
	require.NoError(t, f.store.Complete(t.Context(), fixtureResult(active, "pass")))
	next, err := f.store.Claim(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, active.ID, next.ID)
}

func TestQualityPostgresBudgetsCountAccountsAndReservations(t *testing.T) {
	t.Run("per account includes overlapping policies", func(t *testing.T) {
		f := newQualityPostgresFixture(t)
		account := f.account(t)
		limited := func(p *Policy) { p.DailyRequestLimit = 2 }
		b := f.bind(t, account, f.policy(t, limited))
		other := f.bind(t, account, f.policy(t, limited))
		f.finish(t, b, "pass")
		f.finish(t, b, "pass")
		r, err := f.store.Queue(t.Context(), other.ID, other.Version, 42, uuid.NewString(), fixtureEstimate)
		require.NoError(t, err)
		require.Equal(t, "budget_paused", r.ExecutionStatus, "binding another policy must not multiply the account budget")
		require.Empty(t, f.gates(t, account))
	})
	t.Run("global request budget", func(t *testing.T) {
		f := newQualityPostgresFixture(t)
		settings := DefaultSettings()
		settings.DailyRequestLimit = 1
		_, err := f.store.SaveSettings(t.Context(), settings, 42)
		require.NoError(t, err)
		p := f.policy(t, nil)
		first := f.bind(t, f.account(t), p)
		second := f.bind(t, f.account(t), p)
		f.finish(t, first, "fail")
		r, err := f.store.Queue(t.Context(), second.ID, second.Version, 42, uuid.NewString(), fixtureEstimate)
		require.NoError(t, err)
		require.Equal(t, "budget_paused", r.ExecutionStatus)
		require.Equal(t, 1, f.binding(t, first.ID).Failures)
		require.Equal(t, 0, f.binding(t, second.ID).Failures)
	})
	t.Run("global amount reserves in flight requests", func(t *testing.T) {
		f := newQualityPostgresFixture(t)
		settings := DefaultSettings()
		settings.DailyCostLimitUSD = 1
		_, err := f.store.SaveSettings(t.Context(), settings, 42)
		require.NoError(t, err)
		p := f.policy(t, nil)
		for i := 0; i < 2; i++ {
			b := f.bind(t, f.account(t), p)
			r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), func(Policy, string) *float64 { value := 0.6; return &value })
			require.NoError(t, err)
			require.Equal(t, []string{"queued", "budget_paused"}[i], r.ExecutionStatus)
		}
	})
}

func TestQualityPostgresManualControlsOwnOnlyTheirGate(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	_, err := f.db.ExecContext(t.Context(), "UPDATE accounts SET schedulable=FALSE,temp_unschedulable_until=$2,temp_unschedulable_reason='independent fixture cooldown' WHERE id=$1", account, until)
	require.NoError(t, err)
	p := f.policy(t, nil)
	first, second := f.bind(t, account, p), f.bind(t, account, f.policy(t, nil))
	for _, b := range []*Binding{&first, &second} {
		*b, err = f.store.Control(t.Context(), b.ID, ControlInput{Version: b.Version, Action: "isolate", Reason: "fixture manual isolation"}, 42)
		require.NoError(t, err)
	}
	require.Len(t, f.gates(t, account), 2)
	first, err = f.store.Control(t.Context(), first.ID, ControlInput{Version: first.Version, Action: "override", Reason: "fixture temporary allowance", DurationMinutes: 1}, 42)
	require.NoError(t, err)
	require.NotNil(t, first.OverrideUntil)
	gates := f.gates(t, account)
	require.NotNil(t, gates[strconv.FormatInt(first.ID, 10)].(map[string]any)["override_until"])
	require.Nil(t, gates[strconv.FormatInt(second.ID, 10)].(map[string]any)["override_until"])
	first, err = f.store.Control(t.Context(), first.ID, ControlInput{Version: first.Version, Action: "release", Reason: "fixture owner release"}, 42)
	require.NoError(t, err)
	gates = f.gates(t, account)
	require.Len(t, gates, 1)
	require.Contains(t, gates, strconv.FormatInt(second.ID, 10))
	require.NoError(t, f.store.DeletePolicy(t.Context(), p.ID, p.Version, 42))
	require.Contains(t, f.gates(t, account), strconv.FormatInt(second.ID, 10))
	var scheduled bool
	var gotUntil time.Time
	var reason string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT schedulable,temp_unschedulable_until,temp_unschedulable_reason FROM accounts WHERE id=$1", account).Scan(&scheduled, &gotUntil, &reason))
	require.False(t, scheduled)
	require.True(t, until.Equal(gotUntil))
	require.Equal(t, "independent fixture cooldown", reason)
}

func TestQualityPostgresCancelAndReapRejectLateEvidence(t *testing.T) {
	for _, mode := range []string{"cancel_queued", "cancel_running", "lease_expired"} {
		t.Run(mode, func(t *testing.T) {
			f := newQualityPostgresFixture(t)
			account := f.account(t)
			b := f.bind(t, account, f.policy(t, func(p *Policy) { p.FailureThreshold = 1 }))
			r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
			require.NoError(t, err)
			if mode != "cancel_queued" {
				r, err = f.store.Claim(t.Context())
				require.NoError(t, err)
			}
			if mode == "lease_expired" {
				_, err = f.db.ExecContext(t.Context(), "UPDATE account_quality_runs SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1", r.ID)
				require.NoError(t, err)
				require.NoError(t, f.store.Reap(t.Context()))
			} else {
				require.NoError(t, f.store.Cancel(t.Context(), r.ID, 42))
			}
			require.False(t, f.store.Authorize(t.Context(), r), "cancelled or expired work must not be authorized to start a probe")
			if mode == "cancel_running" {
				_, queueErr := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
				require.ErrorIs(t, queueErr, ErrBusy, "a cancellation request retains the account lease until the in-flight request acknowledges it")
			}
			require.NoError(t, f.store.Complete(t.Context(), fixtureResult(r, "fail")))
			stored, err := f.store.Run(t.Context(), r.ID)
			require.NoError(t, err)
			require.Equal(t, "cancelled", stored.ExecutionStatus)
			require.Equal(t, "not_evaluated", stored.QualityVerdict)
			require.Equal(t, 0, f.binding(t, b.ID).Failures)
			require.Empty(t, f.gates(t, account))
			_, err = f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
			require.NoError(t, err, "cancelled leases must free the unique per-account execution slot")
		})
	}
}

func TestQualityPostgresIdentityResetRefreshesReleasedGate(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, func(p *Policy) { p.FailureThreshold = 1 }))
	f.finish(t, b, "fail")
	require.Len(t, f.gates(t, account), 1)
	b = f.binding(t, b.ID)
	require.NotEmpty(t, b.IdentityVersion, "the baseline must survive database serialization")
	f.mu.Lock()
	f.refreshed = nil
	f.mu.Unlock()
	_, err := f.db.ExecContext(t.Context(), `UPDATE accounts SET credentials=jsonb_set(credentials,'{api_key}','"changed-fixture-key"') WHERE id=$1`, account)
	require.NoError(t, err)
	_, err = f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err)
	require.Empty(t, f.gates(t, account))
	f.mu.Lock()
	refreshed := append([]int64(nil), f.refreshed...)
	f.mu.Unlock()
	require.Contains(t, refreshed, account, fmt.Sprintf("committed identity-reset release for account %d must reach the immediate scheduler snapshot", account))
}

func TestQualityPostgresConcurrentQueueReservesOneAccount(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	bindings := []Binding{f.bind(t, account, f.policy(t, nil)), f.bind(t, account, f.policy(t, nil))}
	results := make(chan error, len(bindings))
	var wg sync.WaitGroup
	for _, binding := range bindings {
		wg.Add(1)
		go func(b Binding) {
			defer wg.Done()
			_, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
			results <- err
		}(binding)
	}
	wg.Wait()
	close(results)
	successes, busy := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrBusy)
			busy++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, busy)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM account_quality_runs WHERE execution_status IN ('queued','running')").Scan(&count))
	require.Equal(t, 1, count)
}

func TestQualityPostgresBindingEditPreservesGateAndUpdatesScope(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	p := f.policy(t, nil)
	b := f.bind(t, account, p)
	b, err := f.store.Control(t.Context(), b.ID, ControlInput{Version: b.Version, Action: "isolate", Reason: "fixture existing quality isolation"}, 42)
	require.NoError(t, err)
	before := f.outboxes(t)
	bound, err := f.store.Bind(t.Context(), BindInput{PolicyID: p.ID, AccountIDs: []int64{account}, AutoIsolate: true, Source: "another overlapping group"}, 42)
	require.NoError(t, err)
	require.Len(t, bound, 1)
	require.Equal(t, b.Version, bound[0].Version, "reapplying equivalent policy assignment is idempotent")
	require.True(t, bound[0].Isolated)
	require.Equal(t, before, f.outboxes(t))
	bound, err = f.store.Bind(t.Context(), BindInput{PolicyID: p.ID, AccountIDs: []int64{account}, AutoIsolate: true, Source: "fixture scope edit", Overrides: Overrides{IsolationScope: "account"}}, 42)
	require.NoError(t, err)
	require.Len(t, bound, 1)
	require.True(t, bound[0].Isolated, "editing assignment does not implicitly release isolation")
	gate := f.gates(t, account)[strconv.FormatInt(b.ID, 10)].(map[string]any)
	require.Equal(t, "account", gate["scope"], "the persisted owner gate must reflect the edited effective isolation scope")
	require.Equal(t, before+1, f.outboxes(t))
}

func TestQualityPostgresNotificationRemindersRespectInterval(t *testing.T) {
	for _, event := range []string{"quarantined", "budget_paused"} {
		t.Run(event, func(t *testing.T) {
			f := newQualityPostgresFixture(t)
			settings := DefaultSettings()
			settings.Notifications.EmailEnabled = true
			settings.Notifications.EmailRecipients = []string{"quality-fixture@example.invalid", "quality-fixture@example.invalid"}
			settings.Notifications.Events = []string{event}
			settings.Notifications.ReminderSeconds = 60
			_, err := f.store.SaveSettings(t.Context(), settings, 42)
			require.NoError(t, err)
			b := f.bind(t, f.account(t), f.policy(t, func(p *Policy) {
				if event == "quarantined" {
					p.FailureThreshold = 1
				} else {
					p.DailyRequestLimit = 1
				}
			}))
			if event == "budget_paused" {
				f.finish(t, b, "pass")
			}
			trigger := func() {
				t.Helper()
				if event == "quarantined" {
					f.finish(t, f.binding(t, b.ID), "fail")
				} else {
					r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
					require.NoError(t, err)
					require.Equal(t, "budget_paused", r.ExecutionStatus)
				}
			}
			count := func() int {
				t.Helper()
				var value int
				require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM account_quality_notifications WHERE binding_id=$1 AND event=$2", b.ID, event).Scan(&value))
				return value
			}
			trigger()
			require.Equal(t, 1, count(), "an explicitly enabled event produces one notice per unique recipient")
			trigger()
			trigger()
			require.Equal(t, 1, count(), "continued isolation or budget pause must not produce notices within the reminder interval")
			_, err = f.db.ExecContext(t.Context(), "UPDATE account_quality_notifications SET created_at=NOW()-INTERVAL '61 seconds' WHERE binding_id=$1 AND event=$2", b.ID, event)
			require.NoError(t, err)
			trigger()
			require.Equal(t, 2, count(), "a continued anomaly produces one reminder after its interval")
			trigger()
			require.Equal(t, 2, count(), "the fresh reminder starts a new suppression interval")
			var body, status string
			require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT body,status FROM account_quality_notifications WHERE binding_id=$1 ORDER BY id DESC LIMIT 1", b.ID).Scan(&body, &status))
			require.Equal(t, "pending", status, "this fixture queues evidence only and never calls a delivery transport")
			require.NotContains(t, body, "fixture-key")
		})
	}
}

func TestQualityPostgresMonetaryBudgetRequiresAccountTypeUpstreamCap(t *testing.T) {
	f := newQualityPostgresFixture(t)
	f.store.Capabilities = []Capability{
		{Platform: "openai", APIMode: "responses", AccountTypes: []string{"apikey"}, AutomaticControl: true, OutputLimitMode: "upstream"},
		{Platform: "openai", APIMode: "responses", AccountTypes: []string{"oauth"}, AutomaticControl: true, OutputLimitMode: "local"},
	}
	settings := DefaultSettings()
	settings.DailyCostLimitUSD = 1
	_, err := f.store.SaveSettings(t.Context(), settings, 42)
	require.NoError(t, err)
	account := f.account(t)
	_, err = f.db.ExecContext(t.Context(), `UPDATE accounts SET type='oauth',credentials='{"account_id":"fixture-identity","access_token":"fixture-token"}'::jsonb WHERE id=$1`, account)
	require.NoError(t, err)
	p := f.policy(t, nil)
	b := f.bind(t, account, p)
	estimates := 0
	r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), func(Policy, string) *float64 {
		estimates++
		value := 0.001
		return &value
	})
	require.NoError(t, err)
	require.Equal(t, 1, estimates)
	require.Equal(t, "budget_paused", r.ExecutionStatus, "an API-key upstream cap cannot certify an OAuth request with only a local cap")
	require.InDelta(t, 0.001, r.ReservedCostUSD, 0.000001)
	require.Empty(t, f.gates(t, account))
	_, err = f.store.Claim(t.Context())
	require.ErrorIs(t, err, ErrNotFound, "a monetary budget pause must never enter the probe execution queue")
	apiBinding := f.bind(t, f.account(t), p)
	apiRun, err := f.store.Queue(t.Context(), apiBinding.ID, apiBinding.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err)
	require.Equal(t, "queued", apiRun.ExecutionStatus, "the matching API-key capability still admits bounded requests")
}

func TestQualityPostgresReservationUsesResolvedUpstreamModel(t *testing.T) {
	for _, limit := range []float64{1, 3} {
		t.Run(fmt.Sprintf("budget_%g", limit), func(t *testing.T) {
			f := newQualityPostgresFixture(t)
			settings := DefaultSettings()
			settings.DailyCostLimitUSD = limit
			_, err := f.store.SaveSettings(t.Context(), settings, 42)
			require.NoError(t, err)
			account := f.account(t)
			_, err = f.db.ExecContext(t.Context(), `UPDATE accounts SET credentials=jsonb_set(credentials,'{model_mapping}','{"cheap-alias":"expensive-upstream-model"}'::jsonb) WHERE id=$1`, account)
			require.NoError(t, err)
			b := f.bind(t, account, f.policy(t, func(p *Policy) { p.Model = "cheap-alias" }))
			resolves := 0
			f.store.ResolveModel = func(_ context.Context, id int64, requested string) (string, error) {
				resolves++
				require.Equal(t, account, id)
				require.Equal(t, "cheap-alias", requested)
				return "expensive-upstream-model", nil
			}
			var estimatedModel string
			r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), func(policy Policy, prompt string) *float64 {
				estimatedModel = policy.Model
				require.NotEmpty(t, prompt)
				value := 0.001
				if policy.Model == "expensive-upstream-model" {
					value = 2
				}
				return &value
			})
			require.NoError(t, err)
			require.Equal(t, 1, resolves)
			require.Equal(t, "expensive-upstream-model", estimatedModel)
			require.Equal(t, "cheap-alias", r.RequestedModel, "evidence preserves the requested logical model")
			require.Equal(t, "cheap-alias", r.Config.Model)
			require.Equal(t, float64(2), r.ReservedCostUSD)
			require.Equal(t, map[bool]string{true: "budget_paused", false: "queued"}[limit < 2], r.ExecutionStatus)
			stored, err := f.store.Run(t.Context(), r.ID)
			require.NoError(t, err)
			require.Equal(t, float64(2), stored.ReservedCostUSD, "the reservation must persist the expensive upstream estimate")
		})
	}
}

type qualityFixtureProberFunc func(context.Context, ProbeRequest) (ProbeResult, error)

func (fn qualityFixtureProberFunc) ProbeAccountText(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	return fn(ctx, request)
}

type qualityFixtureNotifierFunc func(context.Context, string, string) error

func (fn qualityFixtureNotifierFunc) SendAccountQualityNotice(ctx context.Context, recipient, body string) error {
	return fn(ctx, recipient, body)
}

func waitQualityFixtureEvent[T any](t *testing.T, event <-chan T, description string) T {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	select {
	case value := <-event:
		return value
	case <-deadline.C:
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func TestQualityPostgresRuntimeObservesRemoteCancellation(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, nil))
	f.finish(t, b, "fail")
	f.finish(t, b, "fail")
	before := f.binding(t, b.ID)
	beforeGates := f.gates(t, account)
	require.True(t, before.Isolated)
	b = before
	r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err)
	started := make(chan ProbeRequest, 1)
	cancelObserved := make(chan struct{})
	completed := make(chan struct{})
	var completionOnce sync.Once
	f.store.Refresh = func(_ context.Context, id int64) {
		if id == account {
			completionOnce.Do(func() { close(completed) })
		}
	}
	prober := qualityFixtureProberFunc(func(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
		started <- request
		<-ctx.Done()
		close(cancelObserved)
		return ProbeResult{AccountID: request.AccountID, CredentialAccountID: request.AccountID, RequestedModel: request.Model, MappedModel: "fixture-upstream", ExecutionStatus: "cancelled"}, ctx.Err()
	})
	svc := NewService(f.store, prober, f.store.Capabilities)
	t.Cleanup(svc.Stop)
	svc.Start()
	request := waitQualityFixtureEvent(t, started, "service probe dispatch")
	require.Equal(t, account, request.AccountID)
	// Store.Cancel bypasses this service's local cancellation map, as a write
	// from a different administrator node would.
	require.NoError(t, f.store.Cancel(t.Context(), r.ID, 42))
	_, err = f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.ErrorIs(t, err, ErrBusy, "remote cancellation cannot free a still executing account lease")
	waitQualityFixtureEvent(t, cancelObserved, "remote cancellation authorization poll")
	waitQualityFixtureEvent(t, completed, "cancelled probe completion commit")
	stored, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", stored.ExecutionStatus)
	require.True(t, stored.CancelRequested)
	require.Equal(t, "not_evaluated", stored.QualityVerdict)
	after := f.binding(t, b.ID)
	require.Equal(t, before.Failures, after.Failures)
	require.Equal(t, before.RecoveryPasses, after.RecoveryPasses)
	require.Equal(t, before.Isolated, after.Isolated)
	require.Equal(t, beforeGates, f.gates(t, account))
	_, err = f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err, "the acknowledged cancellation releases the per-account execution slot")
}

func TestQualityPostgresRuntimeNoticeDoesNotBlockProbeDispatch(t *testing.T) {
	f := newQualityPostgresFixture(t)
	settings := DefaultSettings()
	settings.Notifications.EmailEnabled = true
	settings.Notifications.EmailRecipients = []string{"quality-runtime@example.invalid"}
	settings.Notifications.Events = []string{"manual"}
	_, err := f.store.SaveSettings(t.Context(), settings, 42)
	require.NoError(t, err)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, nil))
	_, err = f.db.ExecContext(t.Context(), "UPDATE account_quality_bindings SET next_run_at=NOW()+INTERVAL '1 hour' WHERE id=$1", b.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "INSERT INTO account_quality_notifications(dedup_key,event,recipient,body,binding_id) VALUES('runtime-notice','manual',$1,'synthetic runtime notice',$2)", settings.Notifications.EmailRecipients[0], b.ID)
	require.NoError(t, err)
	noticeStarted, noticeReturned := make(chan struct{}), make(chan struct{})
	noticeRelease, probeRelease := make(chan struct{}), make(chan struct{})
	probeStarted := make(chan ProbeRequest, 1)
	probeCommitted := make(chan struct{})
	var noticeOnce, probeOnce, commitOnce sync.Once
	f.store.Refresh = func(_ context.Context, id int64) {
		if id == account {
			commitOnce.Do(func() { close(probeCommitted) })
		}
	}
	prober := qualityFixtureProberFunc(func(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
		probeStarted <- request
		select {
		case <-probeRelease:
			return ProbeResult{AccountID: request.AccountID, CredentialAccountID: request.AccountID, RequestedModel: request.Model, MappedModel: "fixture-upstream", ExecutionStatus: "cancelled"}, nil
		case <-ctx.Done():
			return ProbeResult{AccountID: request.AccountID, ExecutionStatus: "cancelled"}, ctx.Err()
		}
	})
	svc := NewService(f.store, prober, f.store.Capabilities)
	svc.Notifier = qualityFixtureNotifierFunc(func(ctx context.Context, recipient, body string) error {
		if recipient != settings.Notifications.EmailRecipients[0] || body != "synthetic runtime notice" {
			return fmt.Errorf("unexpected synthetic notification")
		}
		close(noticeStarted)
		defer close(noticeReturned)
		select {
		case <-noticeRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	t.Cleanup(func() {
		// Always unblock both fake transports before stopping the worker,
		// including when an assertion fails.
		noticeOnce.Do(func() { close(noticeRelease) })
		probeOnce.Do(func() { close(probeRelease) })
		svc.Stop()
	})
	svc.Start()
	waitQualityFixtureEvent(t, noticeStarted, "blocked synthetic notification delivery")
	r, err := f.store.Queue(t.Context(), b.ID, b.Version, 42, uuid.NewString(), fixtureEstimate)
	require.NoError(t, err)
	request := waitQualityFixtureEvent(t, probeStarted, "fresh probe dispatch while notification delivery is blocked")
	require.Equal(t, account, request.AccountID)
	select {
	case <-noticeReturned:
		t.Fatal("notification fake returned before its release; dispatch independence was not exercised")
	default:
	}
	noticeOnce.Do(func() { close(noticeRelease) })
	waitQualityFixtureEvent(t, noticeReturned, "synthetic notification release")
	probeOnce.Do(func() { close(probeRelease) })
	waitQualityFixtureEvent(t, probeCommitted, "fresh probe completion commit")
	stored, err := f.store.Run(t.Context(), r.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", stored.ExecutionStatus)
}

func TestQualityPostgresAutomaticIsolationInvalidatesOldManualVersion(t *testing.T) {
	f := newQualityPostgresFixture(t)
	account := f.account(t)
	b := f.bind(t, account, f.policy(t, func(p *Policy) { p.FailureThreshold = 1 }))
	f.finish(t, b, "fail")
	current := f.binding(t, b.ID)
	require.True(t, current.Isolated)
	require.Greater(t, current.Version, b.Version, "automatic gate transitions invalidate an administrator's old detail snapshot")
	_, err := f.store.Control(t.Context(), b.ID, ControlInput{Version: b.Version, Action: "release", Reason: "fixture stale manual form"}, 42)
	require.ErrorIs(t, err, ErrConflict)
	require.Len(t, f.gates(t, account), 1)
	released, err := f.store.Control(t.Context(), current.ID, ControlInput{Version: current.Version, Action: "release", Reason: "fixture refreshed manual form"}, 42)
	require.NoError(t, err)
	require.False(t, released.Isolated)
	require.Empty(t, f.gates(t, account))
}
