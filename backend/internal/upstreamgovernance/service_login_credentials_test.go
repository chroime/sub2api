package upstreamgovernance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGovernanceLoginCredentialsEditRoundtripCASAndClear(t *testing.T) {
	svc, store, _, _ := setupEngine(t)
	legacy, err := svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, &LoginCredentialsResult{Version: 1}, legacy)
	sessionBefore := store.site.SessionCipher
	input := store.site
	input.Name = "Renamed upstream"
	login := &LoginCredentials{Username: " administrator-canary@example.test ", Password: "  password-canary  "}
	saved, err := svc.UpdateSiteWithLogin(t.Context(), 1, input, login)
	require.NoError(t, err)
	require.EqualValues(t, 2, saved.Version)
	require.Equal(t, sessionBefore, saved.SessionCipher, "saving credentials must not reconnect")
	require.NotContains(t, saved.LoginCipher, "password-canary")
	revealed, err := svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "administrator-canary@example.test", revealed.Username)
	require.Equal(t, "  password-canary  ", revealed.Password, "password whitespace is significant")
	require.Equal(t, saved.Version, revealed.Version)
	sites, err := svc.ListSites(t.Context())
	require.NoError(t, err)
	raw, err := json.Marshal(sites)
	require.NoError(t, err)
	for _, forbidden := range []string{"password-canary", "administrator-canary", "login_cipher", saved.LoginCipher} {
		require.NotContains(t, string(raw), forbidden)
	}
	_, err = svc.UpdateSiteWithLogin(t.Context(), 1, input, &LoginCredentials{Username: "stale", Password: "stale"})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, saved.LoginCipher, store.site.LoginCipher)
	before := store.site
	before.Name = "Metadata only"
	saved, err = svc.UpdateSite(t.Context(), 1, before)
	require.NoError(t, err)
	require.Equal(t, before.LoginCipher, saved.LoginCipher)
	_, err = svc.UpdateSiteWithLogin(t.Context(), 1, *saved, &LoginCredentials{})
	require.NoError(t, err)
	revealed, err = svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, revealed.Username)
	require.Empty(t, revealed.Password)
	require.Empty(t, store.site.LoginCipher)
	require.Equal(t, sessionBefore, store.site.SessionCipher)
}

func TestGovernanceLoginCredentialsOriginResetAndReplacement(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		svc, store, _, _ := setupEngine(t)
		_, err := svc.UpdateSiteWithLogin(t.Context(), 1, store.site, &LoginCredentials{Username: "old-user", Password: "old-password"})
		require.NoError(t, err)
		input := store.site
		input.BaseURL, input.Platform = "https://replacement.example", "newapi"
		var login *LoginCredentials
		if replacement {
			login = &LoginCredentials{Username: "new-user", Password: "new-password"}
		}
		_, err = svc.UpdateSiteWithLogin(t.Context(), 1, input, login)
		require.NoError(t, err)
		require.Empty(t, store.site.SessionCipher)
		read, err := svc.LoginCredentials(t.Context(), 1)
		require.NoError(t, err)
		if replacement {
			require.Equal(t, *login, read.LoginCredentials)
		} else {
			require.Equal(t, LoginCredentials{}, read.LoginCredentials)
		}
	}
}

func TestGovernanceLoginCredentialsRejectInvalidAndUnavailableEncryption(t *testing.T) {
	svc, store, _, _ := setupEngine(t)
	before := store.site
	for _, login := range []LoginCredentials{{Username: "only-user"}, {Password: "only-password"}, {Username: "bad\nuser", Password: "password"}, {Username: "user", Password: "bad\x00password"}} {
		_, err := svc.UpdateSiteWithLogin(t.Context(), 1, store.site, &login)
		require.ErrorIs(t, err, ErrInvalid)
		require.Equal(t, before, store.site)
	}
	svc.durableKey = false
	_, err := svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err, "legacy empty credentials need no decryption")
	_, err = svc.UpdateSiteWithLogin(t.Context(), 1, store.site, &LoginCredentials{Username: "user", Password: "password"})
	require.ErrorIs(t, err, ErrEncryption)
	require.Equal(t, before, store.site)
	svc.durableKey = true
	store.site.LoginCipher = "invalid ciphertext"
	_, err = svc.LoginCredentials(t.Context(), 1)
	require.ErrorIs(t, err, ErrEncryption)
}

