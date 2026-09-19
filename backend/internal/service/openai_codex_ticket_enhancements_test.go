package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketStateAt(length int, issued time.Time) string {
	rawLength := length / 4 * 3
	if length == 292 {
		rawLength = 217
	}
	if length == 332 {
		rawLength = 249
	}
	b := make([]byte, rawLength)
	b[0] = 128
	binary.BigEndian.PutUint64(b[1:9], uint64(issued.Unix()))
	return base64.URLEncoding.EncodeToString(b)
}

type codexTicketRouteUpstream struct {
	HTTPUpstream
	do func(*http.Request, string) (*http.Response, error)
}

func (u *codexTicketRouteUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return u.do(req, proxy)
}

func TestCodexTicketEnhancementReplayUsesSameRouteAndCandidate(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "verified", false: "mismatch rejected"}[valid], func(t *testing.T) {
			account := ticketTestAccount(41)
			candidate := codexTicketStateAt(292, time.Now())
			calls := 0
			upstream := &codexTicketRouteUpstream{do: func(req *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, "http://fixture", proxy)
				if calls == 1 {
					require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
				} else {
					require.Equal(t, candidate, req.Header.Get(openAICodexTurnStateHeader))
				}
				model := "gpt-6-astra"
				if calls == 2 && !valid {
					model = "wrong"
				}
				h := http.Header{}
				h.Set(openAICodexTurnStateHeader, candidate)
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"model":"` + model + `","status":"completed"}}` + "\n\n"))}, nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, VerifyEnabled: true, HarvestProxyURL: "http://fixture"}, upstream)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, 2, calls)
			require.Equal(t, valid, svc.lookupOpenAICodexTicket(account, "gpt-6-astra") != nil)
		})
	}
}

type codexTicketProxyRepo struct {
	ProxyRepository
	proxies []Proxy
}

func (r *codexTicketProxyRepo) ListByIDs(context.Context, []int64) ([]Proxy, error) {
	return r.proxies, nil
}

func TestCodexTicketEnhancementPoolSkipsExpiredAndRotates(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	proxies := []Proxy{{ID: 1, Name: "expired", Protocol: "http", Host: "expired.example", Port: 80, Status: StatusActive, ExpiresAt: &expired}, {ID: 2, Name: "first", Protocol: "http", Host: "first.example", Port: 80, Status: StatusActive}, {ID: 3, Name: "second", Protocol: "http", Host: "second.example", Port: 80, Status: StatusActive}}
	routes := []string{}
	upstream := &codexTicketRouteUpstream{do: func(_ *http.Request, proxy string) (*http.Response, error) {
		routes = append(routes, proxy)
		return &http.Response{StatusCode: 503, Body: http.NoBody}, nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyIDs: []int64{1, 2, 3}}, upstream)
	svc.SetCodexTicketProxyRepository(&codexTicketProxyRepo{proxies: proxies})
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	state := svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
	state.NextAttemptAt = time.Time{}
	svc.saveOpenAICodexTicketRuntime(context.Background(), account, "292", "gpt-6-astra", state)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, []string{"http://first.example:80", "http://second.example:80"}, routes)
}

func TestCodexTicketEnhancementHarvestConcurrencyBound(t *testing.T) {
	var active, peak atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 20)
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return codexTicketResponse(), nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestConcurrency: 3, HarvestProxyURL: "http://fixture", Models: []string{"gpt-6-astra"}}, upstream)
	accounts := make([]Account, 10)
	for i := range accounts {
		accounts[i] = *ticketTestAccount(int64(i + 1))
		accounts[i].Status = StatusActive
	}
	svc.accountRepo = &codexTicketRefreshRepo{accounts: accounts}
	done := make(chan struct{})
	go func() { defer close(done); svc.refreshOpenAICodexTickets(context.Background()) }()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("workers did not start")
		}
	}
	select {
	case <-started:
		close(release)
		<-done
		t.Fatal("more than three concurrent harvest requests")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	<-done
	require.LessOrEqual(t, peak.Load(), int32(3))
}

func TestCodexTicketEnhancementLateAuthDoesNotRevokeReplacement(t *testing.T) {
	account := ticketTestAccount(41)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	oldState := codexTicketStateAt(292, time.Now().Add(-time.Minute))
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: oldState, Length: 292})
	h := http.Header{"Authorization": []string{"Bearer tok"}, "Chatgpt-Account-Id": []string{"acc-1"}}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	use := svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h)
	require.NotNil(t, use)
	newState := codexTicketStateAt(292, time.Now())
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: newState, Length: 292})
	svc.observeOpenAICodexTicketUse(context.Background(), use, 401, nil)
	require.Equal(t, newState, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").State)
	svc.openaiCodexTickets.Delete(openAICodexTicketModeKey("292", 41, "gpt-6-astra"))
	account.Extra = map[string]any{openAICodexTicketModeExtraKey("292", "gpt-6-astra"): map[string]any{"state": oldState, "length": 292, "credential_hash": codexTicketFixtureHash(account), "issued_at": time.Now().Add(-time.Minute).Truncate(time.Second), "expires_at": time.Now().Add(time.Minute)}}
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"), "stale account snapshots must not resurrect revoked material")
}

func TestCodexTicketEnhancement429RetainsTicketAndPersistsCooldown(t *testing.T) {
	account := ticketTestAccount(41)
	var calls atomic.Int32
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://fixture", Models: []string{"gpt-6-astra"}}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"720"}}, Body: http.NoBody}, nil
	}})
	repo := &codexTicketRefreshRepo{}
	svc.accountRepo = repo
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now()), Length: 292})
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, int32(1), calls.Load())
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	monitor := svc.GetOpenAICodexTicketMonitor()
	require.Len(t, monitor.States, 1)
	require.Greater(t, time.Until(*monitor.States[0].NextAttemptAt), 700*time.Second)
	account.Extra = repo.updates
	restarted := ticketTestService(t, svc.cfg.Gateway.OpenAICodexTicket, svc.httpUpstream)
	restarted.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, int32(1), calls.Load(), "persisted cooldown must survive a new service instance")
}

func TestCodexTicketEnhancementMonitorBoundedAndPrivate(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	for i := int64(1); i <= 2100; i++ {
		account := ticketTestAccount(i)
		svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now()), Length: 292})
	}
	snapshot := svc.GetOpenAICodexTicketMonitor()
	require.NotNil(t, svc.lookupOpenAICodexTicket(ticketTestAccount(1), "gpt-6-astra"), "monitor pruning must not evict an otherwise schedulable ticket")
	require.NotNil(t, svc.lookupOpenAICodexTicket(ticketTestAccount(2100), "gpt-6-astra"))
	require.LessOrEqual(t, len(snapshot.States), 2048)
	require.LessOrEqual(t, len(snapshot.Events), 300)
	require.NotEmpty(t, snapshot.States)
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, secret := range []string{"credential_hash", codexTicketFixtureHash(ticketTestAccount(1)), codexTicketStateAt(292, time.Now()), "Authorization"} {
		require.NotContains(t, string(raw), secret)
	}
}

func codexTicketFixtureHash(account *Account) string {
	sum := sha256.Sum256([]byte(account.GetCredential("access_token") + ":" + account.GetCredential("chatgpt_account_id")))
	return hex.EncodeToString(sum[:])
}

func TestCodexTicketEnhancementRejectsUnboundLegacyAndExpiredIssue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		issued time.Time
		bound  bool
	}{
		{"unbound legacy", time.Now(), false},
		{"old issue despite fresh local expiry", time.Now().Add(-time.Hour), true},
		{"future issue", time.Now().Add(time.Minute), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := ticketTestAccount(41)
			record := map[string]any{"state": codexTicketStateAt(292, tc.issued), "length": 292, "issued_at": tc.issued.Truncate(time.Second), "captured_at": time.Now(), "expires_at": time.Now().Add(time.Hour)}
			if tc.bound {
				record["credential_hash"] = codexTicketFixtureHash(account)
			}
			account.Extra = map[string]any{openAICodexTicketModeExtraKey("292", "gpt-6-astra"): record}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
		})
	}
}

func TestCodexTicketEnhancementTokenReplacementCannotReuseMemory(t *testing.T) {
	account := ticketTestAccount(41)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now()), Length: 292, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	account.Credentials["access_token"] = "replacement-token"
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketEnhancement332RejectsActualModelMismatch(t *testing.T) {
	svc := dualTicketTestService(t, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, codexTicketStateAt(332, time.Now()))
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"model":"other-model","status":"completed"}}` + "\n\n"))}, nil
	}})
	account := ticketTestAccount(41)
	account.Extra = map[string]any{"codex_ticket_mode": "332"}
	_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), account, "fixture", "gpt-6-astra", "http://fixture", time.Second)
	require.Error(t, err)
}

