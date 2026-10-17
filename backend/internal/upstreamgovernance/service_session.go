package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const (
	sessionRefreshLead         = 2 * time.Minute
	sessionRefreshBatchTimeout = 10 * time.Second
	sessionRefreshBatchLimit   = 20
	sessionRefreshScanLimit    = 1000
	sessionRefreshPageSize     = 100
)

// Runtime rotation must not change the configuration version used by previews,
// snapshots, or model runs. The ciphertext CAS also protects against stale writers.
type runtimeSessionStore interface {
	SaveRuntimeSession(context.Context, int64, int64, string, string) error
}

type sessionSitePager interface {
	ListSitesAfter(context.Context, int64, int) ([]Site, error)
}

type AuthorizationStatus struct {
	SiteID                     int64      `json:"site_id"`
	SiteVersion                int64      `json:"site_version"`
	Platform                   string     `json:"platform"`
	HasSession                 bool       `json:"has_session"`
	RefreshSupported           bool       `json:"refresh_supported"`
	HasRefreshToken            bool       `json:"has_refresh_token"`
	AutoRefreshEnabled         bool       `json:"auto_refresh_enabled"`
	AutoReauthorizationEnabled bool       `json:"auto_reauthorization_enabled"`
	AutoReauthorizationState   string     `json:"auto_reauthorization_state"`
	LastAutoReauthorizationAt  *time.Time `json:"last_auto_reauthorization_at,omitempty"`
	ExpiresAt                  *time.Time `json:"expires_at,omitempty"`
	IssuedAt                   *time.Time `json:"issued_at,omitempty"`
	RefreshState               string     `json:"refresh_state"`
	LastRefreshAt              *time.Time `json:"last_refresh_at,omitempty"`
	ReauthorizationRequired    bool       `json:"reauthorization_required"`
}

func (s *Service) refreshSupported(site Site) bool {
	_, connectorOK := s.connector.(sessionRefresher)
	_, verifierOK := s.connector.(sessionVerifier)
	_, storeOK := s.store.(runtimeSessionStore)
	return site.Platform == "sub2api" && connectorOK && verifierOK && storeOK && s.durableKey && s.cipher != nil
}

func blockedSession(session Session) bool {
	return session.RefreshState == "pending" || session.RefreshState == "reauth_required"
}

func sessionRefreshDue(session Session, now time.Time) bool {
	if session.ExpiresAt == nil {
		return false
	}
	lead := sessionRefreshLead
	if session.IssuedAt != nil {
		lifetime := session.ExpiresAt.Sub(*session.IssuedAt)
		lead = min(lead, max(time.Duration(0), lifetime/5))
	}
	return !now.Add(lead).Before(*session.ExpiresAt)
}

func (s *Service) AuthorizationStatus(ctx context.Context, id int64) (*AuthorizationStatus, error) {
	site, err := s.store.GetSite(ctx, id)
	if err != nil {
		return nil, err
	}
	result := &AuthorizationStatus{SiteID: id, SiteVersion: site.Version, Platform: site.Platform, RefreshSupported: s.refreshSupported(*site), RefreshState: "unavailable", ReauthorizationRequired: true, AutoReauthorizationState: autoReauthorizationMissingSession}
	session, err := s.session(*site)
	if err != nil {
		if errors.Is(err, ErrReauth) || errors.Is(err, ErrEncryption) {
			if !site.Enabled {
				result.AutoReauthorizationState = autoReauthorizationCollectionDisabled
			}
			return result, nil
		}
		return nil, err
	}
	result.HasSession, result.HasRefreshToken = true, session.RefreshToken != ""
	result.ExpiresAt, result.IssuedAt = session.ExpiresAt, session.IssuedAt
	result.RefreshState = session.RefreshState
	if result.RefreshState == "" {
		result.RefreshState = "ready"
	}
	if result.RefreshState == "ready" {
		result.LastRefreshAt = session.RefreshAttemptedAt
	}
	result.AutoRefreshEnabled = site.Enabled && result.RefreshSupported && result.HasRefreshToken && session.ExpiresAt != nil && !blockedSession(session)
	result.AutoReauthorizationEnabled, result.AutoReauthorizationState = s.autoReauthorizationState(*site, session)
	result.LastAutoReauthorizationAt = session.LastAutoReauthorizationAt
	result.ReauthorizationRequired = blockedSession(session) || site.Status == "reauth_required" || session.ExpiresAt != nil && !s.now().Before(*session.ExpiresAt) && !result.AutoRefreshEnabled
	return result, nil
}

