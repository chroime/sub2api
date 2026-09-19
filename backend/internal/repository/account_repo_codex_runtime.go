package repository

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The storage projection intentionally excludes all ticket and transport data.
type codexTicketRuntimeRecord struct {
	CredentialHash string    `json:"credential_hash"`
	NextAttemptAt  time.Time `json:"next_attempt_at,omitempty"`
	AuthBlocked    bool      `json:"auth_blocked,omitempty"`
	PreferredRoute string    `json:"preferred_route,omitempty"`
	FailedRoute    string    `json:"failed_route,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastError      string    `json:"last_error,omitempty"`
}

// UpdateCodexTicketRuntime merges current-credential runtime under the account
// row lock. A stale success from another instance cannot shorten a cooldown or
// lift an authentication block, and retired credentials cannot replace state.
func (r *accountRepository) UpdateCodexTicketRuntime(ctx context.Context, accountID int64, mode, model string, runtime map[string]any) error {
	model = strings.TrimSpace(model)
	if accountID <= 0 || (mode != "292" && mode != "332") || model == "" {
		return errors.New("invalid Codex ticket runtime identity")
	}
	raw, err := json.Marshal(runtime)
	if err != nil {
		return errors.New("invalid Codex ticket runtime update")
	}
	var incoming codexTicketRuntimeRecord
	if json.Unmarshal(raw, &incoming) != nil || len(incoming.CredentialHash) != 64 || incoming.UpdatedAt.IsZero() {
		return errors.New("invalid Codex ticket runtime update")
	}
	if _, err := hex.DecodeString(incoming.CredentialHash); err != nil {
		return errors.New("invalid Codex ticket runtime credential hash")
	}
	key := "codex_ticket_runtime:" + mode + ":" + model
	if dbent.TxFromContext(ctx) != nil {
		_, err := r.updateCodexTicketRuntimeInTx(ctx, accountID, key, incoming)
		return err
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		_, err = r.updateCodexTicketRuntimeInTx(ctx, accountID, key, incoming)
		return err
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := r.updateCodexTicketRuntimeInTx(dbent.NewTxContext(ctx, tx), accountID, key, incoming)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if changed {
		// Match UpdateExtra's single-account refresh for observational metadata;
		// ticket cooldown changes do not rebuild scheduler buckets.
		r.syncSchedulerAccountSnapshot(ctx, accountID)
	}
	return nil
}

func (r *accountRepository) updateCodexTicketRuntimeInTx(ctx context.Context, accountID int64, key string, incoming codexTicketRuntimeRecord) (bool, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `
		SELECT COALESCE(credentials ->> 'access_token', ''),
		       COALESCE(credentials ->> 'chatgpt_account_id', ''), extra -> $2
		FROM accounts WHERE id = $1 AND deleted_at IS NULL
		FOR NO KEY UPDATE
	`, accountID, key)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var token, chatGPTAccountID string
	var currentJSON []byte
	if err := rows.Scan(&token, &chatGPTAccountID, &currentJSON); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	currentHash := service.OpenAICodexTicketCredentialHash(&service.Account{Credentials: map[string]any{
		"access_token": token, "chatgpt_account_id": chatGPTAccountID,
	}})
	if currentHash == "" || currentHash != incoming.CredentialHash {
		return false, nil
	}
	var current codexTicketRuntimeRecord
	if len(currentJSON) > 0 && json.Unmarshal(currentJSON, &current) != nil {
		return false, errors.New("invalid stored Codex ticket runtime")
	}
	merged := incoming
	if current.CredentialHash == incoming.CredentialHash {
		if current.UpdatedAt.After(incoming.UpdatedAt) {
			merged = current
		}
		merged.AuthBlocked = current.AuthBlocked || incoming.AuthBlocked
		merged.NextAttemptAt = incoming.NextAttemptAt
		if current.NextAttemptAt.After(incoming.NextAttemptAt) {
			merged.NextAttemptAt = current.NextAttemptAt
			merged.LastError = current.LastError
		} else if incoming.NextAttemptAt.After(current.NextAttemptAt) {
			merged.LastError = incoming.LastError
		}
		if merged.AuthBlocked {
			merged.LastError = "auth_failed"
		}
	}
	payload, err := json.Marshal(map[string]any{key: merged})
	if err != nil {
		return false, err
	}
	result, err := client.ExecContext(ctx,
		`UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`,
		string(payload), accountID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
