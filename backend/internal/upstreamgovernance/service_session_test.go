package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type runtimeMemoryStore struct {
	*memoryStore
	mu       sync.Mutex
	held     bool
	saves    []Session
	failSave int
}

func (m *runtimeMemoryStore) LockSite(context.Context, int64) (func(), bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held {
		return nil, false, nil
	}
	m.held = true
	return func() { m.mu.Lock(); m.held = false; m.mu.Unlock() }, true, nil
}

func (m *runtimeMemoryStore) SaveRuntimeSession(_ context.Context, id, version int64, previous, encrypted string) error {
	if m.site.ID != id || m.site.Version != version || m.site.SessionCipher != previous {
		return ErrConflict
	}
	if m.failSave > 0 && len(m.saves)+1 == m.failSave {
		return errors.New("fixture persistence failure")
	}
	raw, err := (fakeCipher{}).Decrypt(encrypted)
	if err != nil {
		return err
	}
	var value Session
	if err = json.Unmarshal([]byte(raw), &value); err != nil {
		return err
	}
	m.saves = append(m.saves, value)
	m.site.SessionCipher = encrypted
	return nil
}

type lifecycleConnector struct {
	*fakeConnector
	refresh   func(context.Context, Site, Session) (Session, error)
	verify    func(context.Context, Site, Session) (Session, error)
	discover  func(context.Context, Site, Session) (Catalog, error)
	ensureErr error
}

func (c *lifecycleConnector) Refresh(ctx context.Context, site Site, session Session) (Session, error) {
	return c.refresh(ctx, site, session)
}

func (c *lifecycleConnector) VerifySession(ctx context.Context, site Site, session Session) (Session, error) {
	if c.verify != nil {
		return c.verify(ctx, site, session)
	}
	return session, nil
}

func (c *lifecycleConnector) Discover(ctx context.Context, site Site, session Session) (Catalog, error) {
	if c.discover != nil {
		return c.discover(ctx, site, session)
	}
	return c.fakeConnector.Discover(ctx, site, session)
}

func (c *lifecycleConnector) EnsureKey(ctx context.Context, site Site, session Session, group RemoteGroup, name string, plan *KeyCreationPlan) (RemoteKey, error) {
	if c.ensureErr != nil {
		c.keyCalls++
		return RemoteKey{}, c.ensureErr
	}
	return c.fakeConnector.EnsureKey(ctx, site, session, group, name, plan)
}

func setSessionFixture(t *testing.T, m *memoryStore, session Session) {
	t.Helper()
	encrypted, err := (fakeCipher{}).Encrypt(canonical(session))
	require.NoError(t, err)
	m.site.SessionCipher = encrypted
}

func lifecycleEngine(t *testing.T) (*Service, *runtimeMemoryStore, *lifecycleConnector, Session) {
	t.Helper()
	s, m, base, _ := setupEngine(t)
	store := &runtimeMemoryStore{memoryStore: m}
	s.store = store
	c := &lifecycleConnector{fakeConnector: base}
	s.connector = c
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	expiry := now.Add(time.Minute)
	session := Session{AccessToken: "fixture-old", RefreshToken: "fixture-rotation", UserID: 5, UserAgent: "FixtureBrowser/1", ExpiresAt: &expiry}
	setSessionFixture(t, m, session)
	c.refresh = func(_ context.Context, _ Site, old Session) (Session, error) {
		old.AccessToken = "fixture-new"
		old.RefreshToken = "fixture-rotated"
		expires := now.Add(time.Hour)
		old.ExpiresAt = &expires
		return old, nil
	}
	return s, store, c, session
}

func TestSessionRefreshPersistsPendingAndPairBeforeUseWithoutVersionChange(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	version := store.site.Version
	c.refresh = func(_ context.Context, site Site, old Session) (Session, error) {
		require.Equal(t, "pending", store.saves[0].RefreshState)
		require.Equal(t, "fixture-rotation", old.RefreshToken)
		require.Equal(t, "FixtureBrowser/1", old.UserAgent)
		expires := s.now().Add(time.Hour)
		return Session{AccessToken: "fixture-new", RefreshToken: "fixture-rotated", UserID: old.UserID, ExpiresAt: &expires, UserAgent: old.UserAgent}, nil
	}
	c.discover = func(_ context.Context, _ Site, session Session) (Catalog, error) {
		require.Equal(t, "fixture-new", session.AccessToken)
		stored, err := s.session(store.site)
		require.NoError(t, err)
		require.Equal(t, session.AccessToken, stored.AccessToken)
		require.Equal(t, "fixture-rotated", stored.RefreshToken)
		return c.catalog, nil
	}
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, version, store.site.Version)
	require.Equal(t, version, snapshot.SiteVersion)
	require.Len(t, store.saves, 3)
	require.Equal(t, "identity_pending", store.saves[1].RefreshState)
	require.Equal(t, "ready", store.saves[2].RefreshState)
}

