package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

const (
	autoReauthorizationReady                = "ready"
	autoReauthorizationCollectionDisabled   = "collection_disabled"
	autoReauthorizationMissingSession       = "missing_session"
	autoReauthorizationMissingCredentials   = "missing_credentials"
	autoReauthorizationIdentityMismatch     = "identity_mismatch"
	autoReauthorizationVerificationRequired = "verification_required"
	autoReauthorizationCredentialsRejected  = "credentials_rejected"
	autoReauthorizationRetryWait            = "retry_wait"
)

func loginCipherHash(ciphertext string) string {
	sum := sha256.Sum256([]byte(ciphertext))
	return hex.EncodeToString(sum[:])
}

func terminalAutoReauthorizationState(state string) bool {
	return state == autoReauthorizationIdentityMismatch || state == autoReauthorizationVerificationRequired || state == autoReauthorizationCredentialsRejected
}

func (s *Service) autoReauthorizationState(site Site, session Session) (bool, string) {
	if !site.Enabled {
		return false, autoReauthorizationCollectionDisabled
	}
	if !s.durableKey || s.cipher == nil {
		return false, autoReauthorizationMissingCredentials
	}
	if _, ok := s.store.(runtimeSessionStore); !ok {
		return false, autoReauthorizationMissingCredentials
	}
	if !hasSessionCredential(session) {
		return false, autoReauthorizationMissingSession
	}
	login, err := s.storedLogin(site)
	if err != nil || login.Username == "" || login.Password == "" {
		return false, autoReauthorizationMissingCredentials
	}
	if session.UserID <= 0 || login.OwnerUserID <= 0 || login.OwnerUserID != session.UserID {
		return false, autoReauthorizationIdentityMismatch
	}
	if session.AutoReauthorizationCredentialHash == loginCipherHash(site.LoginCipher) {
		if terminalAutoReauthorizationState(session.AutoReauthorizationState) {
			return false, session.AutoReauthorizationState
		}
		if session.AutoReauthorizationRetryAt != nil && s.now().Before(*session.AutoReauthorizationRetryAt) {
			return true, autoReauthorizationRetryWait
		}
	}
	return true, autoReauthorizationReady
}

// The caller owns the site lock and remote slot. Password login is a fresh
// authorization, never a replay of a possibly consumed refresh token.
func (s *Service) autoReauthorizeLocked(ctx context.Context, site *Site) (Session, error) {
	old, err := s.session(*site)
	if err != nil {
		return Session{}, err
	}
	_, state := s.autoReauthorizationState(*site, old)
	if state != autoReauthorizationReady {
		return Session{}, ErrReauth
	}
	login, err := s.storedLogin(*site)
	if err != nil {
		return Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	attempted := s.now()
	hash := loginCipherHash(site.LoginCipher)
	attempt := old
	failures := 1
	if old.AutoReauthorizationCredentialHash == hash {
		failures = min(old.AutoReauthorizationFailures+1, 5)
	}
	retryAt := attempted.Add(time.Duration(1<<uint(failures-1)) * 5 * time.Minute)
	attempt.AutoReauthorizationState = autoReauthorizationRetryWait
	attempt.AutoReauthorizationCredentialHash = hash
	attempt.LastAutoReauthorizationAt = &attempted
	attempt.AutoReauthorizationFailures = failures
	attempt.AutoReauthorizationRetryAt = &retryAt
	if err := s.saveRuntimeSessionLocked(ctx, site, attempt); err != nil {
		return Session{}, err
	}
	input := LoginInput{Username: login.Username, Password: login.Password, UserAgent: old.UserAgent}
	newSession, challenge, loginErr := s.connector.Login(ctx, *site, input)
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if loginErr == nil && challenge == nil && hasSessionCredential(newSession) && newSession.UserID == old.UserID && newSession.UserID == login.OwnerUserID && (newSession.ExpiresAt == nil || s.now().Before(*newSession.ExpiresAt)) {
		keys, err := s.store.ListManagedKeys(persistCtx, site.ID)
		if err != nil {
			return Session{}, err
		}
		for _, key := range keys {
			if key.OwnerUserID != newSession.UserID {
				return s.recordAutoReauthorizationFailure(persistCtx, site, attempt, hash, attempted, autoReauthorizationIdentityMismatch)
			}
		}
		newSession.RefreshState = "ready"
		newSession.LastAutoReauthorizationAt = &attempted
		newSession.AutoReauthorizationState = autoReauthorizationReady
		newSession.AutoReauthorizationCredentialHash = hash
		if err := s.saveRuntimeSessionLocked(persistCtx, site, newSession); err != nil {
			return Session{}, err
		}
		return newSession, nil
	}

	switch {
	case challenge != nil || errors.Is(loginErr, errConnectorCaptcha):
		state = autoReauthorizationVerificationRequired
	case loginErr == nil:
		state = autoReauthorizationIdentityMismatch
	case errors.Is(loginErr, ErrReauth) || errors.Is(loginErr, errConnectorEnvelopeDenied) || errors.Is(loginErr, errConnectorDenied) || errors.Is(loginErr, ErrUnsupported):
		state = autoReauthorizationCredentialsRejected
	default:
		state = autoReauthorizationRetryWait
	}
	return s.recordAutoReauthorizationFailure(persistCtx, site, attempt, hash, attempted, state)
}

func (s *Service) recordAutoReauthorizationFailure(ctx context.Context, site *Site, session Session, hash string, at time.Time, state string) (Session, error) {
	previousState, previousHash := session.AutoReauthorizationState, session.AutoReauthorizationCredentialHash
	session.LastAutoReauthorizationAt = &at
	session.AutoReauthorizationState = state
	session.AutoReauthorizationCredentialHash = hash
	if state != autoReauthorizationRetryWait {
		session.AutoReauthorizationRetryAt = nil
		session.AutoReauthorizationFailures = 0
	}
	if err := s.saveRuntimeSessionLocked(ctx, site, session); err != nil {
		return Session{}, err
	}
	if terminalAutoReauthorizationState(state) && (previousState != state || previousHash != hash) {
		if err := s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "auto_reauthorization_required", After: state, CreatedAt: at}); err != nil {
			return Session{}, err
		}
	}
	return Session{}, ErrReauth
}
