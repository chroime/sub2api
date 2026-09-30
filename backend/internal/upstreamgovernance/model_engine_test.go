package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type modelEngineRunnerFixture struct {
	fakeConnector
	run func(context.Context, Site, RemoteKey, ModelRunRequest) (ModelRunResult, error)
}

func (r *modelEngineRunnerFixture) RunModel(ctx context.Context, s Site, k RemoteKey, q ModelRunRequest) (ModelRunResult, error) {
	if r.run != nil {
		return r.run(ctx, s, k, q)
	}
	return ModelRunResult{Success: true, ResponseText: "21", RequestBody: json.RawMessage(`{"fixture":true}`), InputText: "original fixture prompt", HTML: "<html>fixture</html>", DurationMS: 12}, nil
}

type modelEngineFixture struct {
	service *Service
	store   *modelStore
	db      *sql.DB
	site    Site
	key     ManagedKey
	runner  *modelEngineRunnerFixture
	config  ModelTestConfig
}

func newModelEngineFixture(t *testing.T) *modelEngineFixture {
	t.Helper()
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
	schema := fmt.Sprintf("governance_models_fixture_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(40)
	t.Cleanup(func() { db.Close(); base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() })
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ); INSERT INTO groups(id) VALUES(1)`)
	require.NoError(t, err)
	for _, name := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "253_upstream_governance_automation.sql", "254_upstream_governance_recharge_plans.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql", "257_upstream_governance_model_monitoring.sql", "258_upstream_governance_key_health.sql"} {
		raw, e := os.ReadFile("../../migrations/" + name)
		require.NoError(t, e)
		_, e = db.Exec(string(raw))
		require.NoError(t, e, name)
	}
	store := NewSQLStore(db)
	cipher := fakeCipher{}
	session, _ := cipher.Encrypt(`{"access_token":"fixture-session","user_id":5}`)
	site := Site{Name: "Model fixture", Platform: "sub2api", BaseURL: "https://fixture.example", Enabled: true, IntervalMinutes: 15, SessionCipher: session, Status: "connected", NextSyncAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.CreateSite(t.Context(), &site))
	snapshot := &Snapshot{SiteID: site.ID, SiteVersion: site.Version, Catalog: Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "g1", Name: "Fixture", Platform: "openai", Models: []string{"gpt-6-astra"}}}}, CreatedAt: time.Now()}
	require.NoError(t, store.SaveSnapshot(t.Context(), snapshot, nil))
	keyCipher, _ := cipher.Encrypt(`{"id":"key-1","key":"fixture-model-key"}`)
	key := ManagedKey{SiteID: site.ID, RemoteGroupID: "g1", Platform: "openai", RemoteKeyID: "key-1", Marker: "model-fixture-key", OwnerUserID: 5, KeyCipher: keyCipher}
	require.NoError(t, store.SaveManagedKey(t.Context(), &key))
	runner := &modelEngineRunnerFixture{}
	s := NewService(store, runner, &fakeLocal{}, cipher, true)
	m, err := s.models()
	require.NoError(t, err)
	t.Cleanup(s.stopModelWorker)
	return &modelEngineFixture{service: s, store: m, db: db, site: site, key: key, runner: runner, config: ModelTestConfig{ManagedKeyID: key.ID, Model: "gpt-6-astra", APIMode: "responses", Efforts: []string{"medium"}, Templates: []string{"candy"}, Samples: 1, Concurrency: 1, MaxOutputTokens: 4096, TimeoutSeconds: 60, InputTokens: 1024, Tokenizer: "o200k_base", TokenTolerancePercent: 10}}
}
func (f *modelEngineFixture) batch(t *testing.T, id string, c ModelTestConfig) *ModelBatch {
	t.Helper()
	b, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: id, Config: c})
	require.NoError(t, err)
	return b
}
func (f *modelEngineFixture) policy(t *testing.T, enabled bool) *ModelPolicy {
	t.Helper()
	p, err := f.service.SaveModelPolicy(t.Context(), f.site.ID, ModelPolicy{Name: "Fixture schedule", Config: f.config, Enabled: enabled, IntervalMinutes: 1, DailyRequestLimit: 10, FailureThreshold: 2, NotifyEnabled: true})
	require.NoError(t, err)
	return p
}
func (f *modelEngineFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.QueryRow(query, args...).Scan(&n))
	return n
}

func TestModelEngineValidationAndIdentity(t *testing.T) {
	f := newModelEngineFixture(t)
	for _, mutate := range []func(*ModelTestConfig){func(c *ModelTestConfig) { c.Concurrency = 0 }, func(c *ModelTestConfig) { c.Concurrency = 33 }, func(c *ModelTestConfig) { c.Samples = 101 }, func(c *ModelTestConfig) { c.Templates = []string{"candy", "candy"} }, func(c *ModelTestConfig) { c.Efforts = []string{"ultra"} }, func(c *ModelTestConfig) { c.FirstContentTimeoutSeconds = 61 }, func(c *ModelTestConfig) { c.Tokenizer = "made-up" }, func(c *ModelTestConfig) { c.Templates = []string{"context"}; c.InputTokens = 0 }} {
		c := f.config
		mutate(&c)
		_, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "invalid", Config: c})
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err := f.service.StartModelBatch(t.Context(), 999, ModelBatchInput{RequestID: "other-site", Config: f.config})
	require.ErrorIs(t, err, ErrNotFound)
	other := f.site
	other.ID = 0
	other.Name = "Other"
	require.NoError(t, f.service.store.CreateSite(t.Context(), &other))
	_, err = f.service.StartModelBatch(t.Context(), other.ID, ModelBatchInput{RequestID: "cross-key", Config: f.config})
	require.ErrorIs(t, err, ErrNotFound)
	for _, platform := range []string{"openai", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go"} {
		_, err = f.db.Exec(`UPDATE upstream_governance_keys SET platform=$1 WHERE id=$2`, platform, f.key.ID)
		require.NoError(t, err)
		c := f.config
		c.Tokenizer = "none"
		c.Templates = []string{"candy", "pelican", "token_audit", "context", "probe"}
		_, _, _, err = f.service.modelTarget(t.Context(), f.site.ID, &c)
		require.NoError(t, err, platform)
	}
	_, err = f.db.Exec(`UPDATE upstream_governance_keys SET platform='openai',owner_user_id=6 WHERE id=$1`, f.key.ID)
	require.NoError(t, err)
	_, err = f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "wrong-owner", Config: f.config})
	require.ErrorIs(t, err, ErrConflict)
	_, err = f.db.Exec(`UPDATE upstream_governance_keys SET owner_user_id=5 WHERE id=$1`, f.key.ID)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE upstream_governance_sites SET version=version+1 WHERE id=$1`, f.site.ID)
	require.NoError(t, err)
	_, err = f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "stale-catalog", Config: f.config})
	require.ErrorIs(t, err, ErrConflict)
	require.Zero(t, f.count(t, `SELECT count(*) FROM upstream_governance_model_runs`))
}