func TestSessionRefreshPersistenceFailureAndUncertainRotationNeverReuseToken(t *testing.T) {
	for _, failSave := range []int{1, 2, 0} {
		t.Run(string(rune('0'+failSave)), func(t *testing.T) {
			s, store, c, _ := lifecycleEngine(t)
			store.failSave = failSave
			calls := 0
			refresh := c.refresh
			c.refresh = func(ctx context.Context, site Site, session Session) (Session, error) {
				calls++
				if failSave == 0 {
					return Session{}, errors.New("fixture transport ambiguity")
				}
				return refresh(ctx, site, session)
			}
			_, err := s.Sync(t.Context(), 1)
			require.Error(t, err)
			require.Zero(t, c.discoveryCalls)
			if failSave == 1 {
				require.Zero(t, calls, "failed pending persistence must not send rotation")
				return
			}
			_, err = s.Sync(t.Context(), 1)
			require.ErrorIs(t, err, ErrReauth)
			require.Equal(t, 1, calls, "persisted uncertain marker must prevent blind token reuse")
		})
	}
}

func TestSessionRefreshOnlyRetriesDiscoveryOnceAfterUnauthorized(t *testing.T) {
	s, store, c, session := lifecycleEngine(t)
	expires := s.now().Add(time.Hour)
	session.ExpiresAt = &expires
	setSessionFixture(t, store.memoryStore, session)
	reads, rotations := 0, 0
	refresh := c.refresh
	c.refresh = func(ctx context.Context, site Site, session Session) (Session, error) {
		rotations++
		return refresh(ctx, site, session)
	}
	c.discover = func(_ context.Context, _ Site, session Session) (Catalog, error) {
		reads++
		if reads == 1 {
			require.Equal(t, "fixture-old", session.AccessToken)
			return Catalog{}, ErrReauth
		}
		require.Equal(t, "fixture-new", session.AccessToken)
		return Catalog{}, ErrReauth
	}
	_, err := s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	require.Equal(t, 2, reads)
	require.Equal(t, 1, rotations)
	_, err = s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	require.Equal(t, 2, reads, "rejected rotated access must require a manual authorization")
	require.Equal(t, 1, rotations)
}

func TestSessionRefreshDoesNotAssumeNewAPIAndRejectsExpiredLegacySessions(t *testing.T) {
	for _, platform := range []string{"newapi", "sub2api"} {
		t.Run(platform, func(t *testing.T) {
			s, store, c, session := lifecycleEngine(t)
			store.site.Platform = platform
			if platform == "sub2api" {
				session.RefreshToken = ""
			}
			expires := s.now().Add(-time.Minute)
			session.ExpiresAt = &expires
			setSessionFixture(t, store.memoryStore, session)
			c.refresh = func(context.Context, Site, Session) (Session, error) {
				t.Fatal("unsupported refresh")
				return Session{}, nil
			}
			_, err := s.Sync(t.Context(), 1)
			require.ErrorIs(t, err, ErrReauth)
			require.Zero(t, c.discoveryCalls)
		})
	}
}

func TestSessionWorkerRefreshIndependentOfCollectionInterval(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	store.site.NextSyncAt = s.now().Add(365 * 24 * time.Hour)
	next := store.site.NextSyncAt
	err := s.runDue(t.Context())
	require.NoError(t, err)
	require.Len(t, store.saves, 3)
	require.Zero(t, c.discoveryCalls)
	require.Equal(t, next, store.site.NextSyncAt)
	require.Nil(t, store.site.LastSyncAt)
}

func TestSessionRefreshSerializedAcrossServices(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	other := NewService(store, c, s.local, s.cipher, true)
	other.now = s.now
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	refresh := c.refresh
	c.refresh = func(ctx context.Context, site Site, session Session) (Session, error) {
		close(started)
		<-finish
		return refresh(ctx, site, session)
	}
	go func() { _, err := s.Sync(t.Context(), 1); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not start")
	}
	_, err := other.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrBusy)
	close(finish)
	require.NoError(t, <-done)
	_, err = other.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, store.saves, 3)
}