type loginCredentialConnector struct {
	*fakeConnector
	userID     int64
	loginCalls int
}

func (c *loginCredentialConnector) Login(context.Context, Site, LoginInput) (Session, *Challenge, error) {
	c.loginCalls++
	return Session{AccessToken: "verified-session", UserID: c.userID}, c.challenge, c.err
}

func TestGovernanceConnectExpectedSiteVersionBeforeRemoteLogin(t *testing.T) {
	for _, test := range []struct {
		name       string
		expected   any
		changeSite bool
		wantErr    error
	}{
		{name: "omitted remains compatible"},
		{name: "matching version", expected: int64(1)},
		{name: "origin changed after credential read", expected: int64(1), changeSite: true, wantErr: ErrConflict},
		{name: "zero is invalid", expected: int64(0), wantErr: ErrInvalid},
		{name: "negative is invalid", expected: int64(-1), wantErr: ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, store, connector, _ := setupEngine(t)
			c := &loginCredentialConnector{fakeConnector: connector, userID: 5}
			svc.connector = c
			payload := map[string]any{"username": "version-fixture-user", "password": "version-fixture-password"}
			if test.expected != nil {
				payload["expected_site_version"] = test.expected
			}
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			var input LoginInput
			require.NoError(t, json.Unmarshal(raw, &input))
			if test.changeSite {
				changed := store.site
				changed.BaseURL, changed.Platform = "https://replacement.example", "newapi"
				_, err = svc.UpdateSite(t.Context(), changed.ID, changed)
				require.NoError(t, err)
			}
			before := store.site
			_, err = svc.Connect(t.Context(), 1, input)
			if test.wantErr != nil {
				require.Zero(t, c.loginCalls, "rejected credentials must not reach any upstream")
				require.ErrorIs(t, err, test.wantErr)
				require.Equal(t, before, store.site, "a rejected version must not modify the session or saved login")
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, c.loginCalls)
			}
		})
	}
}

func TestGovernanceConnectExpectedSiteVersionPersistsAcrossTOTPContinuation(t *testing.T) {
	svc, store, connector, _ := setupEngine(t)
	c := &loginCredentialConnector{fakeConnector: connector, userID: 5}
	c.challenge = &Challenge{Kind: "totp", Token: "version-fixture-challenge"}
	svc.connector = c
	version := store.site.Version
	var initial LoginInput
	require.NoError(t, json.Unmarshal([]byte(`{"username":"version-fixture-user","password":"version-fixture-password","expected_site_version":1}`), &initial))
	result, err := svc.Connect(t.Context(), 1, initial)
	require.NoError(t, err)
	require.Equal(t, "totp", result.Challenge.Kind)
	require.Equal(t, version, store.site.Version, "staging TOTP must keep the captured site version valid")
	c.challenge = nil
	var continuation LoginInput
	require.NoError(t, json.Unmarshal([]byte(`{"otp":"123456","challenge_token":"version-fixture-challenge","expected_site_version":1}`), &continuation))
	_, err = svc.Connect(t.Context(), 1, continuation)
	require.NoError(t, err)
	require.Equal(t, 2, c.loginCalls)
	credentials, err := svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "version-fixture-user", credentials.Username)
	require.Equal(t, "version-fixture-password", credentials.Password)
}

