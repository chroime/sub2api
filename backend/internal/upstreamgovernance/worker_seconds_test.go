package upstreamgovernance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fastMemoryStore struct {
	*memoryStore
	fastDue     []Site
	reserved    []int64
	results     []FastObservationResult
	saveCtxErr  error
	savedStatus string
	savedError  string
	nextAt      time.Time
	previous    *CatalogObservation
	result      FastObservationResult
	callOrder   []string
}

func (m *fastMemoryStore) DueFastObservations(context.Context, time.Time, int) ([]Site, error) {
	return append([]Site(nil), m.fastDue...), nil
}
func (m *fastMemoryStore) NextFastObservationAt(context.Context) (*time.Time, error) {
	if len(m.fastDue) == 0 {
		return nil, nil
	}
	value := m.fastDue[0].NextFastObserveAt
	return &value, nil
}
func (m *fastMemoryStore) ReserveFastObservation(_ context.Context, id int64, _, _ time.Time) (bool, error) {
	m.reserved = append(m.reserved, id)
	return true, nil
}

func (m *fastMemoryStore) ObserveFastResult(ctx context.Context, _ int64, observed, next time.Time, status, code string, _ GroupObservation) (FastObservationResult, error) {
	m.saveCtxErr = ctx.Err()
	m.nextAt, m.savedStatus, m.savedError = next, status, code
	m.callOrder = append(m.callOrder, "observe")
	result := m.result
	if result.ObservedAt.IsZero() {
		result = FastObservationResult{Changed: false, Revision: 1, ObservedAt: observed}
	}
	m.results = append(m.results, result)
	return result, nil
}

type errorFastConnector struct {
	*fakeConnector
	err     error
	observe func(context.Context) error
}

func (c *errorFastConnector) ObserveGroups(ctx context.Context, _ Site, _ Session) (GroupObservation, error) {
	if c.observe != nil {
		return GroupObservation{}, c.observe(ctx)
	}
	return GroupObservation{}, c.err
}

func fastFailureFixture(t *testing.T, connector Connector) (*Service, *fastMemoryStore) {
	t.Helper()
	raw, _ := json.Marshal(Session{AccessToken: "fixture", UserID: 5})
	base := &memoryStore{site: Site{ID: 7, Enabled: true, FastObserveEnabled: true, FastIntervalSeconds: 5, SessionCipher: base64.StdEncoding.EncodeToString(raw), NextSyncAt: time.Now().Add(time.Hour)}}
	store := &fastMemoryStore{memoryStore: base, fastDue: []Site{base.site}}
	return NewService(store, connector, nil, fakeCipher{}, true), store
}

func TestWorkerFastObservationHonorsWrappedRateLimit(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &RateLimitError{StatusCode: 429, RetryAfter: 120 * time.Second})
	s, store := fastFailureFixture(t, &errorFastConnector{fakeConnector: &fakeConnector{}, err: err})
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	require.ErrorIs(t, s.runFastDue(t.Context()), err)
	require.Equal(t, "rate_limited", store.savedStatus)
	require.Equal(t, now.Add(120*time.Second), store.nextAt)
}

func TestWorkerFastObservationMissingCapabilityPreservesError(t *testing.T) {
	s, store := fastFailureFixture(t, &fakeConnector{})
	require.NoError(t, s.runFastDue(t.Context()))
	require.Equal(t, "error", store.savedStatus)
	require.Equal(t, "unsupported_contract", store.savedError)
}

func TestWorkerFastObservationBoundsEachRequest(t *testing.T) {
	var deadline time.Time
	var bounded bool
	s, store := fastFailureFixture(t, &errorFastConnector{fakeConnector: &fakeConnector{}, observe: func(ctx context.Context) error {
		deadline, bounded = ctx.Deadline()
		return context.DeadlineExceeded
	}})
	started := time.Now()
	err := s.runFastDue(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, bounded, "each network request must have its own deadline")
	require.GreaterOrEqual(t, deadline, started.Add(9*time.Second))
	require.LessOrEqual(t, deadline, time.Now().Add(10*time.Second))
	require.Equal(t, "timeout", store.savedError)
	require.NoError(t, store.saveCtxErr)
}

