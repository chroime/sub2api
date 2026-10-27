package accountquality

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestReviewIdentityTracksOpenAIModelModeAndProxyEdits(t *testing.T) {
	input := IdentityInput{ID: 17, Platform: "openai", Type: "oauth", Credentials: map[string]any{"chatgpt_account_id": "org-1", "chatgpt_user_id": "user-1", "access_token": "old-token"}}
	initial := Fingerprint(input)
	for _, flag := range []string{"openai_passthrough", "openai_oauth_passthrough"} {
		input.Extra = map[string]any{flag: true}
		require.NotEqual(t, initial, Fingerprint(input), "changing %s changes the executed model and must invalidate old evidence", flag)
	}
	input.Extra = nil
	id := int64(5)
	input.ProxyID = &id
	input.Proxy = map[string]any{"id": int64(5), "protocol": "http", "host": "proxy.example", "port": 8080, "username": "user", "password": "old-password"}
	withProxy := Fingerprint(input)
	input.Proxy["password"] = "new-password"
	require.NotEqual(t, withProxy, Fingerprint(input), "a changed proxy identity must make an in-flight result stale")
}

func TestReviewRoutineOAuthRefreshAndQualityProjectionDoNotInvalidateEvidence(t *testing.T) {
	input := IdentityInput{ID: 17, Platform: "openai", Type: "oauth", Credentials: map[string]any{"chatgpt_account_id": "org-1", "chatgpt_user_id": "user-1", "access_token": "old-token", "refresh_token": "old-refresh", "expires_at": "2026-10-25T00:00:00Z", "_token_version": int64(1)}}
	initial := Fingerprint(input)
	input.Credentials["access_token"] = "new-token"
	input.Credentials["refresh_token"] = "new-refresh"
	input.Credentials["expires_at"] = "2026-10-26T00:00:00Z"
	input.Credentials["_token_version"] = int64(2)
	input.Extra = map[string]any{"account_quality_gates": map[string]any{"9": map[string]any{"blocked": true}}, "codex_usage_updated_at": "2026-10-25T10:00:00Z"}
	require.Equal(t, initial, Fingerprint(input), "routine observations and token rotation must not discard a stable account's result")
	input.Credentials["chatgpt_user_id"] = "different-user"
	require.NotEqual(t, initial, Fingerprint(input), "the stable upstream user identity still participates in the version")
}

func TestReviewNotificationChannelsRemainOffWithOnlyRecipientConfiguration(t *testing.T) {
	for _, mode := range []string{"record_notify", "auto_quarantine", "record_only"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			settings := DefaultSettings()
			settings.Notifications.EmailRecipients = []string{"configured@example.com"}
			settings.Notifications.QQTargets = []string{"bot:GroupMessage:fixture"}
			if mode == "record_only" {
				settings.Notifications.EmailEnabled = true
				settings.Notifications.QQEnabled = true
			}
			// Any attempted INSERT would fail this transaction expectation. No sender is involved.
			require.NoError(t, enqueueNoticeTx(context.Background(), tx, settings, Binding{ID: 1, AccountID: 17}, Run{ID: "run-1", Config: Policy{ControlMode: mode}}, "quarantined"))
			mock.ExpectCommit()
			require.NoError(t, tx.Commit())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
