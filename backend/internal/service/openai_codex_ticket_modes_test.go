package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketModeSelectionDoesNotFallBackTo292(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	for _, tc := range []struct {
		name    string
		extra   map[string]any
		blocked bool
	}{
		{"missing mode keeps official policy", nil, true},
		{"explicit 292", map[string]any{"codex_ticket_mode": "292"}, true},
		{"332 is independently disabled", map[string]any{"codex_ticket_mode": "332"}, false},
		{"off", map[string]any{"codex_ticket_mode": "off"}, false},
		{"unknown", map[string]any{"codex_ticket_mode": "356"}, false},
		{"empty explicit", map[string]any{"codex_ticket_mode": ""}, false},
		{"non string", map[string]any{"codex_ticket_mode": 292}, false},
		{"whitespace is explicit invalid", map[string]any{"codex_ticket_mode": " 292 "}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := ticketTestAccount(41)
			account.Extra = tc.extra
			account.Credentials["plan_type"] = "team"
			require.Equal(t, tc.blocked, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
			headers := http.Header{openAICodexTurnStateHeader: []string{"client-state"}}
			err := svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", headers)
			if tc.blocked {
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCodexTicketPersistenceUsesSelectedModeNamespace(t *testing.T) {
	repo := &codexTicketRefreshRepo{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	svc.accountRepo = repo
	svc.storeOpenAICodexTicket(context.Background(), ticketTestAccount(41), &openAICodexTicket{
		Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})
	require.Contains(t, repo.updates, "codex_turn_ticket:292:gpt-6-astra")
	require.NotContains(t, repo.updates, "codex_turn_ticket:gpt-6-astra")
}

func TestCodexTicketLegacyRequiresRecordedAndActualLength(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	for _, recordedLength := range []int{0, 312, 332, 356} {
		account := ticketTestAccount(int64(recordedLength + 41))
		account.Extra = map[string]any{"codex_turn_ticket:gpt-6-astra": map[string]any{
			"state": fakeCodexTicketState(292), "length": recordedLength,
			"expires_at": time.Now().Add(time.Hour),
		}}
		require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"), "recorded length %d", recordedLength)
	}
}

func TestCodexTicketNamedModeRejectsConfigured312And356(t *testing.T) {
	for _, invalidLength := range []int{312, 356} {
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, TargetLength: invalidLength, FailClosed: true}, nil)
		account := ticketTestAccount(41)
		account.Extra = map[string]any{"codex_turn_ticket:gpt-6-astra": map[string]any{
			"state": fakeCodexTicketState(invalidLength), "length": invalidLength,
			"expires_at": time.Now().Add(time.Hour),
		}}
		require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
	}
}

func TestCodexTicketStatusIncludesSelectedMode(t *testing.T) {
	status := OpenAICodexTicketStatuses(ticketTestAccount(41), config.OpenAICodexTicketConfig{Enabled: true}, time.Now())
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"mode":"292"`)
}

func dualTicketTestService(t *testing.T, upstream HTTPUpstream) *OpenAIGatewayService {
	t.Helper()
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
	svc.cfg.Gateway.OpenAICodexTicket332 = config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, HarvestProxyURL: "http://332.example:8080", Models: []string{"gpt-6-astra"},
	}
	return svc
}

func TestCodexTicketSwitchModesKeepsSeparateTickets(t *testing.T) {
	svc := dualTicketTestService(t, nil)
	repo := &codexTicketRefreshRepo{}
	svc.accountRepo = repo
	account := ticketTestAccount(41)
	for _, length := range []int{292, 332} {
		mode := "292"
		if length == 332 {
			mode = "332"
		}
		account.Extra = map[string]any{"codex_ticket_mode": mode}
		require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
		svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
			Model: "gpt-6-astra", State: fakeCodexTicketState(length), Length: length,
			CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		})
	}
	for _, mode := range []string{"292", "332", "292"} {
		account.Extra["codex_ticket_mode"] = mode
		headers := http.Header{}
		require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", headers))
		length := 292
		if mode == "332" {
			length = 332
		}
		require.Len(t, headers.Get(openAICodexTurnStateHeader), length)
		other := ticketTestAccount(42)
		other.Extra = map[string]any{"codex_ticket_mode": mode}
		require.True(t, svc.openAICodexTicketBlocksAccount(other, "gpt-6-astra"))
		require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-5.6-sol"))
	}
	require.Contains(t, repo.updates, "codex_turn_ticket:292:gpt-6-astra")
	require.Contains(t, repo.updates, "codex_turn_ticket:332:gpt-6-astra")
}

func TestCodexTicket332HarvestRequiresCompletedStream(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		accepted   bool
	}{
		{"completed", "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n", true},
		{"empty", "", false},
		{"truncated", "data: {\"type\":\"response.output_text.delta\"}\n\n", false},
		{"failed before completion", "data: {\"type\":\"response.failed\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n", false},
		{"failed after completion", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\ndata: {\"type\":\"error\"}\n\n", false},
		{"incomplete", "data: {\"type\":\"response.incomplete\"}\n\n", false},
		{"done with failure", "data: {\"type\":\"response.done\",\"response\":{\"status\":\"failed\"}}\n\n", false},
		{"event failure overrides payload", "event: response.failed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n", false},
		{"event incomplete overrides payload", "event: response.incomplete\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n", false},
		{"event error overrides payload", "event: error\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				h := http.Header{}
				h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(332))
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}}
			svc := dualTicketTestService(t, upstream)
			account := ticketTestAccount(41)
			account.Extra = map[string]any{"codex_ticket_mode": "332"}
			_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), account, "fixture", "gpt-6-astra", "", time.Second)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, tc.accepted, svc.lookupOpenAICodexTicket(account, "gpt-6-astra") != nil)
		})
	}
}

func TestCodexTicket332OnlyHarvestResumesAfterRateLimit(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"X-Codex-Turn-State": []string{fakeCodexTicketState(332)}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n"))}}}
	svc := dualTicketTestService(t, upstream)
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	reset := time.Now().Add(time.Hour)
	account := ticketTestAccount(41)
	account.Status = StatusActive
	account.RateLimitResetAt = &reset
	account.Extra = map[string]any{"codex_ticket_mode": "332"}
	repo := &codexTicketRefreshRepo{accounts: []Account{*account}}
	svc.accountRepo = repo
	svc.refreshOpenAICodexTickets(context.Background())
	require.Empty(t, upstream.requests)
	repo.accounts[0].RateLimitResetAt = nil
	svc.refreshOpenAICodexTickets(context.Background())
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "http://332.example:8080", upstream.lastProxyURL)
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicket332SchedulingPrefersReadyUsingEachOutboundModel(t *testing.T) {
	for _, path := range []string{"load", "load failure", "wait"} {
		t.Run(path, func(t *testing.T) {
			accounts := []Account{*ticketTestAccount(41), *ticketTestAccount(42)}
			for i := range accounts {
				accounts[i].Status = StatusActive
				accounts[i].Schedulable = true
				accounts[i].Concurrency = 1
				accounts[i].Extra = map[string]any{"codex_ticket_mode": "332"}
			}
			accounts[0].Credentials["model_mapping"] = map[string]any{"custom-model": "gpt-6-astra"}
			accounts[1].Credentials["model_mapping"] = map[string]any{"custom-model": "gpt-5.6-sol"}
			now := time.Now()
			accounts[1].LastUsedAt = &now // ordinarily account 41 wins LRU and load
			svc := dualTicketTestService(t, nil)
			svc.cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc.cfg.Gateway.OpenAICodexTicket332.FailClosed = false
			svc.cfg.Gateway.OpenAICodexTicket332.Models = []string{"gpt-6-astra", "gpt-5.6-sol"}
			svc.cache = &stubGatewayCache{}
			concurrencyCache := stubConcurrencyCache{loadMap: map[int64]*AccountLoadInfo{
				41: {AccountID: 41, LoadRate: 10}, 42: {AccountID: 42, LoadRate: 80},
			}}
			if path == "load failure" {
				concurrencyCache.loadBatchErr = errors.New("fixture load error")
			}
			if path == "wait" {
				concurrencyCache.acquireResults = map[int64]bool{41: false, 42: false}
			}
			svc.concurrencyService = NewConcurrencyService(concurrencyCache)
			svc.storeOpenAICodexTicket(context.Background(), &accounts[1], &openAICodexTicket{
				Model: "gpt-5.6-sol", State: fakeCodexTicketState(332), Length: 332, CapturedAt: now, ExpiresAt: now.Add(time.Hour),
			})
			svc.accountRepo = stubOpenAIAccountRepo{accounts: accounts}
			selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "custom-model", nil)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, int64(42), selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestCodexTicket332PreferenceKeepsOtherModesAndPrioritiesInPlace(t *testing.T) {
	svc := dualTicketTestService(t, nil)
	accounts := []*Account{ticketTestAccount(41), ticketTestAccount(42), ticketTestAccount(43), ticketTestAccount(44)}
	for _, account := range accounts {
		account.Extra = map[string]any{"codex_ticket_mode": "332"}
	}
	accounts[1].Extra["codex_ticket_mode"] = "292"
	accounts[3].Priority = 1
	for _, account := range accounts[2:] {
		svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
			Model: "gpt-6-astra", State: fakeCodexTicketState(332), Length: 332, ExpiresAt: time.Now().Add(time.Hour),
		})
	}
	svc.prioritizeOpenAICodex332Tickets(accounts, "gpt-6-astra", false)
	require.Equal(t, []int64{43, 42, 41, 44}, []int64{accounts[0].ID, accounts[1].ID, accounts[2].ID, accounts[3].ID})
}

func TestCodexTicketConcurrentModesUseSeparateFlights(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		mode := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		started <- mode
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		length := 292
		if mode == "332" {
			length = 332
		}
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, fakeCodexTicketState(length))
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n"))}, nil
	}}
	svc := dualTicketTestService(t, upstream)
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://292.example:8080"
	accounts := []*Account{ticketTestAccount(41), ticketTestAccount(41)}
	var wg sync.WaitGroup
	for i, mode := range []string{"292", "332"} {
		accounts[i].Extra = map[string]any{"codex_ticket_mode": mode}
		accounts[i].Credentials["access_token"] = mode
		wg.Add(1)
		go func(account *Account) {
			defer wg.Done()
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
		}(accounts[i])
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case mode := <-started:
			seen[mode] = true
		case <-time.After(time.Second):
		}
	}
	close(release)
	wg.Wait()
	require.Equal(t, map[string]bool{"292": true, "332": true}, seen)
	for i, length := range []int{292, 332} {
		ticket := svc.lookupOpenAICodexTicket(accounts[i], "gpt-6-astra")
		require.NotNil(t, ticket)
		require.Equal(t, length, ticket.Length)
	}
}

func TestCodexTicketInFlightModeChangePersistsOriginalNamespace(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return codexTicketResponse(), nil
	}}
	svc := dualTicketTestService(t, upstream)
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://292.example:8080"
	repo := &codexTicketRefreshRepo{}
	svc.accountRepo = repo
	account := ticketTestAccount(41)
	account.Extra = map[string]any{"codex_ticket_mode": "292"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe not started")
	}
	account.Extra["codex_ticket_mode"] = "332"
	close(release)
	<-done
	require.Contains(t, repo.updates, "codex_turn_ticket:292:gpt-6-astra")
	require.NotContains(t, repo.updates, "codex_turn_ticket:332:gpt-6-astra")
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketRefreshHydratesPersistedTicketsBeforeAnyProbe(t *testing.T) {
	accounts := []Account{*ticketTestAccount(41), *ticketTestAccount(42)}
	for i := range accounts {
		accounts[i].Status = StatusActive
	}
	accounts[1].Extra = map[string]any{"codex_turn_ticket:292:gpt-6-astra": boundCodexTicketFixture(&accounts[1], "gpt-6-astra", 292)}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://292.example", Models: []string{"gpt-6-astra"}}, nil)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: accounts}
	metadata := ticketTestAccount(42)
	var alreadyHydrated bool
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		alreadyHydrated = !svc.openAICodexTicketBlocksAccount(metadata, "gpt-6-astra")
		return &http.Response{StatusCode: 503, Body: http.NoBody}, nil
	}}
	svc.refreshOpenAICodexTickets(context.Background())
	require.True(t, alreadyHydrated)
	require.False(t, svc.openAICodexTicketBlocksAccount(metadata, "gpt-6-astra"))
}

func TestCodexTicketLegacyHydrationNeverCrossesModes(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		for _, length := range []int{292, 312, 332, 356} {
			svc := dualTicketTestService(t, nil)
			account := ticketTestAccount(41)
			account.Extra = map[string]any{"codex_ticket_mode": mode, "codex_turn_ticket:gpt-6-astra": map[string]any{
				"state": fakeCodexTicketState(length), "length": length, "expires_at": time.Now().Add(time.Hour),
			}}
			require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"), "unbound legacy requires recapture: mode %s length %d", mode, length)
		}
	}
}

func TestCodexTicketRejectsMalformedPersistedIdentityAndState(t *testing.T) {
	for _, change := range []map[string]any{
		{"account_id": 42},
		{"model": "gpt-5.6-sol"},
		{"state": " " + fakeCodexTicketState(292)},
		{"state": fakeCodexTicketState(292) + " "},
		{"state": strings.Repeat("X", 292)},
		{"expires_at": time.Time{}},
	} {
		svc := dualTicketTestService(t, nil)
		stored := map[string]any{"account_id": 41, "model": "gpt-6-astra", "state": fakeCodexTicketState(292), "length": 292, "expires_at": time.Now().Add(time.Hour)}
		for key, value := range change {
			stored[key] = value
		}
		account := ticketTestAccount(41)
		account.Extra = map[string]any{"codex_turn_ticket:292:gpt-6-astra": stored}
		require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"), "must reject malformed record %v", change)
	}
}
