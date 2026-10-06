package upstreamgovernance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type autoLoginConnector struct {
	*lifecycleConnector
	login      func(context.Context, Site, LoginInput) (Session, *Challenge, error)
	loginCalls int
}

func (c *autoLoginConnector) Login(ctx context.Context, site Site, input LoginInput) (Session, *Challenge, error) {
	c.loginCalls++
	return c.login(ctx, site, input)
}

func saveAutoLoginFixture(t *testing.T, s *Service, site *Site, owner int64) {
	t.Helper()
	var err error
	site.LoginCipher, err = s.encryptLogin(storedLoginCredentials{
		LoginCredentials: LoginCredentials{Username: "saved-user", Password: "saved-password"},
		OwnerUserID:      owner,
	})
	require.NoError(t, err)
}

func TestScheduledAutoReauthorizationRecoversExpiredNewAPISessionWithoutVersionChange(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	store.site.NextSyncAt = s.now().Add(time.Hour)
	nextSync := store.site.NextSyncAt
	old.RefreshToken = ""
	expired := s.now().Add(-time.Minute)
	old.ExpiresAt = &expired
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	version := store.site.Version
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(_ context.Context, _ Site, input LoginInput) (Session, *Challenge, error) {
		require.Equal(t, "saved-user", input.Username)
		require.Equal(t, "saved-password", input.Password)
		require.Equal(t, old.UserAgent, input.UserAgent)
		return Session{AccessToken: "new-login", UserID: old.UserID, UserAgent: input.UserAgent}, nil, nil
	}
	s.connector = connector
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, connector.loginCalls)
	require.Equal(t, version, store.site.Version)
	require.Equal(t, nextSync, store.site.NextSyncAt)
	require.Equal(t, "new-login", mustSession(t, s, store.site).AccessToken)
	require.Nil(t, store.snap, "renewal must not force catalog collection outside its schedule")
}

func TestCollectionAutoReauthorizationRetriesOnlyOneReadAfterUnknownExpiry(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken, old.ExpiresAt = "", nil
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(_ context.Context, _ Site, _ LoginInput) (Session, *Challenge, error) {
		return Session{AccessToken: "relogin", UserID: old.UserID, UserAgent: old.UserAgent}, nil, nil
	}
	reads := 0
	connector.discover = func(_ context.Context, _ Site, session Session) (Catalog, error) {
		reads++
		if reads == 1 {
			require.Equal(t, old.AccessToken, session.AccessToken)
			return Catalog{}, ErrReauth
		}
		require.Equal(t, "relogin", session.AccessToken)
		return connector.catalog, nil
	}
	s.connector = connector
	require.NoError(t, s.runSiteDue(t.Context(), store.site.ID))
	require.NotNil(t, store.snap)
	require.Equal(t, 2, reads)
	require.Equal(t, 1, connector.loginCalls)
	require.Equal(t, "relogin", mustSession(t, s, store.site).AccessToken)
	require.Equal(t, s.now().Add(15*time.Minute), store.site.NextSyncAt, "collection reservation must not be reset to a stale due time")
}

func TestManualCollectionDoesNotAutomaticallyReauthorize(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken = ""
	expired := s.now().Add(-time.Minute)
	old.ExpiresAt = &expired
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(context.Context, Site, LoginInput) (Session, *Challenge, error) {
		t.Fatal("manual collection must not use saved password")
		return Session{}, nil, nil
	}
	s.connector = connector
	_, err := s.Sync(t.Context(), store.site.ID)
	require.ErrorIs(t, err, ErrReauth)
	require.Zero(t, connector.loginCalls)
}

func TestAutoReauthorizationPersistsRetryBeforeLoginAndRespectsBackoff(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken = ""
	expired := s.now().Add(-time.Minute)
	old.ExpiresAt = &expired
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(context.Context, Site, LoginInput) (Session, *Challenge, error) {
		pending := mustSession(t, s, store.site)
		require.Equal(t, autoReauthorizationRetryWait, pending.AutoReauthorizationState)
		require.NotNil(t, pending.AutoReauthorizationRetryAt)
		require.True(t, pending.AutoReauthorizationRetryAt.After(s.now()))
		return Session{}, nil, errConnectorRemote
	}
	s.connector = connector
	require.ErrorIs(t, s.runSiteDue(t.Context(), store.site.ID), ErrReauth)
	require.Equal(t, 1, connector.loginCalls)
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.True(t, status.AutoReauthorizationEnabled)
	require.Equal(t, autoReauthorizationRetryWait, status.AutoReauthorizationState)
	require.NotNil(t, status.LastAutoReauthorizationAt)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, connector.loginCalls, "another scheduler pass must not retry inside persisted cooldown")
}

func TestAutoReauthorizationChallengeStopsUntilCredentialsChange(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken = ""
	expired := s.now().Add(-time.Minute)
	old.ExpiresAt = &expired
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(context.Context, Site, LoginInput) (Session, *Challenge, error) {
		return Session{}, &Challenge{Kind: "captcha", Provider: CaptchaTurnstile}, nil
	}
	s.connector = connector
	require.ErrorIs(t, s.runSiteDue(t.Context(), store.site.ID), ErrReauth)
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, status.AutoReauthorizationEnabled)
	require.Equal(t, autoReauthorizationVerificationRequired, status.AutoReauthorizationState)
	require.NotNil(t, status.LastAutoReauthorizationAt)
	require.Equal(t, old.AccessToken, mustSession(t, s, store.site).AccessToken)
	count := 0
	for _, event := range store.events {
		if event.Kind == "auto_reauthorization_required" {
			count++
			require.Equal(t, autoReauthorizationVerificationRequired, event.After)
		}
	}
	require.Equal(t, 1, count)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, connector.loginCalls)
}