func TestWorkerFastFailuresBackOffConsecutivelyAndResetOnSuccess(t *testing.T) {
	c := &errorFastConnector{fakeConnector: &fakeConnector{}, err: errors.New("temporary")}
	s, store := fastFailureFixture(t, c)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	require.Error(t, s.runFastDue(t.Context()))
	require.GreaterOrEqual(t, store.nextAt, now.Add(10*time.Second))
	require.Error(t, s.runFastDue(t.Context()))
	require.GreaterOrEqual(t, store.nextAt, now.Add(20*time.Second))
	c.err = nil
	require.NoError(t, s.runFastDue(t.Context()))
	require.Less(t, store.nextAt, now.Add(6*time.Second))
}

type cancelWhileReservingStore struct {
	*fastMemoryStore
	started <-chan struct{}
	cancel  context.CancelFunc
}

func (m *cancelWhileReservingStore) ReserveFastObservation(ctx context.Context, id int64, due, now time.Time) (bool, error) {
	if id == 7 {
		return true, nil
	}
	<-m.started
	m.cancel()
	return false, errors.New("reservation failed")
}

func TestWorkerFastCancellationWaitsForStartedTasks(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &errorFastConnector{fakeConnector: &fakeConnector{}, observe: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		close(finished)
		return ctx.Err()
	}}
	s, base := fastFailureFixture(t, c)
	base.fastDue = append(base.fastDue, Site{ID: 8}, Site{ID: 9})
	s.store = &cancelWhileReservingStore{fastMemoryStore: base, started: started, cancel: cancel}
	require.ErrorIs(t, s.runFastDue(ctx), context.Canceled)
	select {
	case <-finished:
	default:
		t.Fatal("worker returned before its started task finished")
	}
	require.Len(t, base.results, 1)
	require.NoError(t, base.saveCtxErr)
}
func (m *fastMemoryStore) LatestCatalogRevision(context.Context, int64) (*CatalogObservation, error) {
	m.callOrder = append(m.callOrder, "latest")
	if m.previous == nil {
		return nil, ErrNotFound
	}
	return m.previous, nil
}

type fastConnector struct {
	*fakeConnector
	groups GroupObservation
}

func (c *fastConnector) ObserveGroups(context.Context, Site, Session) (GroupObservation, error) {
	return c.groups, nil
}

type unsupportedFastConnector struct{ *fakeConnector }

func (c *unsupportedFastConnector) ObserveGroups(context.Context, Site, Session) (GroupObservation, error) {
	return GroupObservation{}, ErrUnsupported
}

type cancelFastConnector struct {
	*fastConnector
	cancel context.CancelFunc
}

func (c *cancelFastConnector) ObserveGroups(ctx context.Context, site Site, session Session) (GroupObservation, error) {
	c.cancel()
	return GroupObservation{}, ctx.Err()
}

func TestWorkerRunsOneSecondFastObservationWithoutMinuteTicker(t *testing.T) {
	raw, _ := json.Marshal(Session{AccessToken: "fixture", UserID: 5})
	base := &memoryStore{site: Site{ID: 7, Enabled: true, SessionCipher: base64.StdEncoding.EncodeToString(raw), NextSyncAt: time.Now().Add(time.Hour)}}
	store := &fastMemoryStore{memoryStore: base, fastDue: []Site{base.site}}
	connector := &fastConnector{fakeConnector: &fakeConnector{}, groups: GroupObservation{GroupsComplete: true, SourceUserID: 5, Groups: []RemoteGroup{{ID: "7", Name: "VIP"}}}}
	service := NewService(store, connector, nil, fakeCipher{}, true)
	service.now = func() time.Time { return time.Now() }
	require.NoError(t, service.runFastDue(context.Background()))
	require.Equal(t, []int64{7}, store.reserved)
	require.Len(t, store.results, 1)
}

