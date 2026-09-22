package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const openAICodexTicketCredentialHashKey = "_codex_ticket_credential_hash"

// OpenAICodexTicketCredentialHash derives identity from credentials whenever they
// are available. Only token-free scheduler projections may use the private hash.
func OpenAICodexTicketCredentialHash(account *Account) string {
	if account == nil {
		return ""
	}
	if token := account.GetCredential("access_token"); token != "" {
		return openAICodexTicketHash(token, account.GetCredential("chatgpt_account_id"))
	}
	value, _ := account.Extra[openAICodexTicketCredentialHashKey].(string)
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func openAICodexTicketHash(token, accountID string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token + ":" + accountID))
	return hex.EncodeToString(sum[:])
}

func openAICodexTicketHeaderHash(headers http.Header) string {
	auth := strings.TrimSpace(openAICodexTicketHeaderValue(headers, "Authorization"))
	if len(auth) < 8 || !strings.EqualFold(auth[:7], "Bearer ") {
		return ""
	}
	return openAICodexTicketHash(strings.TrimSpace(auth[7:]), openAICodexTicketHeaderValue(headers, "ChatGPT-Account-Id"))
}

func openAICodexTicketHeaderValue(headers http.Header, name string) string {
	var value string
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			if value != "" && value != values[0] {
				return ""
			}
			value = values[0]
		}
	}
	return value
}

func parseOpenAICodexTicketIssued(state string, target int, now time.Time) (time.Time, bool) {
	rawLength := 0
	switch target {
	case 292:
		rawLength = 217
	case 332:
		rawLength = 249
	default:
		return time.Time{}, false
	}
	if len(state) != target || strings.ContainsAny(state, "\r\n \t") {
		return time.Time{}, false
	}
	decoded, err := base64.URLEncoding.Strict().DecodeString(state)
	if err != nil || len(decoded) != rawLength || decoded[0] != 128 {
		return time.Time{}, false
	}
	seconds := binary.BigEndian.Uint64(decoded[1:9])
	if seconds > uint64(now.Add(30*time.Second).Unix()) {
		return time.Time{}, false
	}
	issued := time.Unix(int64(seconds), 0).UTC()
	if !now.Before(issued.Add(3570 * time.Second)) {
		return time.Time{}, false
	}
	return issued, true
}

func (t *openAICodexTicket) boundTo(account *Account) bool {
	hash := OpenAICodexTicketCredentialHash(account)
	return t != nil && hash != "" && t.CredentialHash == hash
}
