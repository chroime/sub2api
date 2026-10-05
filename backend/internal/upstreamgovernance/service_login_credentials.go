package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// OwnerUserID associates saved details with the authenticated dashboard identity.
// Pending details never leave this encrypted record and are only promoted after
// the matching challenge succeeds and existing resource ownership is checked.
type storedLoginCredentials struct {
	LoginCredentials
	OwnerUserID int64                  `json:"owner_user_id,omitempty"`
	Pending     *pendingLoginChallenge `json:"pending,omitempty"`
}

type pendingLoginChallenge struct {
	LoginCredentials
	TokenHash string    `json:"token_hash"`
	ExpiresAt time.Time `json:"expires_at"`
}

func normalizeLoginCredentials(input LoginCredentials) (LoginCredentials, error) {
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) > 320 || len(input.Password) > 4096 || strings.ContainsFunc(input.Username, unicode.IsControl) || strings.ContainsRune(input.Password, '\x00') || (input.Username == "") != (input.Password == "") {
		return LoginCredentials{}, ErrInvalid
	}
	return input, nil
}

func (s *Service) storedLogin(site Site) (storedLoginCredentials, error) {
	var value storedLoginCredentials
	if site.LoginCipher == "" {
		return value, nil
	}
	if !s.durableKey || s.cipher == nil {
		return value, ErrEncryption
	}
	raw, err := s.cipher.Decrypt(site.LoginCipher)
	if err != nil || json.Unmarshal([]byte(raw), &value) != nil {
		return storedLoginCredentials{}, ErrEncryption
	}
	if _, err = normalizeLoginCredentials(value.LoginCredentials); err != nil || value.OwnerUserID < 0 {
		return storedLoginCredentials{}, ErrEncryption
	}
	return value, nil
}

func (s *Service) encryptLogin(value storedLoginCredentials) (string, error) {
	if value.Username == "" && value.Password == "" && value.Pending == nil {
		return "", nil
	}
	if !s.durableKey || s.cipher == nil {
		return "", ErrEncryption
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", ErrEncryption
	}
	encrypted, err := s.cipher.Encrypt(string(raw))
	if err != nil {
		return "", ErrEncryption
	}
	return encrypted, nil
}

func (s *Service) LoginCredentials(ctx context.Context, siteID int64) (*LoginCredentialsResult, error) {
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	value, err := s.storedLogin(*site)
	if err != nil {
		return nil, err
	}
	return &LoginCredentialsResult{LoginCredentials: value.LoginCredentials, Version: site.Version}, nil
}

func loginChallengeHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// Only the first password step actually verifies the supplied password. TOTP
// and session-token endpoints ignore any username/password included alongside.
func passwordLoginCredentials(input LoginInput) (*LoginCredentials, error) {
	if input.SessionToken != "" || input.ChallengeToken != "" || input.Username == "" || input.Password == "" {
		return nil, nil
	}
	value, err := normalizeLoginCredentials(LoginCredentials{Username: input.Username, Password: input.Password})
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *Service) stageLoginChallenge(ctx context.Context, site Site, input LoginInput, challenge *Challenge) error {
	if challenge == nil || challenge.Kind != "totp" || challenge.Token == "" {
		return nil
	}
	credentials, err := passwordLoginCredentials(input)
	if err != nil || credentials == nil {
		return err
	}
	value, err := s.storedLogin(site)
	if err != nil {
		return err
	}
	value.Pending = &pendingLoginChallenge{LoginCredentials: *credentials, TokenHash: loginChallengeHash(challenge.Token), ExpiresAt: s.now().Add(15 * time.Minute)}
	encrypted, err := s.encryptLogin(value)
	if err != nil {
		return err
	}
	// Staging is runtime state. It must not stale an open site editor when the
	// administrator cancels TOTP; the advisory lock and CAS still protect writes.
	return s.store.StageLoginChallenge(ctx, site.ID, site.Version, encrypted)
}

func (s *Service) loginAfterConnect(site Site, input LoginInput, session Session) (string, error) {
	credentials, err := passwordLoginCredentials(input)
	if err != nil {
		return "", err
	}
	if credentials != nil {
		return s.encryptLogin(storedLoginCredentials{LoginCredentials: *credentials, OwnerUserID: session.UserID})
	}
	value, err := s.storedLogin(site)
	if err != nil {
		return "", err
	}
	if input.SessionToken == "" && input.ChallengeToken != "" && value.Pending != nil && value.Pending.TokenHash == loginChallengeHash(input.ChallengeToken) && s.now().Before(value.Pending.ExpiresAt) {
		return s.encryptLogin(storedLoginCredentials{LoginCredentials: value.Pending.LoginCredentials, OwnerUserID: session.UserID})
	}
	owner := value.OwnerUserID
	if owner == 0 {
		if previous, err := s.session(site); err == nil {
			owner = previous.UserID
		}
	}
	if owner <= 0 || session.UserID != owner {
		value.LoginCredentials = LoginCredentials{}
		value.OwnerUserID = 0
	}
	value.Pending = nil
	return s.encryptLogin(value)
}
