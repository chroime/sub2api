package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
)

// This marker exists only in server-built header maps. Both WS dial boundaries
// strip it; clients cannot supply it through buildOpenAIWSHeaders. Keeping the
// signature separate from turn-state preserves ordinary client continuation.
const openAIWSCodexTicketSignatureHeader = "X-Sub2api-Internal-Codex-Ticket"

const openAIWSCodexTicketReconnectReason = "codex ticket policy changed or is unavailable; please reconnect"

func (s *OpenAIGatewayService) applyOpenAIWSCodexTicket(ctx context.Context, account *Account, model string, headers http.Header) error {
	clearOpenAIWSCodexTicketSignature(headers)
	// An empty temporary map distinguishes a server-injected ticket from a
	// client's ordinary turn-state, including on disabled/fail-open paths.
	selected := make(http.Header)
	if err := s.applyOpenAICodexTicket(ctx, account, model, selected); err != nil {
		return err
	}
	state := selected.Get(openAICodexTurnStateHeader)
	if state == "" {
		return nil
	}
	headers.Set(openAICodexTurnStateHeader, state)
	digest := sha256.Sum256([]byte(openAICodexTicketModeKey(OpenAICodexTicketMode(account), account.ID, model) + "\x00" + state))
	headers.Set(openAIWSCodexTicketSignatureHeader, hex.EncodeToString(digest[:]))
	return nil
}

func openAIWSHeadersForUpstream(headers http.Header) http.Header {
	out := cloneHeader(headers)
	clearOpenAIWSCodexTicketSignature(out)
	return out
}

func clearOpenAIWSCodexTicketSignature(headers http.Header) {
	// Administrator overrides preserve selected wire casing, so Header.Del's
	// canonical lookup alone cannot remove every possible representation.
	for name := range headers {
		if strings.EqualFold(name, openAIWSCodexTicketSignatureHeader) {
			delete(headers, name)
		}
	}
}

func (l *openAIWSConnLease) codexTicketSignature() string {
	if l == nil || l.conn == nil {
		return ""
	}
	return l.conn.handshakeCompatibility.codexTicketSignature
}

func openAIWSCodexTicketReconnectError(cause error) error {
	return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, openAIWSCodexTicketReconnectReason, cause)
}

// checkOpenAIWSCodexTicket runs immediately before an actual response.create.
// A live upstream cannot change its handshake credential. Fail honestly and
// require reconnect instead of silently changing the backend conversation.
func (s *OpenAIGatewayService) checkOpenAIWSCodexTicket(ctx context.Context, account *Account, model, handshakeSignature string) error {
	if s == nil || (!isOpenAICodexTicketAccount(account) && handshakeSignature == "") {
		return nil
	}
	// No DB read is added to traffic while both mechanisms are unused. A bound
	// connection still needs checking when a global switch has been disabled.
	if handshakeSignature == "" &&
		!s.openAICodexTicketRuntimeConfig(ctx, openAICodexTicketMode292).Enabled &&
		!s.openAICodexTicketRuntimeConfig(ctx, openAICodexTicketMode332).Enabled {
		return nil
	}
	latest := account
	if s.accountRepo != nil {
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		fresh, err := s.accountRepo.GetByID(readCtx, account.ID)
		cancel()
		if err != nil || fresh == nil || fresh.ID != account.ID {
			return openAIWSCodexTicketReconnectError(errors.New("cannot refresh account ticket policy"))
		}
		latest = fresh
	}
	headers := make(http.Header)
	if err := s.applyOpenAIWSCodexTicket(ctx, latest, model, headers); err != nil {
		return openAIWSCodexTicketReconnectError(err)
	}
	if headers.Get(openAIWSCodexTicketSignatureHeader) != handshakeSignature {
		return openAIWSCodexTicketReconnectError(errors.New("websocket ticket handshake no longer matches request"))
	}
	return nil
}
