package upstreamgovernance

import (
	"context"
	"sort"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type sessionScheduleStore struct {
	*memoryStore
	renewals map[int64]*runtimeMemoryStore
}

func (m *sessionScheduleStore) ListSites(ctx context.Context) ([]Site, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sites := []Site{m.site}
	for _, store := range m.renewals {
		sites = append(sites, store.site)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	if len(sites) > 1000 {
		sites = sites[:1000]
	}
	return sites, nil
}

func (m *sessionScheduleStore) ListSitesAfter(ctx context.Context, afterID int64, limit int) ([]Site, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sites := []Site{m.site}
	for _, store := range m.renewals {
		sites = append(sites, store.site)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	start := sort.Search(len(sites), func(i int) bool { return sites[i].ID > afterID })
	end := min(len(sites), start+limit)
	return sites[start:end], nil
}

func (m *sessionScheduleStore) GetSite(ctx context.Context, id int64) (*Site, error) {
	if store := m.renewals[id]; store != nil {
		return store.GetSite(ctx, id)
	}
	return m.memoryStore.GetSite(ctx, id)
}

func (m *sessionScheduleStore) LockSite(ctx context.Context, id int64) (func(), bool, error) {
	if store := m.renewals[id]; store != nil {
		return store.LockSite(ctx, id)
	}
	return m.memoryStore.LockSite(ctx, id)
}

func (m *sessionScheduleStore) SaveRuntimeSession(ctx context.Context, id, version int64, previous, encrypted string) error {
	if store := m.renewals[id]; store != nil {
		return store.SaveRuntimeSession(ctx, id, version, previous, encrypted)
	}
	return ErrNotFound
}

func (m *sessionScheduleStore) DueSites(ctx context.Context, _ time.Time, _ int) ([]Site, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []Site{m.site}, nil
}

func sessionScheduleEngine(t *testing.T, count int) (*Service, *sessionScheduleStore, *lifecycleConnector) {
	t.Helper()
	s, memory, base, _ := setupEngine(t)
	memory.site.ID = int64(count + 1)
	store := &sessionScheduleStore{memoryStore: memory, renewals: map[int64]*runtimeMemoryStore{}}
	expires := s.now().Add(time.Hour)
	for id := int64(1); id <= int64(count); id++ {
		site := memory.site
		site.ID = id
		site.NextSyncAt = s.now().Add(time.Hour)
		renewal := &memoryStore{site: site}
		setSessionFixture(t, renewal, Session{AccessToken: "fixture-rotated-access", RefreshToken: "fixture-rotated-refresh", UserID: 5, UserAgent: "FixtureBrowser/1", ExpiresAt: &expires, RefreshState: "identity_pending"})
		store.renewals[id] = &runtimeMemoryStore{memoryStore: renewal}
	}
	connector := &lifecycleConnector{fakeConnector: base, refresh: func(context.Context, Site, Session) (Session, error) {
		t.Fatal("identity recovery must not repeat token rotation")
		return Session{}, ErrReauth
	}}
	s.store, s.connector = store, connector
	return s, store, connector
}

func TestScheduledSessionRefreshTimeoutLeavesCollectionBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, store, connector := sessionScheduleEngine(t, 1)
		connector.verify = func(ctx context.Context, _ Site, _ Session) (Session, error) {
			<-ctx.Done()
			return Session{}, ctx.Err()
		}
		started := time.Now()
		require.ErrorIs(t, s.runDue(t.Context()), context.DeadlineExceeded)
		require.NotNil(t, store.snap, "a timed-out renewal must leave an uncancelled context for ordinary collection")
		require.Equal(t, store.site.ID, store.snap.SiteID)
		require.LessOrEqual(t, time.Since(started), 10*time.Second)
		stored, err := s.session(store.renewals[1].site)
		require.NoError(t, err)
		require.Equal(t, "identity_pending", stored.RefreshState)
	})
}