func TestWorkerTreatsUnsupportedFastObservationAsAVisibleNonFatalStatus(t *testing.T) {
	raw, _ := json.Marshal(Session{AccessToken: "fixture", UserID: 5})
	base := &memoryStore{site: Site{ID: 8, Enabled: true, SessionCipher: base64.StdEncoding.EncodeToString(raw), NextSyncAt: time.Now().Add(time.Hour)}}
	store := &fastMemoryStore{memoryStore: base, fastDue: []Site{base.site}}
	service := NewService(store, &unsupportedFastConnector{fakeConnector: &fakeConnector{}}, nil, fakeCipher{}, true)
	require.NoError(t, service.runFastDue(context.Background()))
	require.Equal(t, []int64{8}, store.reserved)
	require.Len(t, store.results, 1)
}

func TestWorkerReadsPreviousFastCatalogBeforePersistingChangedObservation(t *testing.T) {
	raw, _ := json.Marshal(Session{AccessToken: "fixture", UserID: 5})
	base := &memoryStore{site: Site{ID: 9, Enabled: true, Platform: "sub2api", SessionCipher: base64.StdEncoding.EncodeToString(raw), NextSyncAt: time.Now().Add(time.Hour)}}
	before, after := 0.8, 0.9
	store := &fastMemoryStore{
		memoryStore: base,
		fastDue:     []Site{base.site},
		previous:    &CatalogObservation{SiteID: 9, Revision: 1, GroupsComplete: true, Groups: []RemoteGroup{{ID: "claude", Name: "Claude Max", ResolvedRateMultiplier: &before}}},
		result:      FastObservationResult{Changed: true, Revision: 2, ObservedAt: time.Now().UTC()},
	}
	connector := &fastConnector{fakeConnector: &fakeConnector{}, groups: GroupObservation{GroupsComplete: true, SourceUserID: 5, Groups: []RemoteGroup{{ID: "claude", Name: "Claude Max", ResolvedRateMultiplier: &after}}}}
	service := NewService(store, connector, nil, fakeCipher{}, true)

	require.NoError(t, service.runFastDue(context.Background()))
	require.Equal(t, []string{"latest", "observe"}, store.callOrder)
}

func TestWorkerPersistsFastFailureAfterRequestCancellation(t *testing.T) {
	raw, _ := json.Marshal(Session{AccessToken: "fixture", UserID: 5})
	base := &memoryStore{site: Site{ID: 10, Enabled: true, SessionCipher: base64.StdEncoding.EncodeToString(raw), NextSyncAt: time.Now().Add(time.Hour)}}
	store := &fastMemoryStore{memoryStore: base, fastDue: []Site{base.site}}
	ctx, cancel := context.WithCancel(context.Background())
	connector := &cancelFastConnector{
		fastConnector: &fastConnector{fakeConnector: &fakeConnector{}, groups: GroupObservation{GroupsComplete: true, SourceUserID: 5}},
		cancel:        cancel,
	}
	service := NewService(store, connector, nil, fakeCipher{}, true)

	err := service.runFastDue(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, store.results, 1, "the terminal error should be persisted before the worker exits")
	require.NoError(t, store.saveCtxErr, "persistence must use a short independent context after cancellation")
}

func TestWorkerClearsFastReservationWhenWaitingForSlotIsCancelled(t *testing.T) {
	raw, _ := json.Marshal(Session{AccessToken: "fixture", UserID: 5})
	base := &memoryStore{site: Site{ID: 11, Enabled: true, SessionCipher: base64.StdEncoding.EncodeToString(raw), NextSyncAt: time.Now().Add(time.Hour)}}
	store := &fastMemoryStore{memoryStore: base, fastDue: []Site{base.site}}
	service := NewService(store, &fastConnector{fakeConnector: &fakeConnector{}}, nil, fakeCipher{}, true)
	service.slots = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.runFastSite(ctx, base.site.ID)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, store.results, 1, "a reserved task must clear its reservation when slot acquisition is cancelled")
	require.NoError(t, store.saveCtxErr)
}