func TestAutoReauthorizationRejectsChangedIdentityAndManagedKeyOwner(t *testing.T) {
	for _, name := range []string{"login_identity", "managed_key_owner"} {
		t.Run(name, func(t *testing.T) {
			s, store, base, old := lifecycleEngine(t)
			store.site.Platform = "newapi"
			old.RefreshToken = ""
			expired := s.now().Add(-time.Minute)
			old.ExpiresAt = &expired
			setSessionFixture(t, store.memoryStore, old)
			saveAutoLoginFixture(t, s, &store.site, old.UserID)
			identity := old.UserID
			if name == "login_identity" {
				identity = 6
			} else {
				store.keys = []ManagedKey{{SiteID: store.site.ID, OwnerUserID: 6}}
			}
			connector := &autoLoginConnector{lifecycleConnector: base}
			connector.login = func(context.Context, Site, LoginInput) (Session, *Challenge, error) {
				return Session{AccessToken: "wrong-owner", UserID: identity}, nil, nil
			}
			s.connector = connector
			require.ErrorIs(t, s.runSiteDue(t.Context(), store.site.ID), ErrReauth)
			stored := mustSession(t, s, store.site)
			require.Equal(t, old.AccessToken, stored.AccessToken)
			status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
			require.NoError(t, err)
			require.False(t, status.AutoReauthorizationEnabled)
			require.Equal(t, autoReauthorizationIdentityMismatch, status.AutoReauthorizationState)
		})
	}
}

func TestAutoReauthorizationStatusNeverExposesCredentialsAndHonorsAvailability(t *testing.T) {
	s, store, _, old := lifecycleEngine(t)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.True(t, status.AutoReauthorizationEnabled)
	require.Equal(t, autoReauthorizationReady, status.AutoReauthorizationState)
	raw, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "saved-user")
	require.NotContains(t, string(raw), "saved-password")
	store.site.Enabled = false
	status, err = s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, status.AutoReauthorizationEnabled)
	require.Equal(t, autoReauthorizationCollectionDisabled, status.AutoReauthorizationState)
	store.site.Enabled = true
	s.durableKey = false
	status, err = s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, status.AutoReauthorizationEnabled)
}

func TestAutoReauthorizationRejectedCredentialsResumeOnlyAfterUpdate(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken = ""
	expired := s.now().Add(-time.Minute)
	old.ExpiresAt = &expired
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(_ context.Context, _ Site, input LoginInput) (Session, *Challenge, error) {
		if input.Password == "saved-password" {
			return Session{}, nil, ErrReauth
		}
		return Session{AccessToken: "new-login", UserID: old.UserID, UserAgent: old.UserAgent}, nil, nil
	}
	s.connector = connector
	require.ErrorIs(t, s.runSiteDue(t.Context(), store.site.ID), ErrReauth)
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, status.AutoReauthorizationEnabled)
	require.Equal(t, autoReauthorizationCredentialsRejected, status.AutoReauthorizationState)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, connector.loginCalls)
	_, err = s.UpdateSiteWithLogin(t.Context(), store.site.ID, store.site, &LoginCredentials{Username: "saved-user", Password: "corrected-password"})
	require.NoError(t, err)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 2, connector.loginCalls)
	require.Equal(t, "new-login", mustSession(t, s, store.site).AccessToken)
}

func TestAutoReauthorizationDoesNotTreatRead403AsExpiredSession(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken, old.ExpiresAt = "", nil
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(context.Context, Site, LoginInput) (Session, *Challenge, error) {
		t.Fatal("403 is not proof of an expired session")
		return Session{}, nil, nil
	}
	connector.discover = func(context.Context, Site, Session) (Catalog, error) {
		return Catalog{}, errConnectorDenied
	}
	s.connector = connector
	require.ErrorIs(t, s.runSiteDue(t.Context(), store.site.ID), errConnectorDenied)
	require.Zero(t, connector.loginCalls)
}

func TestAutoReauthorizationStopsWhenNewSessionIsImmediatelyRejected(t *testing.T) {
	s, store, base, old := lifecycleEngine(t)
	store.site.Platform = "newapi"
	old.RefreshToken, old.ExpiresAt = "", nil
	setSessionFixture(t, store.memoryStore, old)
	saveAutoLoginFixture(t, s, &store.site, old.UserID)
	connector := &autoLoginConnector{lifecycleConnector: base}
	connector.login = func(context.Context, Site, LoginInput) (Session, *Challenge, error) {
		return Session{AccessToken: "still-rejected", UserID: old.UserID, UserAgent: old.UserAgent}, nil, nil
	}
	connector.discover = func(context.Context, Site, Session) (Catalog, error) { return Catalog{}, ErrReauth }
	s.connector = connector
	require.ErrorIs(t, s.runSiteDue(t.Context(), store.site.ID), ErrReauth)
	status, err := s.AuthorizationStatus(t.Context(), store.site.ID)
	require.NoError(t, err)
	require.False(t, status.AutoReauthorizationEnabled)
	require.Equal(t, autoReauthorizationCredentialsRejected, status.AutoReauthorizationState)
	require.NoError(t, s.runDue(t.Context()))
	require.Equal(t, 1, connector.loginCalls)
}

func mustSession(t *testing.T, s *Service, site Site) Session {
	t.Helper()
	session, err := s.session(site)
	require.NoError(t, err)
	return session
}