func TestModelEngineQueueIdempotencyAndSummaries(t *testing.T) {
	f := newModelEngineFixture(t)
	c := f.config
	c.Samples = 4
	c.Concurrency = 2
	b := f.batch(t, "same-request", c)
	again := f.batch(t, "same-request", c)
	require.Equal(t, b.ID, again.ID)
	c.Concurrency = 3
	_, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "same-request", Config: c})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, 4, f.count(t, `SELECT count(*) FROM upstream_governance_model_runs`))
	claim, err := f.store.claim(t.Context(), "fixture-owner", time.Now())
	require.NoError(t, err)
	require.NotNil(t, claim)
	r := ModelRunResult{Success: true, ResponseText: strings.Repeat("original", 1000), InputText: "original prompt", HTML: "<svg>full original</svg>", RequestBody: json.RawMessage(`{"secret":"no auth headers here"}`), DurationMS: 42}
	require.NoError(t, f.store.finish(t.Context(), claim.Run.ID, "fixture-owner", "succeeded", r, time.Now()))
	page, err := f.service.ListModelRuns(t.Context(), f.site.ID, 1, 1, b.ID)
	require.NoError(t, err)
	require.EqualValues(t, 4, page.Total)
	require.EqualValues(t, 3, page.Counts["queued"])
	require.Empty(t, page.Items[0].Result.ResponseText)
	require.Empty(t, page.Items[0].Result.HTML)
	detail, err := f.service.GetModelRun(t.Context(), f.site.ID, claim.Run.ID)
	require.NoError(t, err)
	require.Equal(t, r.ResponseText, detail.Result.ResponseText)
	require.Equal(t, r.HTML, detail.Result.HTML)
	_, err = f.service.GetModelRun(t.Context(), 999, claim.Run.ID)
	require.ErrorIs(t, err, ErrNotFound)
	reviewed, err := f.service.ReviewModelRun(t.Context(), f.site.ID, claim.Run.ID, "pass", "proof reviewed", 47)
	require.NoError(t, err)
	require.EqualValues(t, 47, *reviewed.ReviewerID)
	require.NotNil(t, reviewed.ReviewedAt)
	require.EqualValues(t, 1, reviewed.ReviewVersion)
	require.Equal(t, r.ResponseText, reviewed.Result.ResponseText)
	_, err = f.service.ReviewModelRun(t.Context(), f.site.ID, claim.Run.ID, "passed", "bad contract")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestModelEngineScheduleAtomicBudgetAndDisable(t *testing.T) {
	f := newModelEngineFixture(t)
	f.config.Samples = 3
	p, err := f.service.SaveModelPolicy(t.Context(), f.site.ID, ModelPolicy{Name: "Atomic budget", Config: f.config, Enabled: true, IntervalMinutes: 1, DailyRequestLimit: 6, FailureThreshold: 2})
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.service.scheduleModels(context.Background(), f.store) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_batches`))
	require.Equal(t, 3, f.count(t, `SELECT sum(reserved_requests) FROM upstream_governance_model_budgets`))
	finishQueued := func() {
		_, err = f.db.Exec(`UPDATE upstream_governance_model_runs SET status='succeeded'; UPDATE upstream_governance_model_batches SET status='completed'`)
		require.NoError(t, err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		finishQueued()
		_, err = f.db.Exec(`UPDATE upstream_governance_model_policies SET next_run_at=NOW()-interval '1 minute' WHERE id=$1`, p.ID)
		require.NoError(t, err)
		require.NoError(t, f.service.scheduleModels(t.Context(), f.store))
	}
	require.Equal(t, 2, f.count(t, `SELECT count(*) FROM upstream_governance_model_batches`))
	require.Equal(t, 6, f.count(t, `SELECT sum(reserved_requests) FROM upstream_governance_model_budgets`))
	got, err := f.store.policy(t.Context(), f.site.ID, p.ID)
	require.NoError(t, err)
	require.Equal(t, "daily_request_budget", got.LastError)
	got.Enabled = false
	_, err = f.service.SaveModelPolicy(t.Context(), f.site.ID, *got)
	require.NoError(t, err)
	_, err = f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "disabled-manual", PolicyID: p.ID})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, 2, f.count(t, `SELECT count(*) FROM upstream_governance_model_batches`))
}

func TestModelEngineClaimCapTargetExclusionAndRestart(t *testing.T) {
	f := newModelEngineFixture(t)
	c := f.config
	c.Samples = 4
	c.Concurrency = 2
	b := f.batch(t, "first", c)
	other := f.batch(t, "second", c)
	one, err := f.store.claim(t.Context(), "first-worker", time.Now())
	require.NoError(t, err)
	require.Equal(t, b.ID, one.Run.BatchID)
	two, err := f.store.claim(t.Context(), "other-worker", time.Now())
	require.NoError(t, err)
	require.Equal(t, b.ID, two.Run.BatchID)
	three, err := f.store.claim(t.Context(), "other-worker", time.Now())
	require.NoError(t, err)
	require.Nil(t, three, "both batch concurrency and target exclusion must apply")
	_, err = f.db.Exec(`UPDATE upstream_governance_model_runs SET lease_until=NOW()-interval '1 second' WHERE id=$1`, one.Run.ID)
	require.NoError(t, err)
	three, err = f.store.claim(t.Context(), "restarted-worker", time.Now())
	require.NoError(t, err)
	require.NotNil(t, three)
	require.NotEqual(t, one.Run.ID, three.Run.ID)
	require.Equal(t, b.ID, three.Run.BatchID)
	previous, err := f.service.GetModelRun(t.Context(), f.site.ID, one.Run.ID)
	require.NoError(t, err)
	require.Equal(t, "indeterminate", previous.Status)
	require.NoError(t, f.service.CancelModelBatch(t.Context(), f.site.ID, b.ID))
	ok, err := f.store.renew(t.Context(), two.Run.ID, "other-worker", time.Now())
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, f.store.finish(t.Context(), two.Run.ID, "other-worker", "cancelled", ModelRunResult{}, time.Now()))
	require.NoError(t, f.store.finish(t.Context(), three.Run.ID, "restarted-worker", "cancelled", ModelRunResult{}, time.Now()))
	next, err := f.store.claim(t.Context(), "restarted-worker", time.Now())
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Equal(t, other.ID, next.Run.BatchID)
}

func TestModelEngineRunsActuallyParallelAndCancellationStopsRequests(t *testing.T) {
	f := newModelEngineFixture(t)
	var active, maxActive, calls atomic.Int32
	entered := make(chan struct{}, 10)
	f.runner.run = func(ctx context.Context, _ Site, k RemoteKey, _ ModelRunRequest) (ModelRunResult, error) {
		if k.Key != "fixture-model-key" {
			return ModelRunResult{}, ErrInvalid
		}
		calls.Add(1)
		now := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); now > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, now) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return ModelRunResult{ResponseText: "partial", ErrorCode: "cancelled"}, ctx.Err()
	}
	c := f.config
	c.Samples = 6
	c.Concurrency = 3
	b := f.batch(t, "parallel", c)
	f.service.startModelWorker()
	for i := 0; i < 3; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("requested concurrency never started")
		}
	}
	require.EqualValues(t, 3, maxActive.Load())
	require.EqualValues(t, 3, calls.Load())
	require.NoError(t, f.service.CancelModelBatch(t.Context(), f.site.ID, b.ID))
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT count(*) FROM upstream_governance_model_runs WHERE status='cancelled'`) == 6
	}, 5*time.Second, 30*time.Millisecond)
	require.Zero(t, active.Load())
	require.EqualValues(t, 3, calls.Load(), "cancelled queued samples must never reach upstream")
	f.service.stopModelWorker()
}