func TestScheduledSessionRefreshAdvancesPastPersistentlyFailingSites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, store, connector := sessionScheduleEngine(t, 3)
		due, err := s.session(store.renewals[3].site)
		require.NoError(t, err)
		expires := s.now().Add(time.Minute)
		due.RefreshState, due.ExpiresAt = "ready", &expires
		setSessionFixture(t, store.renewals[3].memoryStore, due)
		rotations := 0
		connector.refresh = func(_ context.Context, site Site, session Session) (Session, error) {
			require.EqualValues(t, 3, site.ID, "unverified rotations must never be repeated")
			rotations++
			expires := s.now().Add(time.Hour)
			session.AccessToken, session.RefreshToken, session.ExpiresAt = "fixture-renewed-access", "fixture-renewed-refresh", &expires
			return session, nil
		}
		connector.verify = func(ctx context.Context, site Site, session Session) (Session, error) {
			if site.ID <= 2 {
				<-ctx.Done()
				return Session{}, ctx.Err()
			}
			return session, nil
		}
		for range 3 {
			store.site.NextSyncAt = s.now().Add(-time.Minute)
			require.ErrorIs(t, s.runDue(t.Context()), context.DeadlineExceeded)
		}
		stored, err := s.session(store.renewals[3].site)
		require.NoError(t, err)
		require.Equal(t, "ready", stored.RefreshState, "later sites must recover even while earlier identity checks keep timing out")
		require.Equal(t, "fixture-renewed-refresh", stored.RefreshToken)
		require.Equal(t, 1, rotations)
		require.NotNil(t, store.snap)
		require.EqualValues(t, 3, store.snap.ID, "ordinary collection must run during every failed renewal batch")
		for _, id := range []int64{1, 2} {
			stored, err = s.session(store.renewals[id].site)
			require.NoError(t, err)
			require.Equal(t, "identity_pending", stored.RefreshState)
		}
	})
}

func TestScheduledSessionRefreshBoundsFastFailuresAndWrapsCursor(t *testing.T) {
	s, store, connector := sessionScheduleEngine(t, 25)
	attempts := 0
	connector.verify = func(_ context.Context, site Site, session Session) (Session, error) {
		attempts++
		if site.ID == 25 {
			return session, nil
		}
		return Session{}, errConnectorRemote
	}
	require.ErrorIs(t, s.runDue(t.Context()), errConnectorRemote)
	require.Equal(t, 20, attempts, "fast failures must not trigger an unbounded recovery batch")
	stored, err := s.session(store.renewals[25].site)
	require.NoError(t, err)
	require.Equal(t, "identity_pending", stored.RefreshState)
	require.ErrorIs(t, s.runDue(t.Context()), errConnectorRemote)
	require.Equal(t, 40, attempts)
	stored, err = s.session(store.renewals[25].site)
	require.NoError(t, err)
	require.Equal(t, "ready", stored.RefreshState)
}

func TestScheduledSessionRefreshReachesBeyondCappedAdminList(t *testing.T) {
	s, store, connector := sessionScheduleEngine(t, 1001)
	expires := s.now().Add(time.Hour)
	for id := int64(1); id <= 1000; id++ {
		setSessionFixture(t, store.renewals[id].memoryStore, Session{
			AccessToken: "fixture-access", RefreshToken: "fixture-refresh", UserID: 5,
			UserAgent: "FixtureBrowser/1", ExpiresAt: &expires, RefreshState: "ready",
		})
	}
	verifications := 0
	connector.verify = func(_ context.Context, site Site, session Session) (Session, error) {
		require.EqualValues(t, 1001, site.ID)
		verifications++
		return session, nil
	}
	require.NoError(t, s.refreshDueSessions(t.Context()))
	stored, err := s.session(store.renewals[1001].site)
	require.NoError(t, err)
	require.Equal(t, "identity_pending", stored.RefreshState, "one scan must remain bounded")
	require.NoError(t, s.refreshDueSessions(t.Context()))
	stored, err = s.session(store.renewals[1001].site)
	require.NoError(t, err)
	require.Equal(t, "ready", stored.RefreshState, "the next scan must reach a site outside the admin list")
	require.Equal(t, 1, verifications)
}
