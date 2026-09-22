package service

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// The write queue is coalesced by state key and served by at most eight workers.
// A saturated worker pool leaves pending work in memory; failed writes are retried
// by the next harvest cycle or a duplicate response observation.
func (s *OpenAIGatewayService) flushOpenAICodexTicketWrites() {
	if s == nil {
		return
	}
	s.openaiCodexTicketRuntimeMu.Lock()
	if s.openaiCodexTicketWritesStopping || len(s.openaiCodexTicketDirty) == 0 && len(s.openaiCodexTicketPendingRevocations) == 0 {
		s.openaiCodexTicketRuntimeMu.Unlock()
		return
	}
	if s.openaiCodexTicketObserveSlots == nil {
		s.openaiCodexTicketObserveSlots = make(chan struct{}, 8)
		s.openaiCodexTicketWriteContext, s.openaiCodexTicketWriteCancel = context.WithCancel(context.Background())
	}
	if s.openaiCodexTicketWritesInFlight == nil {
		s.openaiCodexTicketWritesInFlight = make(map[string]bool)
	}
	slots := s.openaiCodexTicketObserveSlots
	for len(s.openaiCodexTicketPendingRevocations) > openAICodexTicketStateLimit {
		oldest := ""
		var expiry time.Time
		for key, use := range s.openaiCodexTicketPendingRevocations {
			if oldest == "" || use.expiresAt.Before(expiry) {
				oldest, expiry = key, use.expiresAt
			}
		}
		delete(s.openaiCodexTicketPendingRevocations, oldest)
	}
	s.openaiCodexTicketRuntimeMu.Unlock()
	for worker := 0; worker < cap(slots); worker++ {
		s.openaiCodexTicketRuntimeMu.Lock()
		if s.openaiCodexTicketWritesStopping {
			s.openaiCodexTicketRuntimeMu.Unlock()
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			s.openaiCodexTicketRuntimeMu.Unlock()
			return
		}
		s.openaiCodexTicketWriteWG.Add(1)
		writeContext := s.openaiCodexTicketWriteContext
		s.openaiCodexTicketRuntimeMu.Unlock()
		go func() {
			defer s.openaiCodexTicketWriteWG.Done()
			defer func() { <-slots }()
			for batch := 0; batch < 64; batch++ {
				if writeContext.Err() != nil {
					return
				}
				key, mode, model, hash, account, use := s.nextOpenAICodexTicketWrite()
				if key == "" {
					return
				}
				ctx, cancel := context.WithTimeout(writeContext, 2*time.Second)
				ok := true
				if use != nil {
					if repo, exists := s.accountRepo.(openAICodexTicketInvalidator); exists {
						unlock, locked := s.lockOpenAICodexTicketPersistence(ctx, use.mode, use.accountID, use.model)
						ok = locked
						if locked {
							_, err := repo.InvalidateCodexTicket(ctx, use.accountID, use.mode, use.model, use.state, use.credentialHash)
							unlock()
							ok = err == nil
						}
					}
					if !ok {
						s.recordOpenAICodexTicketEvent(account, mode, model, "persist", "error", "persist_failed", 0, nil, nil, time.Time{})
					}
				} else {
					ok = s.persistOpenAICodexTicketRuntime(ctx, account, mode, model, hash)
				}
				cancel()
				s.openaiCodexTicketRuntimeMu.Lock()
				delete(s.openaiCodexTicketWritesInFlight, key)
				if ok && use != nil {
					delete(s.openaiCodexTicketPendingRevocations, strings.TrimPrefix(key, "revoke:"))
				}
				s.openaiCodexTicketRuntimeMu.Unlock()
				if !ok {
					return
				}
			}
		}()
	}
}

