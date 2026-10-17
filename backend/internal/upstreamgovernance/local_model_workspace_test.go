package upstreamgovernance

import (
	"context"
	"database/sql"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type localModelTargetReaderFixture struct {
	fakeLocal
	target      LocalModelTarget
	resolveCall int
}

func (f *localModelTargetReaderFixture) ListLocalModelTargets(context.Context, int64) ([]LocalModelTarget, error) {
	return []LocalModelTarget{f.target}, nil
}

func (f *localModelTargetReaderFixture) ResolveLocalModelTarget(context.Context, int64, int64, int64) (LocalModelTarget, error) {
	f.resolveCall++
	return f.target, nil
}

type localModelRunnerFixture struct{}

func (localModelRunnerFixture) RunLocalModel(context.Context, LocalModelTarget, ModelRunRequest) (ModelRunResult, error) {
	return ModelRunResult{Success: true, Completed: true, ResponseText: "21"}, nil
}

func localModelServiceFixture(site Site, reader *localModelTargetReaderFixture) *Service {
	svc := NewService(&memoryStore{site: site}, &fakeConnector{}, reader, fakeCipher{}, true)
	svc.SetLocalModelRunner(localModelRunnerFixture{})
	return svc
}

func TestModelTargetRejectsLocalGroupOnUpstreamSite(t *testing.T) {
	reader := &localModelTargetReaderFixture{target: LocalModelTarget{GroupID: 7, APIKeyID: 11, OwnerUserID: 42, Platform: "openai", Key: "local-secret", GroupFingerprint: "group-v1", KeyFingerprint: "key-v1"}}
	svc := localModelServiceFixture(Site{ID: 9, Enabled: true, Status: "connected"}, reader)

	_, _, _, err := svc.modelTarget(t.Context(), 9, &ModelTestConfig{
		TargetType: "local_group", LocalGroupID: 7, LocalAPIKeyID: 11, TargetOwnerUserID: 42,
		Platform: "openai", Model: "gpt-6", APIMode: "chat_completions", Efforts: []string{"medium"},
		Templates: []string{"candy"}, Samples: 1, Concurrency: 1, MaxOutputTokens: 64, TimeoutSeconds: 5,
		InputTokens: 32, Tokenizer: "auto", TokenTolerancePercent: 10,
	})

	require.ErrorIs(t, err, ErrConflict)
	require.Zero(t, reader.resolveCall)
}

func TestModelTargetRejectsUpstreamKeyOnLocalWorkspace(t *testing.T) {
	reader := &localModelTargetReaderFixture{}
	svc := localModelServiceFixture(Site{ID: 9, Enabled: true, Status: localModelSiteStatus, LoginCipher: localModelOwnerPrefix + "42"}, reader)

	_, _, _, err := svc.modelTarget(t.Context(), 9, &ModelTestConfig{
		TargetType: "upstream", ManagedKeyID: 11, Platform: "openai", Model: "gpt-6", APIMode: "chat_completions",
		Efforts: []string{"medium"}, Templates: []string{"candy"}, Samples: 1, Concurrency: 1,
		MaxOutputTokens: 64, TimeoutSeconds: 5, InputTokens: 32, Tokenizer: "auto", TokenTolerancePercent: 10,
	})

	require.ErrorIs(t, err, ErrConflict)
}

func TestLocalModelTargetRequiresOwnerOfHiddenWorkspace(t *testing.T) {
	reader := &localModelTargetReaderFixture{target: LocalModelTarget{GroupID: 7, APIKeyID: 11, OwnerUserID: 99, Platform: "openai", Key: "local-secret", GroupFingerprint: "group-v1", KeyFingerprint: "key-v1"}}
	svc := localModelServiceFixture(Site{ID: 9, Enabled: true, Status: localModelSiteStatus, LoginCipher: localModelOwnerPrefix + "42"}, reader)

	_, _, _, err := svc.modelTarget(t.Context(), 9, &ModelTestConfig{
		TargetType: "local_group", LocalGroupID: 7, LocalAPIKeyID: 11, TargetOwnerUserID: 99,
		Platform: "openai", Model: "gpt-6", APIMode: "chat_completions", Efforts: []string{"medium"},
		Templates: []string{"candy"}, Samples: 1, Concurrency: 1, MaxOutputTokens: 64, TimeoutSeconds: 5,
		InputTokens: 32, Tokenizer: "auto", TokenTolerancePercent: 10,
	})

	require.ErrorIs(t, err, ErrConflict)
	require.Zero(t, reader.resolveCall)
}

func TestEnsureLocalModelSiteIsIdempotentForMemoryStore(t *testing.T) {
	store := &memoryStore{}
	svc := NewService(store, &fakeConnector{}, &fakeLocal{}, fakeCipher{}, true)

	first, err := svc.EnsureLocalModelSite(t.Context(), 42)
	require.NoError(t, err)
	second, err := svc.EnsureLocalModelSite(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.LoginCipher, second.LoginCipher)
}

func TestLocalModelWorkspaceMarkerIsOwnerScoped(t *testing.T) {
	for _, tc := range []struct {
		name string
		site Site
		want int64
		ok   bool
	}{
		{name: "valid", site: Site{Status: localModelSiteStatus, LoginCipher: localModelOwnerPrefix + "42"}, want: 42, ok: true},
		{name: "wrong status", site: Site{Status: "connected", LoginCipher: localModelOwnerPrefix + "42"}},
		{name: "wrong prefix", site: Site{Status: localModelSiteStatus, LoginCipher: "owner:42"}},
		{name: "invalid owner", site: Site{Status: localModelSiteStatus, LoginCipher: localModelOwnerPrefix + "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, ok := localModelOwner(tc.site)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, owner)
		})
	}
}

func TestRemoteSiteLockRejectsLocalWorkspace(t *testing.T) {
	svc := NewService(&memoryStore{site: Site{ID: 9, Enabled: true, Status: localModelSiteStatus, LoginCipher: localModelOwnerPrefix + "42"}}, &fakeConnector{}, &fakeLocal{}, fakeCipher{}, true)

	site, release, err := svc.remoteSiteLock(t.Context(), 9)
	require.ErrorIs(t, err, ErrInvalid)
	require.Nil(t, site)
	require.Nil(t, release)
}

func TestLocalModelWorkspaceConcurrentMemoryCreationDoesNotDuplicate(t *testing.T) {
	store := &memoryStore{}
	svc := NewService(store, &fakeConnector{}, &fakeLocal{}, fakeCipher{}, true)
	var wg sync.WaitGroup
	ids := make(chan int64, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			site, err := svc.EnsureLocalModelSite(t.Context(), 42)
			require.NoError(t, err)
			ids <- site.ID
		}()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		require.Equal(t, int64(1), id)
	}
}

