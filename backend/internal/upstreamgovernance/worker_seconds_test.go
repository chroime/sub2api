package upstreamgovernance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fastMemoryStore struct {
	*memoryStore
	fastDue  []Site
	reserved []int64
	results  []FastObservationResult
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
func (m *fastMemoryStore) ObserveFastResult(_ context.Context, _ int64, observed, _ time.Time, _, _ string, _ GroupObservation) (FastObservationResult, error) {
	result := FastObservationResult{Changed: false, Revision: 1, ObservedAt: observed}
	m.results = append(m.results, result)
	return result, nil
}
func (m *fastMemoryStore) LatestCatalogRevision(context.Context, int64) (*CatalogObservation, error) {
	return nil, ErrNotFound
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
