package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	openAICodexTicketRuntimePrefix = "codex_ticket_runtime:"
	openAICodexTicketStateLimit    = 2048
	openAICodexTicketEventLimit    = 300
)

type OpenAICodexTicketMonitorState struct {
	Mode          string     `json:"mode"`
	AccountID     int64      `json:"account_id"`
	AccountName   string     `json:"account_name"`
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	Phase         string     `json:"phase"`
	ProxyID       *int64     `json:"proxy_id,omitempty"`
	ProxyName     string     `json:"proxy_name"`
	Length        int        `json:"length"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	LastError     string     `json:"last_error"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Uses          uint64     `json:"uses"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}

type OpenAICodexTicketMonitorEvent struct {
	ID            uint64     `json:"id"`
	Time          time.Time  `json:"time"`
	Mode          string     `json:"mode"`
	AccountID     int64      `json:"account_id"`
	AccountName   string     `json:"account_name"`
	Model         string     `json:"model"`
	Phase         string     `json:"phase"`
	Status        string     `json:"status"`
	ProxyID       *int64     `json:"proxy_id,omitempty"`
	ProxyName     string     `json:"proxy_name"`
	HTTPStatus    int        `json:"http_status"`
	Length        int        `json:"length"`
	ErrorCode     string     `json:"error_code"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

type OpenAICodexTicketMonitorSnapshot struct {
	UpdatedAt time.Time                       `json:"updated_at"`
	States    []OpenAICodexTicketMonitorState `json:"states"`
	Events    []OpenAICodexTicketMonitorEvent `json:"events"`
}

type openAICodexTicketRuntime struct {
	CredentialHash string    `json:"credential_hash"`
	NextAttemptAt  time.Time `json:"next_attempt_at,omitempty"`
	AuthBlocked    bool      `json:"auth_blocked,omitempty"`
	PreferredRoute string    `json:"preferred_route,omitempty"`
	FailedRoute    string    `json:"failed_route,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastError      string    `json:"last_error,omitempty"`
}

type openAICodexTicketRevocation struct{ IssuedAt, ExpiresAt time.Time }

// The immutable snapshot identifies precisely the material sent by this request.
// It deliberately stays private: none of its credential/state fields are DTOs.
type openAICodexTicketUse struct {
	mode, model, state, credentialHash string
	accountID                          int64
	accountName                        string
	issuedAt, expiresAt                time.Time
}

type openAICodexTicketInvalidator interface {
	InvalidateCodexTicket(context.Context, int64, string, string, string, string) (bool, error)
}

func (s *OpenAIGatewayService) SetCodexTicketProxyRepository(repo ProxyRepository) {
	if s == nil {
		return
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	s.openaiCodexTicketProxyRepo = repo
	s.openaiCodexTicketRuntimeMu.Unlock()
}

func (s *OpenAIGatewayService) GetOpenAICodexTicketMonitor() OpenAICodexTicketMonitorSnapshot {
	out := OpenAICodexTicketMonitorSnapshot{UpdatedAt: time.Now(), States: []OpenAICodexTicketMonitorState{}, Events: []OpenAICodexTicketMonitorEvent{}}
	if s == nil {
		return out
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	for _, state := range s.openaiCodexTicketMonitorStates {
		out.States = append(out.States, state)
	}
	out.Events = append(out.Events, s.openaiCodexTicketEvents...)
	sort.Slice(out.States, func(i, j int) bool { return out.States[i].UpdatedAt.After(out.States[j].UpdatedAt) })
	return out
}

func codexTicketMonitorError(code string) string {
	switch code {
	case "", "credential_missing", "invalid_ticket", "credential_mismatch", "network_error", "http_error", "stream_invalid", "stream_incomplete", "model_mismatch", "rate_limited", "auth_failed", "proxy_unavailable", "persist_failed", "cancelled":
		return code
	default:
		return "http_error"
	}
}

func (s *OpenAIGatewayService) recordOpenAICodexTicketEvent(account *Account, mode, model, phase, status, code string, httpStatus int, route *openAICodexTicketRoute, ticket *openAICodexTicket, next time.Time) {
	if s == nil || account == nil {
		return
	}
	key := openAICodexTicketModeKey(mode, account.ID, model)
	now := time.Now()
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	if s.openaiCodexTicketMonitorStates == nil {
		s.openaiCodexTicketMonitorStates = make(map[string]OpenAICodexTicketMonitorState)
	}
	state := s.openaiCodexTicketMonitorStates[key]
	state.Mode, state.AccountID, state.AccountName, state.Model = mode, account.ID, account.Name, model
	state.Phase, state.Status, state.LastError, state.UpdatedAt = phase, status, codexTicketMonitorError(code), now
	state.NextAttemptAt = nil
	if !next.IsZero() {
		state.NextAttemptAt = &next
	}
	if route != nil {
		state.ProxyID, state.ProxyName = route.ID, route.Name
	}
	if ticket != nil {
		state.Length = ticket.Length
		exp := ticket.ExpiresAt
		state.ExpiresAt = &exp
	}
	s.openaiCodexTicketMonitorStates[key] = state
	if len(s.openaiCodexTicketMonitorStates) > openAICodexTicketStateLimit {
		var oldest string
		var earliest time.Time
		for k, v := range s.openaiCodexTicketMonitorStates {
			if oldest == "" || v.UpdatedAt.Before(earliest) {
				oldest, earliest = k, v.UpdatedAt
			}
		}
		delete(s.openaiCodexTicketMonitorStates, oldest)
	}
	s.openaiCodexTicketEventID++
	event := OpenAICodexTicketMonitorEvent{ID: s.openaiCodexTicketEventID, Time: now, Mode: mode, AccountID: account.ID, AccountName: account.Name, Model: model, Phase: phase, Status: status, ProxyID: state.ProxyID, ProxyName: state.ProxyName, HTTPStatus: httpStatus, Length: state.Length, ErrorCode: state.LastError, NextAttemptAt: state.NextAttemptAt}
	if len(s.openaiCodexTicketEvents) >= openAICodexTicketEventLimit {
		copy(s.openaiCodexTicketEvents, s.openaiCodexTicketEvents[1:])
		s.openaiCodexTicketEvents = s.openaiCodexTicketEvents[:openAICodexTicketEventLimit-1]
	}
	s.openaiCodexTicketEvents = append(s.openaiCodexTicketEvents, event)
}

func (s *OpenAIGatewayService) loadOpenAICodexTicketRuntime(account *Account, mode, model string) openAICodexTicketRuntime {
	key, hash := openAICodexTicketModeKey(mode, account.ID, model), OpenAICodexTicketCredentialHash(account)
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	state := s.openaiCodexTicketRuntime[key]
	if state.CredentialHash != hash {
		state = openAICodexTicketRuntime{CredentialHash: hash}
	}
	var persisted openAICodexTicketRuntime
	if raw, err := json.Marshal(account.Extra[openAICodexTicketRuntimePrefix+mode+":"+model]); err == nil {
		if json.Unmarshal(raw, &persisted) == nil && hash != "" && persisted.CredentialHash == hash {
			local := state
			if persisted.UpdatedAt.After(state.UpdatedAt) {
				state = persisted
			}
			// Cross-instance merges may strengthen safety fields without changing
			// the metadata timestamp. Route ordering must not weaken suspension.
			state.AuthBlocked = local.AuthBlocked || persisted.AuthBlocked
			if local.NextAttemptAt.After(state.NextAttemptAt) {
				state.NextAttemptAt = local.NextAttemptAt
			}
			if persisted.NextAttemptAt.After(state.NextAttemptAt) {
				state.NextAttemptAt = persisted.NextAttemptAt
			}
			if s.openaiCodexTicketRuntime == nil {
				s.openaiCodexTicketRuntime = make(map[string]openAICodexTicketRuntime)
			}
			s.openaiCodexTicketRuntime[key] = state
			s.pruneOpenAICodexTicketRuntimeLocked()
		}
	}
	return state
}

func (s *OpenAIGatewayService) saveOpenAICodexTicketRuntime(ctx context.Context, account *Account, mode, model string, state openAICodexTicketRuntime) {
	if s.setOpenAICodexTicketRuntime(account, mode, model, state) {
		s.persistOpenAICodexTicketRuntime(ctx, account, mode, model, state.CredentialHash)
	}
}

func (s *OpenAIGatewayService) setOpenAICodexTicketRuntime(account *Account, mode, model string, state openAICodexTicketRuntime) bool {
	state.LastError = codexTicketMonitorError(state.LastError)
	key := openAICodexTicketModeKey(mode, account.ID, model)
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	if s.openaiCodexTicketRuntime == nil {
		s.openaiCodexTicketRuntime = make(map[string]openAICodexTicketRuntime)
	}
	current := s.openaiCodexTicketRuntime[key]
	// Response observers carry only the original credential projection. They
	// cannot reset runtime state established by a subsequent credential capture.
	if account.GetCredential("access_token") == "" && current.CredentialHash != "" && current.CredentialHash != state.CredentialHash {
		return false
	}
	if current.CredentialHash == state.CredentialHash && current.UpdatedAt.After(state.UpdatedAt) {
		state.AuthBlocked = state.AuthBlocked || current.AuthBlocked
		if current.NextAttemptAt.After(state.NextAttemptAt) {
			state.NextAttemptAt = current.NextAttemptAt
			state.LastError = current.LastError
		}
	}
	state.UpdatedAt = time.Now()
	s.openaiCodexTicketRuntime[key] = state
	if s.openaiCodexTicketDirty == nil {
		s.openaiCodexTicketDirty = make(map[string]bool)
	}
	s.openaiCodexTicketDirty[key] = true
	s.pruneOpenAICodexTicketRuntimeLocked()
	return true
}

func (s *OpenAIGatewayService) pruneOpenAICodexTicketRuntimeLocked() {
	for len(s.openaiCodexTicketRuntime) > openAICodexTicketStateLimit {
		var oldest string
		var earliest time.Time
		for k, v := range s.openaiCodexTicketRuntime {
			if oldest == "" || v.UpdatedAt.Before(earliest) {
				oldest, earliest = k, v.UpdatedAt
			}
		}
		delete(s.openaiCodexTicketRuntime, oldest)
		delete(s.openaiCodexTicketDirty, oldest)
	}
}

func (s *OpenAIGatewayService) persistOpenAICodexTicketRuntime(ctx context.Context, account *Account, mode, model, credentialHash string) bool {
	if ctx.Err() != nil {
		return false
	}
	key := openAICodexTicketModeKey(mode, account.ID, model)
	unlock, locked := s.lockOpenAICodexTicketPersistence(ctx, mode, account.ID, model)
	if !locked {
		return false
	}
	defer unlock()
	if ctx.Err() != nil {
		return false
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	state, ok := s.openaiCodexTicketRuntime[key]
	s.openaiCodexTicketRuntimeMu.Unlock()
	if !ok || state.CredentialHash != credentialHash {
		return true
	}
	if s.accountRepo != nil {
		writeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var err error
		if repo, ok := s.accountRepo.(interface {
			UpdateCodexTicketRuntime(context.Context, int64, string, string, map[string]any) error
		}); ok {
			raw, _ := json.Marshal(state)
			var data map[string]any
			_ = json.Unmarshal(raw, &data)
			err = repo.UpdateCodexTicketRuntime(writeCtx, account.ID, mode, model, data)
		} else {
			err = s.accountRepo.UpdateExtra(writeCtx, account.ID, map[string]any{openAICodexTicketRuntimePrefix + mode + ":" + model: state})
		}
		if err != nil {
			s.recordOpenAICodexTicketEvent(account, mode, model, "persist", "error", "persist_failed", 0, nil, nil, time.Time{})
			return false
		}
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	if latest := s.openaiCodexTicketRuntime[key]; latest.UpdatedAt.Equal(state.UpdatedAt) {
		delete(s.openaiCodexTicketDirty, key)
	}
	s.openaiCodexTicketRuntimeMu.Unlock()
	return true
}

func (s *OpenAIGatewayService) lockOpenAICodexTicketPersistence(ctx context.Context, mode string, accountID int64, model string) (func(), bool) {
	digest := sha256.Sum256([]byte(openAICodexTicketModeKey(mode, accountID, model)))
	lock := &s.openaiCodexTicketPersistLocks[int(digest[0])%len(s.openaiCodexTicketPersistLocks)]
	for !lock.TryLock() {
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(5 * time.Millisecond):
		}
	}
	return lock.Unlock, true
}

func codexTicketRevocationKey(mode string, accountID int64, model, state, hash string) string {
	digest := sha256.Sum256([]byte(state))
	return openAICodexTicketModeKey(mode, accountID, model) + "\x00" + hash + "\x00" + hex.EncodeToString(digest[:])
}

func (s *OpenAIGatewayService) openAICodexTicketRevoked(mode string, ticket *openAICodexTicket) bool {
	if ticket == nil {
		return false
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	if !s.openaiCodexTicketRejectIssuedBefore.IsZero() && !ticket.IssuedAt.After(s.openaiCodexTicketRejectIssuedBefore) {
		return true
	}
	_, revoked := s.openaiCodexTicketRevocations[codexTicketRevocationKey(mode, ticket.AccountID, ticket.Model, ticket.State, ticket.CredentialHash)]
	return revoked
}

func (s *OpenAIGatewayService) snapshotOpenAICodexTicketUse(ctx context.Context, account *Account, model string, headers http.Header) *openAICodexTicketUse {
	if s == nil || account == nil {
		return nil
	}
	mode := OpenAICodexTicketMode(account)
	if !openAICodexTicketGatesModel(s.openAICodexTicketRuntimeConfig(ctx, mode), model) {
		return nil
	}
	ticket := s.lookupOpenAICodexTicket(account, model)
	value, hash := openAICodexTicketHeaderValue(headers, openAICodexTurnStateHeader), openAICodexTicketHeaderHash(headers)
	if hash == "" || hash != OpenAICodexTicketCredentialHash(account) {
		return nil
	}
	var use *openAICodexTicketUse
	if ticket != nil && value == ticket.State && hash == ticket.CredentialHash {
		use = &openAICodexTicketUse{mode: mode, model: model, state: ticket.State, credentialHash: ticket.CredentialHash, accountID: account.ID, accountName: account.Name, issuedAt: ticket.IssuedAt, expiresAt: ticket.ExpiresAt}
	} else {
		s.openaiCodexTicketRuntimeMu.Lock()
		remembered := s.openaiCodexTicketInjectedUses[codexTicketRevocationKey(mode, account.ID, model, value, hash)]
		if remembered != nil && time.Now().Before(remembered.expiresAt) {
			copy := *remembered
			use = &copy
		}
		s.openaiCodexTicketRuntimeMu.Unlock()
	}
	if use == nil {
		return nil
	}
	key := openAICodexTicketModeKey(mode, account.ID, model)
	s.openaiCodexTicketRuntimeMu.Lock()
	if state, ok := s.openaiCodexTicketMonitorStates[key]; ok {
		now := time.Now()
		state.Uses++
		state.LastUsedAt = &now
		state.UpdatedAt = now
		s.openaiCodexTicketMonitorStates[key] = state
	}
	s.openaiCodexTicketRuntimeMu.Unlock()
	return use
}

func (s *OpenAIGatewayService) observeOpenAICodexTicketUse(ctx context.Context, use *openAICodexTicketUse, status int, headers http.Header) {
	if s == nil || use == nil || (status != 401 && status != 403 && status != 429) {
		return
	}
	account := &Account{ID: use.accountID, Name: use.accountName, Extra: map[string]any{openAICodexTicketCredentialHashKey: use.credentialHash}}
	if status == 401 || status == 403 {
		s.openaiCodexTicketRuntimeMu.Lock()
		if s.openaiCodexTicketRevocations == nil {
			s.openaiCodexTicketRevocations = make(map[string]openAICodexTicketRevocation)
		}
		rkey := codexTicketRevocationKey(use.mode, use.accountID, use.model, use.state, use.credentialHash)
		_, duplicate := s.openaiCodexTicketRevocations[rkey]
		s.openaiCodexTicketRevocations[rkey] = openAICodexTicketRevocation{IssuedAt: use.issuedAt, ExpiresAt: use.expiresAt}
		for key, value := range s.openaiCodexTicketRevocations {
			if !time.Now().Before(value.ExpiresAt) {
				delete(s.openaiCodexTicketRevocations, key)
			}
		}
		if len(s.openaiCodexTicketRevocations) > openAICodexTicketStateLimit {
			var earliest time.Time
			for _, value := range s.openaiCodexTicketRevocations {
				if earliest.IsZero() || value.IssuedAt.Before(earliest) {
					earliest = value.IssuedAt
				}
			}
			if earliest.After(s.openaiCodexTicketRejectIssuedBefore) {
				s.openaiCodexTicketRejectIssuedBefore = earliest
			}
			for key, value := range s.openaiCodexTicketRevocations {
				if !value.IssuedAt.After(earliest) {
					delete(s.openaiCodexTicketRevocations, key)
				}
			}
		}
		s.openaiCodexTicketRuntimeMu.Unlock()
		key := openAICodexTicketModeKey(use.mode, use.accountID, use.model)
		if raw, ok := s.openaiCodexTickets.Load(key); ok {
			if ticket, ok := raw.(*openAICodexTicket); ok && ticket.State == use.state && ticket.CredentialHash == use.credentialHash {
				s.openaiCodexTickets.CompareAndDelete(key, raw)
			}
		}
		s.openaiCodexTicketRuntimeMu.Lock()
		if !duplicate {
			if s.openaiCodexTicketPendingRevocations == nil {
				s.openaiCodexTicketPendingRevocations = make(map[string]*openAICodexTicketUse)
			}
			s.openaiCodexTicketPendingRevocations[rkey] = use
		}
		s.openaiCodexTicketRuntimeMu.Unlock()
		if duplicate {
			s.flushOpenAICodexTicketWrites()
			return
		}
		s.recordOpenAICodexTicketEvent(account, use.mode, use.model, "revoke", "revoked", "auth_failed", status, nil, nil, time.Time{})
	}
	state := s.loadOpenAICodexTicketRuntime(account, use.mode, use.model)
	if status == 429 {
		next := openAICodexTicketRetryAt(headers, nil, time.Now())
		if state.NextAttemptAt.After(next) {
			return
		}
		state.NextAttemptAt, state.LastError = next, "rate_limited"
		s.recordOpenAICodexTicketEvent(account, use.mode, use.model, "cooldown", "cooldown", state.LastError, status, nil, nil, next)
	} else {
		state.AuthBlocked, state.LastError = true, "auth_failed"
		s.recordOpenAICodexTicketEvent(account, use.mode, use.model, "auth", "auth_blocked", state.LastError, status, nil, nil, time.Time{})
	}
	s.setOpenAICodexTicketRuntime(account, use.mode, use.model, state)
	s.flushOpenAICodexTicketWrites()
}

type openAICodexTicketRoute struct {
	Key       string
	ID        *int64
	Name, URL string
}

func openAICodexTicketRetryAt(headers http.Header, body []byte, now time.Time) time.Time {
	delay := 300 * time.Second
	if retry := strings.TrimSpace(openAICodexTicketHeaderValue(headers, "Retry-After")); retry != "" {
		if duration, err := time.ParseDuration(retry + "s"); err == nil && duration > delay {
			delay = duration
		} else if at, err := http.ParseTime(retry); err == nil && at.Sub(now) > delay {
			delay = at.Sub(now)
		}
	}
	var data struct {
		Error struct {
			ResetsInSeconds int64 `json:"resets_in_seconds"`
			ResetsAt        int64 `json:"resets_at"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &data) == nil {
		seconds := data.Error.ResetsInSeconds
		if remaining := data.Error.ResetsAt - now.Unix(); remaining > seconds {
			seconds = remaining
		}
		if seconds > int64(delay/time.Second) {
			delay = time.Duration(min(seconds, int64(7*24*60*60))) * time.Second
		}
	}
	return now.Add(min(delay, 7*24*time.Hour))
}