type codexTicketBlockingPersistRepo struct {
	AccountRepository
	started chan struct{}
}

func (r *codexTicketBlockingPersistRepo) UpdateExtra(ctx context.Context, _ int64, _ map[string]any) error {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestCodexTicketEnhancementObservationNeverWaitsForPersistence(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now()), Length: 292})
	h := http.Header{"authorization": []string{"Bearer tok"}, "chatgpt-account-id": []string{"acc-1"}}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	use := svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h)
	require.NotNil(t, use)
	svc.accountRepo = &codexTicketBlockingPersistRepo{started: make(chan struct{}, 1)}
	started := time.Now()
	svc.observeOpenAICodexTicketUse(context.Background(), use, 429, http.Header{"Retry-After": []string{"600"}})
	require.Less(t, time.Since(started), 200*time.Millisecond)
	state := svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
	require.Greater(t, time.Until(state.NextAttemptAt), 590*time.Second)
}

func TestCodexTicketEnhancementOldCredentialFeedbackKeepsCurrentRuntime(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now().Add(-time.Minute)), Length: 292})
	h := http.Header{"Authorization": []string{"Bearer tok"}, "Chatgpt-Account-Id": []string{"acc-1"}}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	use := svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h)
	account.Credentials["access_token"] = "replacement"
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now()), Length: 292})
	current := openAICodexTicketRuntime{CredentialHash: codexTicketFixtureHash(account), NextAttemptAt: time.Now().Add(time.Hour)}
	svc.saveOpenAICodexTicketRuntime(context.Background(), account, "292", "gpt-6-astra", current)
	svc.observeOpenAICodexTicketUse(context.Background(), use, 401, nil)
	got := svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
	require.False(t, got.AuthBlocked)
	require.Equal(t, current.CredentialHash, got.CredentialHash)
	require.Greater(t, time.Until(got.NextAttemptAt), 59*time.Minute)
}