func (s *Service) saveRuntimeSessionLocked(ctx context.Context, site *Site, session Session) error {
	store, ok := s.store.(runtimeSessionStore)
	if !ok {
		return ErrUnsupported
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return ErrEncryption
	}
	encrypted, err := s.cipher.Encrypt(string(raw))
	if err != nil {
		return ErrEncryption
	}
	if err = store.SaveRuntimeSession(ctx, site.ID, site.Version, site.SessionCipher, encrypted); err != nil {
		return err
	}
	site.SessionCipher = encrypted
	return nil
}

func (s *Service) requireAuthorizationLocked(ctx context.Context, site *Site, session Session) error {
	if _, ok := s.store.(runtimeSessionStore); ok {
		session.RefreshState = "reauth_required"
		if err := s.saveRuntimeSessionLocked(ctx, site, session); err != nil {
			return err
		}
	}
	if err := s.store.ObserveSite(ctx, site.ID, "reauth_required", "reauth_required", time.Time{}, site.NextSyncAt); err != nil {
		return err
	}
	site.Status, site.LastError = "reauth_required", "reauth_required"
	return ErrReauth
}

// Caller holds the per-site advisory lock and an outbound-operation slot.
func (s *Service) managementSessionLocked(ctx context.Context, site *Site, force bool) (Session, error) {
	session, err := s.session(*site)
	if err != nil {
		return Session{}, err
	}
	if blockedSession(session) {
		return Session{}, ErrReauth
	}
	if session.RefreshState == "identity_pending" {
		return s.verifyRotatedSessionLocked(ctx, site, session)
	}
	expired := session.ExpiresAt != nil && !s.now().Before(*session.ExpiresAt)
	due := sessionRefreshDue(session, s.now())
	if !force && !due {
		return session, nil
	}
	if session.RefreshToken == "" || !s.refreshSupported(*site) {
		if expired || force {
			return Session{}, s.requireAuthorizationLocked(ctx, site, session)
		}
		return session, nil
	}
	if ctx.Err() != nil {
		return Session{}, ctx.Err()
	}
	attempted := s.now()
	session.RefreshState, session.RefreshAttemptedAt = "pending", &attempted
	if err = s.saveRuntimeSessionLocked(ctx, site, session); err != nil {
		return Session{}, err
	}
	rotated, err := s.connector.(sessionRefresher).Refresh(ctx, *site, session)
	// A completed rotation must survive cancellation of the initiating request.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err != nil {
		return Session{}, s.requireAuthorizationLocked(persistCtx, site, session)
	}
	if rotated.AccessToken == "" || rotated.RefreshToken == "" || rotated.UserID != session.UserID || rotated.UserAgent != session.UserAgent || rotated.ExpiresAt == nil || !s.now().Before(*rotated.ExpiresAt) {
		return Session{}, s.requireAuthorizationLocked(persistCtx, site, session)
	}
	rotated.RefreshState, rotated.RefreshAttemptedAt = "identity_pending", &attempted
	if err = s.saveRuntimeSessionLocked(persistCtx, site, rotated); err != nil {
		return Session{}, err
	}
	return s.verifyRotatedSessionLocked(persistCtx, site, rotated)
}

