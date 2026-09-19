package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataDerivesCodexIdentityWithoutOAuthSecrets(t *testing.T) {
	account := service.Account{
		ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "original-oauth-secret", "chatgpt_account_id": "upstream-identity"},
		Extra:       map[string]any{"codex_ticket_mode": "332", "_codex_ticket_credential_hash": "injected"},
	}
	metadata := buildSchedulerMetadataAccount(account)
	digest := sha256.Sum256([]byte("original-oauth-secret:upstream-identity"))
	require.Equal(t, hex.EncodeToString(digest[:]), metadata.Extra["_codex_ticket_credential_hash"])
	require.Equal(t, "332", metadata.Extra["codex_ticket_mode"])
	require.Equal(t, "injected", account.Extra["_codex_ticket_credential_hash"], "projection must not mutate source")
	payload, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "original-oauth-secret")
	require.NotContains(t, string(payload), "upstream-identity")
	account.Credentials["access_token"] = "rotated-oauth-secret"
	require.NotEqual(t, metadata.Extra["_codex_ticket_credential_hash"], buildSchedulerMetadataAccount(account).Extra["_codex_ticket_credential_hash"])
}

func TestSchedulerMetadataDoesNotTrustUnboundInjectedIdentity(t *testing.T) {
	metadata := buildSchedulerMetadataAccount(service.Account{
		ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Extra: map[string]any{"_codex_ticket_credential_hash": "injected"},
	})
	require.NotContains(t, metadata.Extra, "_codex_ticket_credential_hash")
}

func TestSchedulerMetadataBindsCodexSetupTokenIdentity(t *testing.T) {
	account := service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeSetupToken,
		Credentials: map[string]any{"access_token": "setup-token", "chatgpt_account_id": "setup-account"}}
	metadata := buildSchedulerMetadataAccount(account)
	require.Equal(t, service.OpenAICodexTicketCredentialHash(&account), service.OpenAICodexTicketCredentialHash(&metadata))
	require.NotContains(t, metadata.Credentials, "access_token")
}
