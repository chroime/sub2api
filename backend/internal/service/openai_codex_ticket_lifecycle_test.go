package service

import (
	"context"
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

type codexTicketFuncUpstream struct {
	HTTPUpstream
	do func(*http.Request) (*http.Response, error)
}

func (u *codexTicketFuncUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.do(req)
}
func codexTicketResponse() *http.Response {
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(292))
	return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader("data: {}\n\n"))}
}

func TestCodexTicketProbeBypassesPluginDuringWiring(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, unavailable: "plugin must not handle synthetic probes"})
	var calls atomic.Int64
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if HTTPUpstreamProfileFromContext(req.Context()) != HTTPUpstreamProfileOpenAIHarvest || !req.Close {
			return nil, errors.New("missing no-reuse transport profile")
		}
		return codexTicketResponse(), nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	svc.SetPluginManager(manager)
	account := ticketTestAccount(41)
	// This binding rejects ordinary traffic; harvesting still uses the dedicated transport.
	request, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	_, err := svc.doOpenAIUpstream(request, "", account)
	require.Error(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			svc.SetPluginManager(manager)
		}
	}()
	close(start)
	for i := 0; i < 20; i++ {
		state, status, err := svc.fireOpenAICodexTicketProbe(context.Background(), account, "test-token", "gpt-6-astra", "http://proxy.example.com:8080", time.Second)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, state, 292)
	}
	wg.Wait()
	require.Equal(t, int64(20), calls.Load())
}

type codexTicketLifecycleRepo struct {
	AccountRepository
	account Account
	list    func(context.Context) ([]Account, error)
	persist func(context.Context) error
}

func (r *codexTicketLifecycleRepo) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	if r.list != nil {
		return r.list(ctx)
	}
	return []Account{r.account}, nil
}
func (r *codexTicketLifecycleRepo) UpdateExtra(ctx context.Context, _ int64, _ map[string]any) error {
	if r.persist != nil {
		return r.persist(ctx)
	}
	return nil
}

type codexTicketLifecycleSettings struct {
	SettingRepository
	get func(context.Context, string) (string, error)
}

func (r *codexTicketLifecycleSettings) GetValue(ctx context.Context, key string) (string, error) {
	return r.get(ctx, key)
}

func TestCodexTicketHarvesterStopCancelsInFlightWork(t *testing.T) {
	for _, stage := range []string{"settings-enabled", "settings-proxy", "accounts", "upstream", "persist"} {
		t.Run(stage, func(t *testing.T) {
			started := make(chan struct{})
			cancelled := make(chan struct{})
			var once sync.Once
			block := func(ctx context.Context) error {
				once.Do(func() { close(started) })
				<-ctx.Done()
				close(cancelled)
				return ctx.Err()
			}
			account := ticketTestAccount(41)
			account.Status = StatusActive
			repo := &codexTicketLifecycleRepo{account: *account}
			upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				if stage == "upstream" {
					return nil, block(req.Context())
				}
				return codexTicketResponse(), nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example.com:8080", HarvestAttemptTimeoutSeconds: 25, Models: []string{"gpt-6-astra"}}, upstream)
			svc.accountRepo = repo
			if stage == "accounts" {
				repo.list = func(ctx context.Context) ([]Account, error) { return nil, block(ctx) }
			}
			if stage == "persist" {
				repo.persist = block
			}
			if strings.HasPrefix(stage, "settings-") {
				svc.settingService = NewSettingService(&codexTicketLifecycleSettings{get: func(ctx context.Context, key string) (string, error) {
					if stage == "settings-enabled" && key == SettingKeyOpenAICodexTicketEnabled || stage == "settings-proxy" && key == SettingKeyOpenAICodexTicketHarvestProxyURL {
						return "", block(ctx)
					}
					if key == SettingKeyOpenAICodexTicketEnabled {
						return "true", nil
					}
					return "", ErrSettingNotFound
				}}, svc.cfg)
			}
			svc.StartOpenAICodexTicketHarvester()
			t.Cleanup(svc.StopOpenAICodexTicketHarvester)
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("harvester did not reach " + stage)
			}
			// Repeated start must not create a second loop or overwrite the cancellation state.
			svc.StartOpenAICodexTicketHarvester()
			stopped := make(chan struct{})
			go func() { svc.StopOpenAICodexTicketHarvester(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("stop waited for the probe timeout")
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("in-flight operation did not receive cancellation")
			}
			svc.StopOpenAICodexTicketHarvester()
			svc.StartOpenAICodexTicketHarvester()
		})
	}
}

type codexTicketHeaderOnlyBody struct{ reads, closes int }

func (b *codexTicketHeaderOnlyBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *codexTicketHeaderOnlyBody) Close() error             { b.closes++; return nil }
func TestCodexTicketProbeClosesStreamWithoutDraining(t *testing.T) {
	body := &codexTicketHeaderOnlyBody{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		response := codexTicketResponse()
		response.Body = body
		return response, nil
	}})
	_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "test-token", "gpt-6-astra", "", time.Second)
	require.NoError(t, err)
	require.Zero(t, body.reads)
	require.Equal(t, 1, body.closes)
}

func TestCodexTicketPolicyExemptsCredentialShadows(t *testing.T) {
	parentID := int64(41)
	parent := ticketTestAccount(parentID)
	shadow := ticketTestAccount(42)
	shadow.ParentAccountID = &parentID
	shadow.Status = StatusActive
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://proxy.example.com:8080"}
	upstream := &httpUpstreamRecorder{}
	svc := ticketTestService(t, cfg, upstream)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*shadow}}
	require.True(t, svc.openAICodexTicketBlocksAccount(parent, "gpt-6-astra"))
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		shadow.Type = accountType
		require.False(t, svc.openAICodexTicketBlocksAccount(shadow, "gpt-6-astra"))
		headers := http.Header{}
		headers.Set(openAICodexTurnStateHeader, "client-state")
		require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), shadow, "gpt-6-astra", headers))
		require.Equal(t, "client-state", headers.Get(openAICodexTurnStateHeader))
		require.Empty(t, OpenAICodexTicketStatuses(shadow, cfg, time.Now()))
		svc.probeOnceOpenAICodexTicket(context.Background(), shadow, "gpt-6-astra")
	}
	svc.refreshOpenAICodexTickets(context.Background())
	require.Empty(t, upstream.requests)
}

type codexTicketContextBody struct {
	ctx     context.Context
	started chan struct{}
	closed  atomic.Bool
}

func (b *codexTicketContextBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *codexTicketContextBody) Close() error { b.closed.Store(true); return nil }

func TestCodexTicket332StopCancelsStreamRead(t *testing.T) {
	started := make(chan struct{})
	var body *codexTicketContextBody
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		body = &codexTicketContextBody{ctx: req.Context(), started: started}
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(332))
		return &http.Response{StatusCode: 200, Header: h, Body: body}, nil
	}}
	svc := dualTicketTestService(t, upstream)
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	account := ticketTestAccount(41)
	account.Status = StatusActive
	account.Extra = map[string]any{"codex_ticket_mode": "332"}
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("332 did not begin reading the stream")
	}
	stopped := make(chan struct{})
	go func() { svc.StopOpenAICodexTicketHarvester(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel the 332 stream")
	}
	require.True(t, body.closed.Load())
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicket332OnlyLoopUsesItsOwnInterval(t *testing.T) {
	started := make(chan struct{}, 4)
	upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		started <- struct{}{}
		return &http.Response{StatusCode: 503, Body: http.NoBody}, nil
	}}
	svc := dualTicketTestService(t, upstream)
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProbeIntervalSeconds = 60
	svc.cfg.Gateway.OpenAICodexTicket332.HarvestProbeIntervalSeconds = 1
	account := ticketTestAccount(41)
	account.Status = StatusActive
	account.Extra = map[string]any{"codex_ticket_mode": "332"}
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("332 loop did not use its configured interval")
		}
	}
	svc.StopOpenAICodexTicketHarvester()
}

func TestCodexTicketRefreshDisabledSkipsDBAndEnabledBoundsRead(t *testing.T) {
	var calls int
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
	svc.accountRepo = &codexTicketLifecycleRepo{list: func(ctx context.Context) ([]Account, error) {
		calls++
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
		return nil, nil
	}}
	svc.refreshOpenAICodexTickets(context.Background())
	require.Zero(t, calls)
	svc.cfg.Gateway.OpenAICodexTicket332.Enabled = true
	svc.refreshOpenAICodexTickets(context.Background())
	require.Equal(t, 1, calls)
}

type codexTicketTokenErrorRepo struct {
	AccountRepository
	account          Account
	started, release chan struct{}
}

func (r *codexTicketTokenErrorRepo) ListByPlatform(context.Context, string) ([]Account, error) {
	return []Account{r.account}, nil
}
func (r *codexTicketTokenErrorRepo) SetError(ctx context.Context, _ int64, _ string) error {
	close(r.started)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestCodexTicketHarvesterStopCancelsExpiredTokenError(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	account.Credentials["expires_at"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	repo := &codexTicketTokenErrorRepo{account: *account, started: make(chan struct{}), release: make(chan struct{})}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}, HarvestProxyURL: "http://292.example"}, &httpUpstreamRecorder{})
	svc.accountRepo = repo
	svc.openAITokenProvider = NewOpenAITokenProvider(repo, nil, nil)
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(func() { close(repo.release); svc.StopOpenAICodexTicketHarvester() })
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("token error persistence did not start")
	}
	stopped := make(chan struct{})
	go func() { svc.StopOpenAICodexTicketHarvester(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("token error persistence ignored harvester cancellation")
	}
}