func TestBrowserSessionImportVerifiesIdentityAndPreservesManagedOwner(t *testing.T) {
	for _, scenario := range []string{"success", "stale", "mismatched_identity", "managed_owner", "binding_owner", "invalid_user_agent"} {
		t.Run(scenario, func(t *testing.T) {
			s, store, c, session := lifecycleEngine(t)
			importer, ok := any(s).(interface {
				ImportBrowserSession(context.Context, int64, int64, Session, *LoginCredentials) (*ConnectResult, error)
			})
			require.True(t, ok, "trusted browser captures need a versioned domain import")
			before := store.site
			version := before.Version
			verified := 0
			c.verify = func(_ context.Context, site Site, incoming Session) (Session, error) {
				verified++
				require.Equal(t, "https://upstream.example", site.BaseURL)
				require.Equal(t, "FixtureBrowser/1", incoming.UserAgent)
				if scenario == "mismatched_identity" {
					return Session{}, ErrReauth
				}
				incoming.UserID = 5
				return incoming, nil
			}
			switch scenario {
			case "stale":
				version--
			case "managed_owner":
				store.keys = []ManagedKey{{SiteID: 1, OwnerUserID: 99}}
			case "binding_owner":
				store.bindings = []Binding{{ID: 1}}
				old := session
				old.UserID = 99
				setSessionFixture(t, store.memoryStore, old)
				before = store.site
			case "invalid_user_agent":
				session.UserAgent = "Browser\r\nInjected: yes"
			}
			credentials := &LoginCredentials{Username: "verified@example.com", Password: "fixture-browser-password"}
			result, err := importer.ImportBrowserSession(t.Context(), 1, version, session, credentials)
			if scenario != "success" {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, before, store.site)
				if scenario == "stale" || scenario == "invalid_user_agent" {
					require.Zero(t, verified)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, before.Version+1, result.Site.Version)
			require.Equal(t, 1, verified)
			stored, err := s.session(store.site)
			require.NoError(t, err)
			require.Equal(t, session.AccessToken, stored.AccessToken)
			require.Equal(t, session.RefreshToken, stored.RefreshToken)
			require.Equal(t, session.UserAgent, stored.UserAgent)
			login, err := s.LoginCredentials(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, *credentials, login.LoginCredentials)
			encoded := canonical(result)
			for _, secret := range []string{session.AccessToken, session.RefreshToken, session.UserAgent, credentials.Password} {
				require.NotContains(t, encoded, secret)
			}
		})
	}
}

func TestSessionAuthorizationStatusIsSanitized(t *testing.T) {
	s, store, _, _ := lifecycleEngine(t)
	status, err := s.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.AutoRefreshEnabled)
	require.True(t, status.HasRefreshToken)
	require.False(t, status.ReauthorizationRequired)
	for _, secret := range []string{"fixture-old", "fixture-rotation", "FixtureBrowser/1", store.site.SessionCipher} {
		require.NotContains(t, canonical(status), secret)
		require.NotContains(t, canonical(store.site), secret)
	}
	for _, field := range []string{`"access_token"`, `"refresh_token"`, `"user_agent"`, `"cookies"`} {
		require.False(t, strings.Contains(canonical(status), field))
	}
}

func TestManualSessionImportRejectsRefreshTokenWithoutExpiry(t *testing.T) {
	s, store, _, _ := setupEngine(t)
	before := store.site
	result, err := s.Connect(t.Context(), store.site.ID, LoginInput{SessionToken: "fixture-access", RefreshToken: "fixture-refresh"})
	require.ErrorIs(t, err, ErrInvalid)
	require.Nil(t, result)
	require.Equal(t, before, store.site, "an unschedulable refresh token must not replace a working authorization")
}

func TestSessionProxyChangeRequiresAuthorizationButRetainsOwnership(t *testing.T) {
	s, store, _, session := lifecycleEngine(t)
	proxy := int64(31)
	input := store.site
	input.ProxyID = &proxy
	_, err := s.UpdateSite(t.Context(), 1, input)
	require.NoError(t, err)
	stored, err := s.session(store.site)
	require.NoError(t, err)
	require.Equal(t, session.UserID, stored.UserID)
	require.Equal(t, "reauth_required", stored.RefreshState)
	status, err := s.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.ReauthorizationRequired)
	require.False(t, status.AutoRefreshEnabled)
}