func TestCodexTicketEnhancementSnapshotTracksInjectedTicketAcrossRefresh(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	old := codexTicketStateAt(292, time.Now().Add(-time.Minute))
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: old, Length: 292})
	h := http.Header{"Authorization": []string{"Bearer tok"}, "Chatgpt-Account-Id": []string{"acc-1"}}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: codexTicketStateAt(292, time.Now()), Length: 292})
	use := svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h)
	require.NotNil(t, use, "the snapshot must describe what was injected, even if the cache was replaced before send")
	require.Equal(t, old, use.state)
}

type codexTicketQueuedPersistRepo struct {
	AccountRepository
	gate    chan struct{}
	mu      sync.Mutex
	revoked map[int64]bool
}

func (r *codexTicketQueuedPersistRepo) UpdateExtra(ctx context.Context, _ int64, _ map[string]any) error {
	select {
	case <-r.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *codexTicketQueuedPersistRepo) InvalidateCodexTicket(_ context.Context, id int64, _ string, _ string, _ string, _ string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revoked[id] = true
	return true, nil
}

func TestCodexTicketEnhancementObserverQueueSurvivesBusyWorkers(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	uses := make([]*openAICodexTicketUse, 0, 9)
	for id := int64(1); id <= 9; id++ {
		account := ticketTestAccount(id)
		ticket := boundCodexTicketFixture(account, "gpt-6-astra", 292)
		svc.storeOpenAICodexTicket(context.Background(), account, ticket)
		h := http.Header{"Authorization": []string{"Bearer tok"}, "Chatgpt-Account-Id": []string{"acc-1"}}
		require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
		uses = append(uses, svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h))
	}
	repo := &codexTicketQueuedPersistRepo{gate: make(chan struct{}), revoked: make(map[int64]bool)}
	svc.accountRepo = repo
	for _, use := range uses {
		svc.observeOpenAICodexTicketUse(context.Background(), use, 401, nil)
	}
	close(repo.gate)
	require.Eventually(t, func() bool { repo.mu.Lock(); defer repo.mu.Unlock(); return len(repo.revoked) == 9 }, time.Second, 5*time.Millisecond, "overflow observations must remain queued for bounded workers")
}

