package upstreamgovernance

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sessionPostgresFixture(t *testing.T) *sql.DB {
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
	schema := fmt.Sprintf("governance_sessions_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); _ = base.Close() })
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY, extra JSONB NOT NULL DEFAULT '{}', deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	for _, name := range []string{"247_upstream_governance.sql", "248_upstream_governance_keys.sql", "249_upstream_governance_balance_monitor.sql", "250_upstream_governance_platforms.sql", "251_upstream_governance_login_credentials.sql", "252_upstream_governance_multiple_target_groups.sql", "255_upstream_governance_key_creation_plans.sql", "256_upstream_governance_flexible_intervals.sql", "258_upstream_governance_key_health.sql"} {
		raw, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	return db
}

func TestSQLRuntimeSessionCASDoesNotChangeConfigurationOrSnapshot(t *testing.T) {
	db := sessionPostgresFixture(t)
	store := NewSQLStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	site := &Site{Name: "Fixture", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true, IntervalMinutes: 43200, SessionCipher: "initial-cipher", LoginCipher: "login-cipher", Status: "healthy", NextSyncAt: now.Add(time.Hour)}
	require.NoError(t, store.CreateSite(t.Context(), site))
	require.NoError(t, store.ObserveSite(t.Context(), site.ID, "healthy", "", now, site.NextSyncAt))
	site, err := store.GetSite(t.Context(), site.ID)
	require.NoError(t, err)
	snapshot := &Snapshot{SiteID: site.ID, SiteVersion: site.Version, CreatedAt: now, Catalog: Catalog{Groups: []RemoteGroup{}}}
	require.NoError(t, store.SaveSnapshot(t.Context(), snapshot, nil))
	runtime := store.(runtimeSessionStore)
	require.NoError(t, runtime.SaveRuntimeSession(t.Context(), site.ID, site.Version, "initial-cipher", "rotated-cipher"))
	got, err := store.GetSite(t.Context(), site.ID)
	require.NoError(t, err)
	want := *site
	want.SessionCipher = "rotated-cipher"
	require.Equal(t, want, *got)
	latest, err := store.LatestSnapshot(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, site.Version, latest.SiteVersion)
	require.ErrorIs(t, runtime.SaveRuntimeSession(t.Context(), site.ID, site.Version, "initial-cipher", "stale-cipher"), ErrConflict)
	got.Name = "Manual edit"
	require.NoError(t, store.UpdateSite(t.Context(), got, got.Version))
	require.ErrorIs(t, runtime.SaveRuntimeSession(t.Context(), site.ID, site.Version, "rotated-cipher", "stale-cipher"), ErrConflict)
	current, err := store.GetSite(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, "rotated-cipher", current.SessionCipher)
}

func TestSQLSessionRefreshSerializedAndCrashMarkerSurvivesServiceRestart(t *testing.T) {
	db := sessionPostgresFixture(t)
	store := NewSQLStore(db)
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	session := Session{AccessToken: "fixture-old", RefreshToken: "fixture-rotation", UserAgent: "FixtureBrowser/1", UserID: 5, ExpiresAt: &expires}
	encrypted, err := (fakeCipher{}).Encrypt(canonical(session))
	require.NoError(t, err)
	site := &Site{Name: "Fixture", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true, IntervalMinutes: 43200, SessionCipher: encrypted, Status: "healthy", NextSyncAt: now.Add(30 * 24 * time.Hour)}
	require.NoError(t, store.CreateSite(t.Context(), site))
	var calls atomic.Int32
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	connector := &lifecycleConnector{fakeConnector: &fakeConnector{}}
	connector.refresh = func(_ context.Context, _ Site, input Session) (Session, error) {
		calls.Add(1)
		close(started)
		<-finish
		input.AccessToken, input.RefreshToken = "fixture-new", "fixture-rotated"
		expiry := now.Add(time.Hour)
		input.ExpiresAt = &expiry
		return input, nil
	}
	first := NewService(store, connector, nil, fakeCipher{}, true)
	second := NewService(NewSQLStore(db), connector, nil, fakeCipher{}, true)
	go func() { done <- first.refreshSiteSession(t.Context(), site.ID) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh not started")
	}
	require.ErrorIs(t, second.refreshSiteSession(t.Context(), site.ID), ErrBusy)
	close(finish)
	require.NoError(t, <-done)
	require.NoError(t, second.refreshSiteSession(t.Context(), site.ID))
	require.Equal(t, int32(1), calls.Load())
	current, err := store.GetSite(t.Context(), site.ID)
	require.NoError(t, err)
	require.Equal(t, site.Version, current.Version)
	rotated, err := first.session(*current)
	require.NoError(t, err)
	require.Equal(t, "fixture-rotated", rotated.RefreshToken)
	rotated.RefreshState = "pending"
	pending, err := (fakeCipher{}).Encrypt(canonical(rotated))
	require.NoError(t, err)
	require.NoError(t, store.(runtimeSessionStore).SaveRuntimeSession(t.Context(), site.ID, current.Version, current.SessionCipher, pending))
	restarted := NewService(NewSQLStore(db), connector, nil, fakeCipher{}, true)
	require.ErrorIs(t, restarted.refreshSiteSession(t.Context(), site.ID), ErrReauth)
	require.Equal(t, int32(1), calls.Load(), "crash recovery must not resend a rotation token")
}

func TestSQLRefreshSitePagesReachBeyondAdminList(t *testing.T) {
	db := sessionPostgresFixture(t)
	store := NewSQLStore(db)
	_, err := db.ExecContext(t.Context(), `INSERT INTO upstream_governance_sites
		(name, platform, base_url, interval_minutes)
		SELECT 'Fixture', 'sub2api', 'https://upstream.example', 15
		FROM generate_series(1, 1002)`)
	require.NoError(t, err)
	adminSites, err := store.ListSites(t.Context())
	require.NoError(t, err)
	require.Len(t, adminSites, 1000)
	pager, ok := store.(interface {
		ListSitesAfter(context.Context, int64, int) ([]Site, error)
	})
	require.True(t, ok, "refresh requires a keyset query separate from the capped admin list")
	cursor := int64(0)
	for _, want := range []int{400, 400, 202} {
		page, pageErr := pager.ListSitesAfter(t.Context(), cursor, 400)
		require.NoError(t, pageErr)
		require.Len(t, page, want)
		for _, site := range page {
			require.Greater(t, site.ID, cursor)
			cursor = site.ID
		}
	}
	require.Greater(t, cursor, adminSites[len(adminSites)-1].ID)
	empty, err := pager.ListSitesAfter(t.Context(), cursor, 400)
	require.NoError(t, err)
	require.Empty(t, empty)
}