func TestSessionMutationUnauthorizedIsNeverRefreshedOrRetried(t *testing.T) {
	for _, operation := range []string{"create_key", "apply"} {
		t.Run(operation, func(t *testing.T) {
			s, store, c, session := lifecycleEngine(t)
			expires := s.now().Add(time.Hour)
			session.ExpiresAt = &expires
			setSessionFixture(t, store.memoryStore, session)
			c.refresh = func(context.Context, Site, Session) (Session, error) {
				t.Fatal("mutation retried through refresh")
				return Session{}, nil
			}
			snapshot, err := s.Sync(t.Context(), 1)
			require.NoError(t, err)
			c.ensureErr = ErrReauth
			if operation == "create_key" {
				result, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "openai"}}})
				require.NoError(t, err)
				require.Equal(t, "reauth_required", result.Items[0].Error)
			} else {
				preview, err := s.Preview(t.Context(), 1, selections())
				require.NoError(t, err)
				result, err := s.Apply(t.Context(), 1, preview.ID)
				require.NoError(t, err)
				require.Equal(t, "reauth_required", result.Items[0].Error)
			}
			require.Equal(t, 1, c.keyCalls)
			stored, err := s.session(store.site)
			require.NoError(t, err)
			require.Equal(t, "reauth_required", stored.RefreshState)
		})
	}
}

func TestSessionRefreshRejectsChangedIdentityAfterDurableRotation(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	c.verify = func(_ context.Context, _ Site, session Session) (Session, error) {
		require.Equal(t, "fixture-rotated", store.saves[1].RefreshToken)
		session.UserID = 99
		return session, nil
	}
	_, err := s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	require.Zero(t, c.discoveryCalls)
	stored, err := s.session(store.site)
	require.NoError(t, err)
	require.Equal(t, "reauth_required", stored.RefreshState)
	require.Equal(t, int64(5), stored.UserID)
}

func TestSessionRefreshCannotOverwriteNewManualAuthorization(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	refresh := c.refresh
	var manualSite Site
	c.refresh = func(ctx context.Context, site Site, session Session) (Session, error) {
		manual := Session{AccessToken: "fixture-manual-new", RefreshToken: "fixture-manual-rotation", UserID: 5}
		setSessionFixture(t, store.memoryStore, manual)
		store.site.Version++
		store.site.Status = "connected"
		manualSite = store.site
		return refresh(ctx, site, session)
	}
	_, err := s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrConflict)
	stored, err := s.session(store.site)
	require.NoError(t, err)
	require.Equal(t, "fixture-manual-new", stored.AccessToken)
	require.Equal(t, "fixture-manual-rotation", stored.RefreshToken)
	require.Zero(t, c.discoveryCalls)
	require.Equal(t, manualSite, store.site, "a stale refresh cannot overwrite newer authorization status or scheduling")
}

func TestSessionClearanceCookieAloneIsNotAuthorization(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	session := Session{UserAgent: "FixtureBrowser/1", Cookies: map[string]string{"cf_clearance": "fixture-clearance"}}
	setSessionFixture(t, store.memoryStore, session)
	_, err := s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	require.Zero(t, c.discoveryCalls)
}

func TestSessionRefreshShortLifetimeDoesNotRotateOnEveryRead(t *testing.T) {
	s, store, c, session := lifecycleEngine(t)
	now := s.now()
	expires := now.Add(time.Minute)
	session.IssuedAt, session.ExpiresAt = &now, &expires
	setSessionFixture(t, store.memoryStore, session)
	rotations := 0
	c.refresh = func(_ context.Context, _ Site, previous Session) (Session, error) {
		rotations++
		issued := s.now()
		expires := issued.Add(time.Minute)
		previous.IssuedAt, previous.ExpiresAt = &issued, &expires
		previous.AccessToken, previous.RefreshToken = "fixture-new", "fixture-rotated"
		return previous, nil
	}
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, rotations, "a newly issued short-lived token is still fresh")
	s.now = func() time.Time { return now.Add(49 * time.Second) }
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, rotations)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, rotations, "a second read must reuse the new short-lived pair")
}