func TestCodexTicketEnhancementRuntimeHydrationPreservesLongCooldown(t *testing.T) {
	account := ticketTestAccount(41)
	hash := codexTicketFixtureHash(account)
	account.Extra = map[string]any{openAICodexTicketRuntimePrefix + "292:gpt-6-astra": openAICodexTicketRuntime{CredentialHash: hash, NextAttemptAt: time.Now().Add(time.Hour), AuthBlocked: true, UpdatedAt: time.Now().Add(-time.Minute)}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
	svc.observeOpenAICodexTicketUse(context.Background(), &openAICodexTicketUse{mode: "292", model: "gpt-6-astra", accountID: 41, credentialHash: hash}, 429, nil)
	got := svc.loadOpenAICodexTicketRuntime(&Account{ID: 41, Extra: map[string]any{openAICodexTicketCredentialHashKey: hash}}, "292", "gpt-6-astra")
	require.True(t, got.AuthBlocked)
	require.Greater(t, time.Until(got.NextAttemptAt), 59*time.Minute)
}

func TestCodexTicketEnhancementHydrationSeedsMonitor(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = map[string]any{openAICodexTicketModeExtraKey("292", "gpt-6-astra"): boundCodexTicketFixture(account, "gpt-6-astra", 292)}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	for i := 0; i < 3; i++ {
		require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	}
	snapshot := svc.GetOpenAICodexTicketMonitor()
	require.Len(t, snapshot.States, 1)
	require.Empty(t, snapshot.Events, "periodic hydration must not flood events")
}

func TestCodexTicketEnhancementPersistFailureRemainsVisible(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://fixture"}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { return codexTicketResponse(), nil }})
	svc.accountRepo = &codexTicketLifecycleRepo{persist: func(context.Context) error { return errors.New("database secret detail") }}
	svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(41), "gpt-6-astra")
	state := svc.GetOpenAICodexTicketMonitor().States[0]
	require.Equal(t, "persist_failed", state.LastError)
	require.Equal(t, "error", state.Status)
}

func TestCodexTicketEnhancementStopCancelsBackgroundPersistence(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	started, cancelled := make(chan struct{}), make(chan struct{})
	var startOnce, cancelOnce sync.Once
	svc.accountRepo = &codexTicketLifecycleRepo{persist: func(ctx context.Context) error {
		startOnce.Do(func() { close(started) })
		<-ctx.Done()
		cancelOnce.Do(func() { close(cancelled) })
		return ctx.Err()
	}}
	account := ticketTestAccount(41)
	svc.observeOpenAICodexTicketUse(context.Background(), &openAICodexTicketUse{mode: "292", model: "gpt-6-astra", accountID: 41, credentialHash: codexTicketFixtureHash(account)}, 429, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	svc.StopOpenAICodexTicketHarvester()
	select {
	case <-cancelled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stop did not cancel background persistence")
	}
}

func TestCodexTicketEnhancementEqualRevisionMergesPersistedSafetyState(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	svc.saveOpenAICodexTicketRuntime(context.Background(), account, "292", "gpt-6-astra", openAICodexTicketRuntime{CredentialHash: codexTicketFixtureHash(account), NextAttemptAt: time.Now().Add(5 * time.Minute), PreferredRoute: "proxy:2"})
	persisted := svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
	persisted.AuthBlocked = true
	persisted.NextAttemptAt = time.Now().Add(time.Hour)
	persisted.PreferredRoute = "older-route"
	account.Extra = map[string]any{openAICodexTicketRuntimePrefix + "292:gpt-6-astra": persisted}
	got := svc.loadOpenAICodexTicketRuntime(account, "292", "gpt-6-astra")
	require.True(t, got.AuthBlocked, "an equal metadata timestamp cannot discard DB auth suspension")
	require.Greater(t, time.Until(got.NextAttemptAt), 59*time.Minute)
	require.Equal(t, "proxy:2", got.PreferredRoute, "timestamp ordering still chooses route details")
}

func TestCodexTicketEnhancementRefreshHydratesRuntimeForReadyTickets(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	hash := codexTicketFixtureHash(account)
	account.Extra = map[string]any{
		openAICodexTicketModeExtraKey("292", "gpt-6-astra"): boundCodexTicketFixture(account, "gpt-6-astra", 292),
		openAICodexTicketRuntimePrefix + "292:gpt-6-astra":  openAICodexTicketRuntime{CredentialHash: hash, AuthBlocked: true, NextAttemptAt: time.Now().Add(time.Hour), UpdatedAt: time.Now().Add(-time.Minute)},
	}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}}, nil)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	svc.refreshOpenAICodexTicketsForMode(context.Background(), "292")
	h := http.Header{"Authorization": []string{"Bearer tok"}, "Chatgpt-Account-Id": []string{"acc-1"}}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
	use := svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h)
	require.NotNil(t, use)
	svc.observeOpenAICodexTicketUse(context.Background(), use, 429, nil)
	got := svc.loadOpenAICodexTicketRuntime(&Account{ID: 41, Extra: map[string]any{openAICodexTicketCredentialHashKey: hash}}, "292", "gpt-6-astra")
	require.True(t, got.AuthBlocked)
	require.Greater(t, time.Until(got.NextAttemptAt), 59*time.Minute)
}