func (s *OpenAIGatewayService) stopOpenAICodexTicketWrites() {
	s.openaiCodexTicketRuntimeMu.Lock()
	s.openaiCodexTicketWritesStopping = true
	cancel := s.openaiCodexTicketWriteCancel
	s.openaiCodexTicketRuntimeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() { s.openaiCodexTicketWriteWG.Wait(); close(done) }()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (s *OpenAIGatewayService) nextOpenAICodexTicketWrite() (key, mode, model, hash string, account *Account, use *openAICodexTicketUse) {
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	for candidate, pending := range s.openaiCodexTicketPendingRevocations {
		key = "revoke:" + candidate
		if s.openaiCodexTicketWritesInFlight[key] {
			continue
		}
		s.openaiCodexTicketWritesInFlight[key] = true
		return key, pending.mode, pending.model, pending.credentialHash, &Account{ID: pending.accountID, Name: pending.accountName}, pending
	}
	for candidate := range s.openaiCodexTicketDirty {
		key = "runtime:" + candidate
		if s.openaiCodexTicketWritesInFlight[key] {
			continue
		}
		state, exists := s.openaiCodexTicketRuntime[candidate]
		parts := strings.Split(candidate, "\x00")
		if !exists || len(parts) != 3 {
			delete(s.openaiCodexTicketDirty, candidate)
			continue
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			delete(s.openaiCodexTicketDirty, candidate)
			continue
		}
		s.openaiCodexTicketWritesInFlight[key] = true
		return key, parts[0], parts[2], state.CredentialHash, &Account{ID: id, Name: s.openaiCodexTicketMonitorStates[candidate].AccountName}, nil
	}
	return "", "", "", "", nil, nil
}

func (s *OpenAIGatewayService) cacheOpenAICodexTicket(mode string, ticket *openAICodexTicket) {
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	s.openaiCodexTickets.Store(openAICodexTicketModeKey(mode, ticket.AccountID, ticket.Model), ticket)
	now := time.Now()
	s.openaiCodexTickets.Range(func(key, value any) bool {
		candidate, ok := value.(*openAICodexTicket)
		if !ok || candidate == nil || !now.Before(candidate.ExpiresAt) {
			s.openaiCodexTickets.CompareAndDelete(key, value)
		}
		return true
	})
}

func (s *OpenAIGatewayService) seedOpenAICodexTicketMonitor(account *Account, mode, model string, ticket *openAICodexTicket) {
	key := openAICodexTicketModeKey(mode, account.ID, model)
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	if s.openaiCodexTicketMonitorStates == nil {
		s.openaiCodexTicketMonitorStates = make(map[string]OpenAICodexTicketMonitorState)
	}
	if _, exists := s.openaiCodexTicketMonitorStates[key]; exists {
		return
	}
	expires := ticket.ExpiresAt
	s.openaiCodexTicketMonitorStates[key] = OpenAICodexTicketMonitorState{Mode: mode, AccountID: account.ID, AccountName: account.Name, Model: model, Status: "ready", Phase: "persist", Length: ticket.Length, ExpiresAt: &expires, UpdatedAt: time.Now()}
	if len(s.openaiCodexTicketMonitorStates) > openAICodexTicketStateLimit {
		oldest := ""
		var before time.Time
		for k, state := range s.openaiCodexTicketMonitorStates {
			if oldest == "" || state.UpdatedAt.Before(before) {
				oldest, before = k, state.UpdatedAt
			}
		}
		delete(s.openaiCodexTicketMonitorStates, oldest)
	}
}

func (s *OpenAIGatewayService) rememberOpenAICodexTicketInjection(account *Account, mode, model string, ticket *openAICodexTicket) {
	key := codexTicketRevocationKey(mode, account.ID, model, ticket.State, ticket.CredentialHash)
	s.openaiCodexTicketRuntimeMu.Lock()
	defer s.openaiCodexTicketRuntimeMu.Unlock()
	if s.openaiCodexTicketInjectedUses == nil {
		s.openaiCodexTicketInjectedUses = make(map[string]*openAICodexTicketUse)
	}
	if _, exists := s.openaiCodexTicketInjectedUses[key]; exists {
		return
	}
	s.openaiCodexTicketInjectedUses[key] = &openAICodexTicketUse{mode: mode, model: model, state: ticket.State, credentialHash: ticket.CredentialHash, accountID: account.ID, accountName: account.Name, issuedAt: ticket.IssuedAt, expiresAt: ticket.ExpiresAt}
	for k, use := range s.openaiCodexTicketInjectedUses {
		if !time.Now().Before(use.expiresAt) {
			delete(s.openaiCodexTicketInjectedUses, k)
		}
	}
	if len(s.openaiCodexTicketInjectedUses) > openAICodexTicketStateLimit {
		oldest := ""
		var before time.Time
		for k, use := range s.openaiCodexTicketInjectedUses {
			if oldest == "" || use.expiresAt.Before(before) {
				oldest, before = k, use.expiresAt
			}
		}
		delete(s.openaiCodexTicketInjectedUses, oldest)
	}
}