func TestSessionRefreshVerifiesSavedPairAfterInitiatingCancellation(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	refresh := c.refresh
	c.refresh = func(ctx context.Context, site Site, previous Session) (Session, error) {
		rotated, err := refresh(ctx, site, previous)
		cancel()
		return rotated, err
	}
	c.verify = func(ctx context.Context, _ Site, session Session) (Session, error) {
		require.NoError(t, ctx.Err(), "verification uses its own bounded completion context")
		return session, ctx.Err()
	}
	require.NoError(t, s.refreshSiteSession(ctx, 1))
	stored, err := s.session(store.site)
	require.NoError(t, err)
	require.Equal(t, "fixture-rotated", stored.RefreshToken)
	require.Equal(t, "ready", stored.RefreshState)
}

func TestSessionRefreshTransientIdentityFailureRetriesOnlyIdentity(t *testing.T) {
	s, store, c, _ := lifecycleEngine(t)
	rotations, verifications := 0, 0
	refresh := c.refresh
	c.refresh = func(ctx context.Context, site Site, session Session) (Session, error) {
		rotations++
		return refresh(ctx, site, session)
	}
	c.verify = func(_ context.Context, _ Site, session Session) (Session, error) {
		verifications++
		if verifications == 1 {
			return Session{}, errConnectorRemote
		}
		return session, nil
	}
	require.ErrorIs(t, s.refreshSiteSession(t.Context(), 1), errConnectorRemote)
	stored, err := s.session(store.site)
	require.NoError(t, err)
	require.Equal(t, "fixture-rotated", stored.RefreshToken)
	require.Equal(t, "identity_pending", stored.RefreshState)
	require.NoError(t, s.refreshSiteSession(t.Context(), 1))
	require.Equal(t, 1, rotations)
	require.Equal(t, 2, verifications)
	stored, err = s.session(store.site)
	require.NoError(t, err)
	require.Equal(t, "ready", stored.RefreshState)
}

func TestSessionExpiredManagementDoesNotReplayOrRefreshBillableProbe(t *testing.T) {
	s, memory, base, _ := importedEngine(t)
	store := &runtimeMemoryStore{memoryStore: memory}
	s.store = store
	c := &lifecycleConnector{fakeConnector: base}
	s.connector = c
	base.err = ErrReauth
	c.refresh = func(context.Context, Site, Session) (Session, error) {
		t.Fatal("billable probe refreshed dashboard session")
		return Session{}, nil
	}
	expires := s.now().Add(-time.Minute)
	setSessionFixture(t, memory, Session{AccessToken: "fixture-old", RefreshToken: "fixture-refresh", UserID: 5, ExpiresAt: &expires})
	result, err := s.Probe(t.Context(), 1, memory.bindings[0].ID, "gpt-fixture")
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "reauth_required", result.ErrorCode)
	require.Equal(t, 1, c.probeCalls)
	require.Empty(t, store.saves)
}

func TestAuthorizationStatusPreservesKnownExpiryWithoutRefreshToken(t *testing.T) {
	s, store, _, _ := setupEngine(t)
	expires := s.now().Add(-time.Minute)
	raw, err := json.Marshal(Session{AccessToken: "fixture-access", UserID: 5, UserAgent: "FixtureBrowser/1", ExpiresAt: &expires})
	require.NoError(t, err)
	store.site.SessionCipher, err = s.cipher.Encrypt(string(raw))
	require.NoError(t, err)
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.True(t, status.HasSession)
	require.False(t, status.HasRefreshToken)
	require.False(t, status.AutoRefreshEnabled)
	require.True(t, status.ReauthorizationRequired)
	require.Equal(t, &expires, status.ExpiresAt)
}

func TestAuthorizationStatusNeverClaimsScheduledRefreshWithoutKnownExpiry(t *testing.T) {
	s, store, _, _ := lifecycleEngine(t)
	setSessionFixture(t, store.memoryStore, Session{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", UserID: 5, UserAgent: "FixtureBrowser/1", RefreshState: "ready"})
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.True(t, status.HasRefreshToken)
	require.True(t, status.RefreshSupported)
	require.Nil(t, status.ExpiresAt)
	require.False(t, status.AutoRefreshEnabled, "without expiry the background scheduler cannot choose a renewal time")
	require.False(t, status.ReauthorizationRequired, "the current bearer can still work or refresh on a safe 401")
}
