package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// This marker exists only in server-built header maps. Both WS dial boundaries
// strip it; clients cannot supply it through buildOpenAIWSHeaders. Keeping the
// signature separate from turn-state preserves ordinary client continuation.
const openAIWSCodexTicketSignatureHeader = "X-Sub2api-Internal-Codex-Ticket"

const openAIWSCodexTicketReconnectReason = "codex ticket policy changed or is unavailable; please reconnect"

func (s *OpenAIGatewayService) applyOpenAIWSCodexTicket(ctx context.Context, account *Account, model string, headers http.Header) error {
	clearOpenAIWSCodexTicketSignature(headers)
	// Keep the actual outbound identity for binding validation, but remove client
	// continuation state so disabled/fail-open paths cannot claim it as a ticket.
	selected := cloneHeader(headers)
	if selected == nil {
		selected = make(http.Header)
	}
	for key := range selected {
		if strings.EqualFold(key, openAICodexTurnStateHeader) {
			delete(selected, key)
		}
	}
	if err := s.applyOpenAICodexTicket(ctx, account, model, selected); err != nil {
		return err
	}
	state := selected.Get(openAICodexTurnStateHeader)
	if state == "" {
		return nil
	}
	headers.Set(openAICodexTurnStateHeader, state)
	digest := sha256.Sum256([]byte(openAICodexTicketModeKey(OpenAICodexTicketMode(account), account.ID, model) + "\x00" + OpenAICodexTicketCredentialHash(account) + "\x00" + state))
	headers.Set(openAIWSCodexTicketSignatureHeader, hex.EncodeToString(digest[:]))
	return nil
}

// Observe errors before fallback/relay handling consumes them. A failed response
// nests its error, while standalone error frames expose the same fields at root.
func (s *OpenAIGatewayService) observeOpenAICodexTicketWSError(ctx context.Context, use *openAICodexTicketUse, headers http.Header, payload []byte) {
	if use == nil {
		return
	}
	switch gjson.GetBytes(payload, "type").String() {
	case "response.failed":
		payload = []byte(gjson.GetBytes(payload, "response").Raw)
	case "error":
	default:
		return
	}
	code, kind, _ := parseOpenAIWSErrorEventFields(payload)
	status := openAIWSErrorHTTPStatusFromRaw(code, kind)
	// Specific credential/rate codes take precedence over a generic
	// invalid_request_error envelope used by some upstream WS responses.
	codeStatus := openAIWSErrorHTTPStatusFromRaw(code, "")
	if codeStatus == http.StatusUnauthorized || codeStatus == http.StatusForbidden || codeStatus == http.StatusTooManyRequests {
		status = codeStatus
	}
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "token_expired", "expired_token", "invalid_token", "account_deactivated":
		status = http.StatusUnauthorized
	}
	// Some upstream error frames provide only an explicit numeric status.
	for _, path := range []string{"status", "status_code", "error.status", "error.status_code"} {
		value := int(gjson.GetBytes(payload, path).Int())
		if value == http.StatusUnauthorized || value == http.StatusForbidden || value == http.StatusTooManyRequests {
			status = value
			break
		}
	}
	if status == http.StatusTooManyRequests {
		headers = cloneHeader(headers)
		if headers == nil {
			headers = make(http.Header)
		}
		now := time.Now()
		next := openAICodexTicketRetryAt(headers, payload, now)
		headers.Set("Retry-After", strconv.FormatInt(int64(next.Sub(now).Seconds()+1), 10))
	}
	s.observeOpenAICodexTicketUse(ctx, use, status, headers)
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
