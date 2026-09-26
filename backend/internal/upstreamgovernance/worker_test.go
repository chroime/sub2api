package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (m *memoryStore) DueSites(context.Context, time.Time, int) ([]Site, error) {
	// Return a stale selection too: workers must recheck state after locking.
	return []Site{m.site}, nil
}
func (m *memoryStore) LatestCheck(_ context.Context, siteID, bindingID int64) (*Check, error) {
	for i := len(m.checks) - 1; i >= 0; i-- {
		if m.checks[i].SiteID == siteID && m.checks[i].BindingID == bindingID {
			c := m.checks[i]
			return &c, nil
		}
	}
	return nil, ErrNotFound
}
func importedEngine(t *testing.T) (*Service, *memoryStore, *fakeConnector, *fakeLocal) {
	t.Helper()
	s, m, c, l := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	r, e := s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Equal(t, "applied", r.Items[0].Status)
	return s, m, c, l
}

func TestAutomaticDiscoveryCannotEnableBillableProbes(t *testing.T) {
	s, m, c, l := importedEngine(t)
	m.site.NextSyncAt = s.now().Add(-time.Minute)
	before := l.calls
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 2, c.discoveryCalls)
	require.Zero(t, c.probeCalls)
	require.Equal(t, before, l.calls)
	require.False(t, m.bindings[0].ProbeEnabled)
}

func TestMonitorValidatesBindingAndRequiresExplicitModelAndCadence(t *testing.T) {
	s, m, c, _ := importedEngine(t)
	for _, input := range []struct {
		model    string
		interval int
	}{{"", 30}, {"m\nsecret", 30}, {"m", 1}, {"m", 1441}} {
		_, e := s.ConfigureMonitor(t.Context(), 1, m.bindings[0].ID, true, input.model, input.interval)
		require.ErrorIs(t, e, ErrInvalid)
	}
	_, e := s.ConfigureMonitor(t.Context(), 1, 999, true, "gpt-fixture", 30)
	require.ErrorIs(t, e, ErrNotFound)
	b, e := s.ConfigureMonitor(t.Context(), 1, m.bindings[0].ID, true, "gpt-fixture", 0)
	require.NoError(t, e)
	require.True(t, b.ProbeEnabled)
	require.Equal(t, 30, b.ProbeIntervalMinutes)
	require.Zero(t, c.probeCalls, "saving the opt-in must not itself bill a request")
	b, e = s.ConfigureMonitor(t.Context(), 1, b.ID, false, "", 0)
	require.NoError(t, e)
	require.False(t, b.ProbeEnabled)
}

func TestProbeUsesStoredInferenceKeyAndRecordsOnlySanitizedOutcomes(t *testing.T) {
	s, m, c, l := importedEngine(t)
	m.site.SessionCipher = "" // inference checks do not need dashboard authorization
	before := l.calls
	result, e := s.Probe(t.Context(), 1, m.bindings[0].ID, "gpt-fixture")
	require.NoError(t, e)
	require.True(t, result.Success)
	c.err = errors.New("upstream fixture-secret body")
	c.probeResult = &ProbeResult{Success: true, ErrorCode: "fixture-secret body", LatencyMS: -1}
	result, e = s.Probe(t.Context(), 1, m.bindings[0].ID, "gpt-fixture")
	require.NoError(t, e, "a completed failed check is still recorded and returned")
	require.False(t, result.Success)
	require.Equal(t, "operation_failed", result.ErrorCode)
	require.GreaterOrEqual(t, result.LatencyMS, int64(0))
	require.Len(t, m.checks, 2)
	raw, _ := json.Marshal(m.checks)
	require.NotContains(t, string(raw), "fixture-secret")
	require.Equal(t, before, l.calls)
	require.True(t, m.bindings[0].NextProbeAt.After(s.now()))
}

func TestProbeEventsOnlyOnFailureAndRecoveryTransitions(t *testing.T) {
	s, m, c, _ := importedEngine(t)
	c.err = ErrReauth
	for range 2 {
		_, e := s.Probe(t.Context(), 1, m.bindings[0].ID, "gpt-fixture")
		require.NoError(t, e)
	}
	c.err = nil
	for range 2 {
		_, e := s.Probe(t.Context(), 1, m.bindings[0].ID, "gpt-fixture")
		require.NoError(t, e)
	}
	kinds := []string{}
	for _, e := range m.events {
		if e.Kind == "probe_failed" || e.Kind == "probe_recovered" {
			kinds = append(kinds, e.Kind)
		}
	}
	require.Equal(t, []string{"probe_failed", "probe_recovered"}, kinds)
}

func TestWorkerRechecksDueTimeAndDisabledSiteAfterLock(t *testing.T) {
	s, m, c, _ := importedEngine(t)
	_, e := s.ConfigureMonitor(t.Context(), 1, m.bindings[0].ID, true, "gpt-fixture", 15)
	require.NoError(t, e)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, c.probeCalls)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, c.probeCalls, "stale due-list must not reissue a paid probe")
	m.bindings[0].NextProbeAt = s.now().Add(-time.Minute)
	m.site.Enabled = false
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, c.probeCalls)
	m.site.Enabled = true
	m.locked = true
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, c.probeCalls)
}

func TestWorkerBoundsPerSiteWorkAndMissingAccountNeverCallsUpstream(t *testing.T) {
	s, m, c, l := importedEngine(t)
	base := m.bindings[0]
	m.bindings = nil
	for i := int64(1); i <= 12; i++ {
		b := base
		b.ID = i
		b.Marker = fmt.Sprintf("fixture-%d", i)
		b.ProbeEnabled = true
		b.ProbeModel = "gpt-fixture"
		m.bindings = append(m.bindings, b)
		l.accounts[b.Marker] = &LocalAccount{ID: base.AccountID}
	}
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 5, c.probeCalls)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 10, c.probeCalls)
	delete(l.accounts, m.bindings[10].Marker)
	result, e := s.Probe(t.Context(), 1, 11, "gpt-fixture")
	require.NoError(t, e)
	require.False(t, result.Success)
	require.Equal(t, "local_account_missing", result.ErrorCode)
	require.Equal(t, 10, c.probeCalls)
}

type cancelConnector struct {
	Connector
	started chan struct{}
	stopped chan struct{}
}

type cancelDuringDiscovery struct {
	Connector
	cancel context.CancelFunc
}

func (c cancelDuringDiscovery) Discover(ctx context.Context, _ Site, _ Session) (Catalog, error) {
	c.cancel()
	return Catalog{}, ctx.Err()
}
func TestCancelledDiscoveryCannotMonopolizeFutureWorkerBatches(t *testing.T) {
	s, m, c, _ := setupEngine(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.connector = cancelDuringDiscovery{Connector: c, cancel: cancel}
	require.ErrorIs(t, s.runDue(ctx), context.Canceled)
	require.True(t, m.site.NextSyncAt.After(s.now()), "reserve next sync before external work, even if recording its outcome is cancelled")
}

func (c *cancelConnector) Discover(ctx context.Context, _ Site, _ Session) (Catalog, error) {
	close(c.started)
	<-ctx.Done()
	close(c.stopped)
	return Catalog{}, ctx.Err()
}
func TestWorkerStopCancelsInflightRequestsAndStartIsIdempotent(t *testing.T) {
	s, _, _, _ := setupEngine(t)
	c := &cancelConnector{started: make(chan struct{}), stopped: make(chan struct{})}
	s.connector = c
	s.Start()
	s.Start()
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop failed to cancel an in-flight request")
	}
	select {
	case <-c.stopped:
	default:
		t.Fatal("Stop returned before request finished")
	}
	s.Stop()
}