// A crash or transient profile failure may retry this read, never token rotation.
func (s *Service) verifyRotatedSessionLocked(ctx context.Context, site *Site, session Session) (Session, error) {
	verifier, ok := s.connector.(sessionVerifier)
	if !ok {
		return Session{}, ErrUnsupported
	}
	verified, err := verifier.VerifySession(ctx, *site, session)
	if err != nil && !errors.Is(err, ErrReauth) {
		return Session{}, err
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err != nil || verified.UserID != session.UserID {
		return Session{}, s.requireAuthorizationLocked(persistCtx, site, session)
	}
	session.RefreshState = "ready"
	if err = s.saveRuntimeSessionLocked(persistCtx, site, session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Service) refreshDueSessions(ctx context.Context) error {
	if _, ok := s.store.(runtimeSessionStore); !ok {
		return nil
	}
	if !s.sessionRefreshMu.TryLock() {
		return nil
	}
	defer s.sessionRefreshMu.Unlock()
	var firstErr error
	attempts := 0
	err := s.scanRefreshSites(ctx, func(candidate Site) bool {
		if !candidate.Enabled {
			return true
		}
		session, err := s.session(candidate)
		if err != nil {
			return true
		}
		refreshDue := s.refreshSupported(candidate) && session.RefreshToken != "" && !blockedSession(session) && (session.RefreshState == "identity_pending" || sessionRefreshDue(session, s.now()))
		_, autoState := s.autoReauthorizationState(candidate, session)
		autoDue := autoState == autoReauthorizationReady && (blockedSession(session) || session.ExpiresAt != nil && !s.now().Before(*session.ExpiresAt))
		if !refreshDue && !autoDue {
			return true
		}
		attempts++
		err = s.refreshSiteSession(ctx, candidate.ID)
		if err != nil && !errors.Is(err, ErrBusy) && firstErr == nil {
			firstErr = err
		}
		return attempts < sessionRefreshBatchLimit
	})
	if err != nil {
		return err
	}
	return firstErr
}

func (s *Service) scanRefreshSites(ctx context.Context, visit func(Site) bool) error {
	startCursor := s.sessionRefreshCursor
	if pager, ok := s.store.(sessionSitePager); ok {
		after, scanned, wrapped := startCursor, 0, false
		for scanned < sessionRefreshScanLimit {
			if err := ctx.Err(); err != nil {
				return err
			}
			page, err := pager.ListSitesAfter(ctx, after, min(sessionRefreshPageSize, sessionRefreshScanLimit-scanned))
			if err != nil {
				return err
			}
			if len(page) == 0 {
				if wrapped || startCursor == 0 {
					return nil
				}
				after, wrapped = 0, true
				continue
			}
			for _, candidate := range page {
				if wrapped && candidate.ID > startCursor {
					return nil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				s.sessionRefreshCursor = candidate.ID
				after = candidate.ID
				scanned++
				if !visit(candidate) {
					return nil
				}
			}
		}
		return nil
	}
	sites, err := s.store.ListSites(ctx)
	if err != nil {
		return err
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	start := sort.Search(len(sites), func(i int) bool { return sites[i].ID > startCursor })
	for offset := 0; offset < len(sites) && offset < sessionRefreshScanLimit; offset++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidate := sites[(start+offset)%len(sites)]
		s.sessionRefreshCursor = candidate.ID
		if !visit(candidate) {
			return nil
		}
	}
	return nil
}

func (s *Service) refreshSiteSession(ctx context.Context, id int64) error {
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return err
	}
	defer free()
	site, release, err := s.remoteSiteLock(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if !site.Enabled {
		return nil
	}
	_, err = s.managementSessionLocked(ctx, site, false)
	if errors.Is(err, ErrReauth) {
		_, err = s.autoReauthorizeLocked(ctx, site)
		if err == nil {
			// The scanner must not move collection's already-reserved next_sync_at.
			err = s.store.ObserveSite(ctx, site.ID, "connected", "", time.Time{}, site.NextSyncAt)
		}
	}
	return err
}

// ImportBrowserSession accepts only captures returned by the trusted browser
// helper. Public JSON inputs must use Connect, not this credentials promotion path.
func (s *Service) ImportBrowserSession(ctx context.Context, id, expectedVersion int64, session Session, verifiedLogin *LoginCredentials) (*ConnectResult, error) {
	if !s.durableKey || s.cipher == nil {
		return nil, ErrEncryption
	}
	if expectedVersion <= 0 || session.UserID < 0 || !hasSessionCredential(session) || !validHeaderValue(session.AccessToken, maxSessionTokenLength) || !validHeaderValue(session.RefreshToken, maxSessionTokenLength) || session.UserAgent == "" || !validHeaderValue(session.UserAgent, 1024) {
		return nil, ErrInvalid
	}
	if session.ExpiresAt != nil && (!s.now().Before(*session.ExpiresAt) || session.ExpiresAt.After(s.now().Add(time.Duration(maxSessionLifetimeSeconds)*time.Second))) || session.IssuedAt != nil && session.IssuedAt.After(s.now().Add(time.Minute)) {
		return nil, ErrInvalid
	}
	for key, value := range session.Cookies {
		if !sessionCookieAllowed(key) || !validSessionCookie(value) {
			return nil, ErrInvalid
		}
	}
	if verifiedLogin != nil {
		normalized, err := normalizeLoginCredentials(*verifiedLogin)
		if err != nil {
			return nil, err
		}
		verifiedLogin = &normalized
	}
	verifier, ok := s.connector.(sessionVerifier)
	if !ok {
		return nil, ErrUnsupported
	}
	free, err := s.remoteSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer free()
	site, release, err := s.remoteSiteLock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	if site.Version != expectedVersion {
		return nil, ErrConflict
	}
	verified, err := verifier.VerifySession(ctx, *site, session)
	if err != nil {
		return nil, err
	}
	if verified.UserID <= 0 || session.UserID > 0 && verified.UserID != session.UserID {
		return nil, ErrReauth
	}
	verified.RefreshState, verified.RefreshAttemptedAt = "ready", nil
	if verified.IssuedAt == nil {
		now := s.now()
		verified.IssuedAt = &now
	}
	// SessionToken selects the existing token-import credential-preservation rule
	// even when a New API browser capture is cookie-authenticated.
	return s.connectSessionLocked(ctx, site, LoginInput{SessionToken: "browser-import"}, verified, verifiedLogin)
}