func TestModelEngineStaleKeyAndMissingGroupNeverRun(t *testing.T) {
	f := newModelEngineFixture(t)
	var calls atomic.Int32
	f.runner.run = func(context.Context, Site, RemoteKey, ModelRunRequest) (ModelRunResult, error) {
		calls.Add(1)
		return ModelRunResult{Success: true}, nil
	}
	f.batch(t, "stale-key", f.config)
	claim, err := f.store.claim(t.Context(), "fixture", time.Now())
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE upstream_governance_keys SET key_cipher=$2 WHERE id=$1`, f.key.ID, "changed-key-cipher")
	require.NoError(t, err)
	f.service.executeModelRun(t.Context(), t.Context(), f.store, "fixture", *claim)
	r, err := f.service.GetModelRun(t.Context(), f.site.ID, claim.Run.ID)
	require.NoError(t, err)
	require.Equal(t, "skipped", r.Status)
	require.Equal(t, "target_changed", r.Result.ErrorCode)
	require.Zero(t, calls.Load())
	_, err = f.db.Exec(`UPDATE upstream_governance_keys SET key_cipher=$2 WHERE id=$1`, f.key.ID, f.key.KeyCipher)
	require.NoError(t, err)
	p := f.policy(t, true)
	_, err = f.db.Exec(`UPDATE upstream_governance_snapshots SET catalog='{"groups_complete":true,"groups":[]}'::jsonb WHERE site_id=$1`, f.site.ID)
	require.NoError(t, err)
	require.NoError(t, f.service.scheduleModels(t.Context(), f.store))
	p, err = f.store.policy(t.Context(), f.site.ID, p.ID)
	require.NoError(t, err)
	require.False(t, p.Enabled)
	require.Equal(t, "upstream_group_missing", p.LastError)
	require.Zero(t, calls.Load())
}

func TestModelEngineLegacyTakeoverAndPolicyRevision(t *testing.T) {
	f := newModelEngineFixture(t)
	_, err := f.db.Exec(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,marker,probe_enabled,probe_model) VALUES($1,'g1','openai',1,'legacy-probe',TRUE,'gpt-6-astra')`, f.site.ID)
	require.NoError(t, err)
	p := ModelPolicy{Name: "Take over", Config: f.config, Enabled: true, IntervalMinutes: 1, DailyRequestLimit: 10, FailureThreshold: 2}
	_, err = f.service.SaveModelPolicy(t.Context(), f.site.ID, p)
	require.ErrorIs(t, err, ErrConflict)
	p.TakeOverLegacy = true
	stored, err := f.service.SaveModelPolicy(t.Context(), f.site.ID, p)
	require.NoError(t, err)
	require.Zero(t, f.count(t, `SELECT count(*) FROM upstream_governance_bindings WHERE probe_enabled`))
	conflict, err := f.service.modelLegacyConflict(t.Context(), f.site.ID, &Binding{RemoteGroupID: "g1", Platform: "openai"}, f.config.Model)
	require.NoError(t, err)
	require.True(t, conflict)
	b, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "policy-revision", PolicyID: stored.ID})
	require.NoError(t, err)
	stale := *stored
	stored.Config.Model = "gpt-6-sol"
	stored, err = f.service.SaveModelPolicy(t.Context(), f.site.ID, *stored)
	require.NoError(t, err)
	_, err = f.service.SaveModelPolicy(t.Context(), f.site.ID, stale)
	require.ErrorIs(t, err, ErrConflict)
	claim, err := f.store.claim(t.Context(), "new-worker", time.Now())
	require.NoError(t, err)
	require.Nil(t, claim)
	page, err := f.service.ListModelRuns(t.Context(), f.site.ID, 1, 20, b.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Counts["cancelled"])
	_, err = f.db.Exec(`UPDATE upstream_governance_keys SET key_cipher='' WHERE id=$1`, f.key.ID)
	require.Error(t, err, "schema protects broken key state")
	require.NoError(t, f.service.DeleteModelPolicy(t.Context(), f.site.ID, stored.ID, stored.Version))
	err = f.service.DeleteModelPolicy(t.Context(), f.site.ID, stored.ID, stored.Version)
	require.True(t, errors.Is(err, ErrNotFound))
}