func TestGovernanceConnectSavesVerifiedPasswordAndPreservesSameOwner(t *testing.T) {
	svc, store, connector, _ := setupEngine(t)
	c := &loginCredentialConnector{fakeConnector: connector, userID: 5}
	svc.connector = c
	_, err := svc.Connect(t.Context(), 1, LoginInput{Username: "user-a", Password: "password-a"})
	require.NoError(t, err)
	read, err := svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, LoginCredentials{Username: "user-a", Password: "password-a"}, read.LoginCredentials)
	sessionPlain, err := svc.cipher.Decrypt(store.site.SessionCipher)
	require.NoError(t, err)
	require.NotContains(t, sessionPlain, "password")
	_, err = svc.Connect(t.Context(), 1, LoginInput{SessionToken: "replacement-token", Username: "ignored", Password: "unverified-password"})
	require.NoError(t, err)
	read, err = svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "password-a", read.Password)
	c.userID = 6
	_, err = svc.Connect(t.Context(), 1, LoginInput{SessionToken: "other-user-token"})
	require.NoError(t, err)
	read, err = svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, LoginCredentials{}, read.LoginCredentials, "another dashboard identity must not retain old credentials")
}

func TestGovernanceTOTPRemembersOnlyChallengeBoundPassword(t *testing.T) {
	svc, store, connector, _ := setupEngine(t)
	_, err := svc.UpdateSiteWithLogin(t.Context(), 1, store.site, &LoginCredentials{Username: "old-user", Password: "old-password"})
	require.NoError(t, err)
	oldVersion, oldSession := store.site.Version, store.site.SessionCipher
	connector.challenge = &Challenge{Kind: "totp", Token: "challenge-token-canary"}
	_, err = svc.Connect(t.Context(), 1, LoginInput{Username: "totp-user", Password: "totp-password"})
	require.NoError(t, err)
	require.Equal(t, oldVersion, store.site.Version, "a cancelled challenge must not stale the site editor")
	require.Equal(t, oldSession, store.site.SessionCipher)
	read, err := svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "old-password", read.Password, "unverified pending details must not be revealed")
	staged, err := svc.storedLogin(store.site)
	require.NoError(t, err)
	require.NotNil(t, staged.Pending)
	require.NotEqual(t, "challenge-token-canary", staged.Pending.TokenHash)
	connector.challenge = nil
	_, err = svc.Connect(t.Context(), 1, LoginInput{OTP: "123456", ChallengeToken: "challenge-token-canary", Username: "ignored", Password: "unverified-password"})
	require.NoError(t, err)
	read, err = svc.LoginCredentials(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, LoginCredentials{Username: "totp-user", Password: "totp-password"}, read.LoginCredentials)
	stored, err := svc.storedLogin(store.site)
	require.NoError(t, err)
	require.Nil(t, stored.Pending)
	require.EqualValues(t, 5, stored.OwnerUserID)
}

func TestGovernanceTOTPDoesNotPromoteExpiredMismatchedOrRejectedOwner(t *testing.T) {
	for _, test := range []string{"expired", "mismatch", "owner-conflict"} {
		t.Run(test, func(t *testing.T) {
			svc, store, connector, _ := setupEngine(t)
			_, err := svc.UpdateSiteWithLogin(t.Context(), 1, store.site, &LoginCredentials{Username: "old-user", Password: "old-password"})
			require.NoError(t, err)
			connector.challenge = &Challenge{Kind: "totp", Token: "original-challenge"}
			_, err = svc.Connect(t.Context(), 1, LoginInput{Username: "pending-user", Password: "pending-password"})
			require.NoError(t, err)
			connector.challenge = nil
			input := LoginInput{OTP: "123456", ChallengeToken: "original-challenge"}
			if test == "expired" {
				now := svc.now().Add(16 * time.Minute)
				svc.now = func() time.Time { return now }
			} else if test == "mismatch" {
				input.ChallengeToken = "other-challenge"
			} else {
				store.keys = []ManagedKey{{SiteID: 1, OwnerUserID: 5}}
				svc.connector = &loginCredentialConnector{fakeConnector: connector, userID: 6}
			}
			before := store.site
			_, err = svc.Connect(t.Context(), 1, input)
			if test == "owner-conflict" {
				require.ErrorIs(t, err, ErrConflict)
				require.Equal(t, before, store.site)
			} else {
				require.NoError(t, err)
			}
			read, err := svc.LoginCredentials(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, "old-password", read.Password)
		})
	}
}