func TestEnsureLocalModelSiteSQLReusesExistingWorkspaceUnderDatabaseLock(t *testing.T) {
	store, mock := storeFixture(t)
	svc := NewService(store, &fakeConnector{}, &fakeLocal{}, fakeCipher{}, true)
	now := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock(hashtext($1))`)).WithArgs("local-model-owner:42").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM upstream_governance_sites WHERE status=\$1 AND login_cipher=\$2 ORDER BY id LIMIT 1`).
		WithArgs("local_model", "local-model-owner:42").
		WillReturnRows(localModelSiteRows(now, 1024, 42))
	mock.ExpectCommit()

	workspace, err := svc.EnsureLocalModelSite(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(1024), workspace.ID)
	require.Equal(t, "local-model-owner:42", workspace.LoginCipher)
}

func TestEnsureLocalModelSiteSQLCreatesWorkspaceUnderDatabaseLock(t *testing.T) {
	store, mock := storeFixture(t)
	svc := NewService(store, &fakeConnector{}, &fakeLocal{}, fakeCipher{}, true)
	now := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock(hashtext($1))`)).WithArgs("local-model-owner:42").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM upstream_governance_sites WHERE status=\$1 AND login_cipher=\$2 ORDER BY id LIMIT 1`).
		WithArgs("local_model", "local-model-owner:42").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO upstream_governance_sites`).
		WithArgs("本地分组检测", "sub2api", "http://127.0.0.1", true, 1440, "local_model", now, "local-model-owner:42").
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "created_at", "updated_at"}).AddRow(1024, 1, now, now))
	mock.ExpectCommit()

	workspace, err := svc.EnsureLocalModelSite(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(1024), workspace.ID)
	require.Equal(t, "local-model-owner:42", workspace.LoginCipher)
	require.Empty(t, workspace.SessionCipher)
}

func localModelSiteRows(now time.Time, id, owner int64) *sqlmock.Rows {
	marker := "local-model-owner:42"
	if owner == 99 {
		marker = "local-model-owner:99"
	}
	return sqlmock.NewRows([]string{"id", "name", "platform", "base_url", "proxy_id", "enabled", "interval_minutes", "version", "session_cipher", "status", "last_error", "last_sync_at", "next_sync_at", "created_at", "updated_at", "balance_monitor", "balance_monitor_state", "login_cipher"}).
		AddRow(id, "本地分组检测", "sub2api", "http://127.0.0.1", nil, true, 1440, 1, "", "local_model", "", nil, now, now, now, `{}`, `{}`, marker)
}