type codexTicketPublishRepo struct {
	AccountRepository
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	ticket  *openAICodexTicket
}

func (r *codexTicketPublishRepo) UpdateExtra(ctx context.Context, _ int64, updates map[string]any) error {
	for key, value := range updates {
		if strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix) {
			select {
			case r.started <- struct{}{}:
			default:
			}
			select {
			case <-r.release:
			case <-ctx.Done():
				return ctx.Err()
			}
			ticket, ok := value.(*openAICodexTicket)
			if !ok {
				return errors.New("unexpected ticket fixture type")
			}
			r.mu.Lock()
			r.ticket = ticket
			r.mu.Unlock()
		}
	}
	return nil
}
func (r *codexTicketPublishRepo) InvalidateCodexTicket(_ context.Context, _ int64, _ string, _ string, state, hash string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ticket != nil && r.ticket.State == state && r.ticket.CredentialHash == hash {
		r.ticket = nil
		return true, nil
	}
	return false, nil
}

func TestCodexTicketEnhancementPersistenceCannotResurrectRevokedCapture(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "first capture stays private until durable", true: "same state recapture after auth failure"}[existing], func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
			account := ticketTestAccount(41)
			ticket := boundCodexTicketFixture(account, "gpt-6-astra", 292)
			var use *openAICodexTicketUse
			if existing {
				svc.storeOpenAICodexTicket(context.Background(), account, ticket)
				h := http.Header{"Authorization": []string{"Bearer tok"}, "Chatgpt-Account-Id": []string{"acc-1"}}
				require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", h))
				use = svc.snapshotOpenAICodexTicketUse(context.Background(), account, "gpt-6-astra", h)
			}
			repo := &codexTicketPublishRepo{started: make(chan struct{}, 1), release: make(chan struct{})}
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(repo.release) }); svc.StopOpenAICodexTicketHarvester() })
			svc.accountRepo = repo
			done := make(chan struct{})
			go func() { defer close(done); svc.storeOpenAICodexTicket(context.Background(), account, ticket) }()
			select {
			case <-repo.started:
			case <-time.After(time.Second):
				t.Fatal("ticket write did not start")
			}
			if !existing {
				require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			} else {
				svc.observeOpenAICodexTicketUse(context.Background(), use, 401, nil)
				time.Sleep(20 * time.Millisecond)
			}
			releaseOnce.Do(func() { close(repo.release) })
			<-done
			if existing {
				require.Eventually(t, func() bool { repo.mu.Lock(); defer repo.mu.Unlock(); return repo.ticket == nil }, time.Second, 5*time.Millisecond)
				require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			} else {
				require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			}
		})
	}
}
