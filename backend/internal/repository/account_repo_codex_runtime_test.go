package repository

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type codexTicketRuntimeWriter interface {
	UpdateCodexTicketRuntime(context.Context, int64, string, string, map[string]any) error
}

type codexRuntimePayloadMatcher struct{ inspect func(map[string]any) bool }

func (m codexRuntimePayloadMatcher) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return false
	}
	return m.inspect(payload)
}

func codexRuntimeTestHash(token string) string {
	sum := sha256.Sum256([]byte(token + ":org-1"))
	return hex.EncodeToString(sum[:])
}

func newCodexRuntimeTestRepo(t *testing.T) (codexTicketRuntimeWriter, *dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo, ok := NewAccountRepository(client, db, nil).(codexTicketRuntimeWriter)
	require.True(t, ok, "production repository must merge durable Codex runtime under a row lock")
	return repo, client, mock
}

func TestUpdateCodexTicketRuntimeMergesLatestDatabaseState(t *testing.T) {
	now := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	hash := codexRuntimeTestHash("current-token")
	for _, tc := range []struct {
		name      string
		existing  map[string]any
		incoming  map[string]any
		wantAuth  bool
		wantNext  time.Time
		wantError string
	}{
		{name: "new state", wantNext: now.Add(time.Minute), incoming: map[string]any{"next_attempt_at": now.Add(time.Minute)}, wantError: "network_error"},
		{name: "late success cannot clear authentication block", existing: map[string]any{"credential_hash": hash, "auth_blocked": true, "last_error": "auth_failed", "updated_at": now.Add(time.Minute)}, incoming: map[string]any{"last_error": ""}, wantAuth: true, wantError: "auth_failed"},
		{name: "newer success cannot shorten another instance cooldown", existing: map[string]any{"credential_hash": hash, "next_attempt_at": now.Add(time.Hour), "last_error": "rate_limited", "updated_at": now.Add(-time.Minute)}, incoming: map[string]any{"last_error": ""}, wantNext: now.Add(time.Hour), wantError: "rate_limited"},
		{name: "shorter failure retains longer durable cooldown", existing: map[string]any{"credential_hash": hash, "next_attempt_at": now.Add(time.Hour), "last_error": "rate_limited", "updated_at": now.Add(-time.Minute)}, incoming: map[string]any{"next_attempt_at": now.Add(5 * time.Minute)}, wantNext: now.Add(time.Hour), wantError: "rate_limited"},
		{name: "longer failure extends durable cooldown", existing: map[string]any{"credential_hash": hash, "next_attempt_at": now.Add(time.Minute), "updated_at": now.Add(-time.Minute)}, incoming: map[string]any{"next_attempt_at": now.Add(2 * time.Hour), "last_error": "rate_limited"}, wantNext: now.Add(2 * time.Hour), wantError: "rate_limited"},
		{name: "current new credential resets prior credential block", existing: map[string]any{"credential_hash": codexRuntimeTestHash("retired-token"), "auth_blocked": true, "next_attempt_at": now.Add(time.Hour), "updated_at": now.Add(time.Minute)}, incoming: map[string]any{"last_error": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, mock := newCodexRuntimeTestRepo(t)
			current, err := json.Marshal(tc.existing)
			require.NoError(t, err)
			incoming := map[string]any{"credential_hash": hash, "updated_at": now, "last_error": "network_error", "preferred_route": "proxy:7", "authorization": "must-not-persist"}
			for key, value := range tc.incoming {
				incoming[key] = value
			}
			mock.ExpectBegin()
			mock.ExpectQuery(`(?s)SELECT.*credentials.*extra.*FROM accounts.*FOR NO KEY UPDATE`).WithArgs(int64(41), "codex_ticket_runtime:332:gpt-6-astra").WillReturnRows(sqlmock.NewRows([]string{"access_token", "chatgpt_account_id", "runtime"}).AddRow("current-token", "org-1", current))
			var captured map[string]any
			mock.ExpectExec(`(?s)UPDATE accounts SET extra = .*updated_at = NOW\(\).*WHERE id = \$2 AND deleted_at IS NULL`).WithArgs(codexRuntimePayloadMatcher{inspect: func(payload map[string]any) bool {
				if len(payload) != 1 {
					return false
				}
				captured, _ = payload["codex_ticket_runtime:332:gpt-6-astra"].(map[string]any)
				return captured != nil
			}}, int64(41)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			require.NoError(t, repo.UpdateCodexTicketRuntime(context.Background(), 41, "332", "gpt-6-astra", incoming))
			require.Equal(t, hash, captured["credential_hash"])
			blocked, _ := captured["auth_blocked"].(bool)
			require.Equal(t, tc.wantAuth, blocked)
			next := time.Time{}
			if value, ok := captured["next_attempt_at"].(string); ok {
				next, err = time.Parse(time.RFC3339Nano, value)
				require.NoError(t, err)
			}
			require.True(t, tc.wantNext.Equal(next), "expected next attempt %s, got %s", tc.wantNext, next)
			lastError, _ := captured["last_error"].(string)
			require.Equal(t, tc.wantError, lastError)
			require.NotContains(t, captured, "authorization")
			require.NoError(t, mock.ExpectationsWereMet(), "no scheduler bucket outbox should be written")
		})
	}
}

func TestUpdateCodexTicketRuntimeRejectsRetiredCredential(t *testing.T) {
	for _, token := range []string{"current-token", ""} {
		t.Run("database token "+token, func(t *testing.T) {
			repo, _, mock := newCodexRuntimeTestRepo(t)
			mock.ExpectBegin()
			mock.ExpectQuery(`(?s)SELECT.*FOR NO KEY UPDATE`).WithArgs(int64(41), "codex_ticket_runtime:292:gpt-6-astra").WillReturnRows(sqlmock.NewRows([]string{"access_token", "chatgpt_account_id", "runtime"}).AddRow(token, "org-1", nil))
			mock.ExpectCommit()
			err := repo.UpdateCodexTicketRuntime(context.Background(), 41, "292", "gpt-6-astra", map[string]any{"credential_hash": codexRuntimeTestHash("retired-token"), "updated_at": time.Now(), "auth_blocked": true})
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet(), "retired credentials must not write or clear runtime state")
		})
	}
}

func TestUpdateCodexTicketRuntimePropagatesFailureAndRollsBack(t *testing.T) {
	for _, at := range []string{"read", "write", "commit"} {
		t.Run(at, func(t *testing.T) {
			repo, _, mock := newCodexRuntimeTestRepo(t)
			dbErr := errors.New("temporary database failure")
			mock.ExpectBegin()
			query := mock.ExpectQuery(`(?s)SELECT.*FOR NO KEY UPDATE`)
			if at == "read" {
				query.WillReturnError(dbErr)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"access_token", "chatgpt_account_id", "runtime"}).AddRow("current-token", "org-1", nil))
				write := mock.ExpectExec(`UPDATE accounts SET extra`)
				if at == "write" {
					write.WillReturnError(dbErr)
				} else {
					write.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if at == "commit" {
				mock.ExpectCommit().WillReturnError(dbErr)
			} else {
				mock.ExpectRollback()
			}
			err := repo.UpdateCodexTicketRuntime(context.Background(), 41, "292", "gpt-6-astra", map[string]any{"credential_hash": codexRuntimeTestHash("current-token"), "updated_at": time.Now()})
			require.ErrorIs(t, err, dbErr)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpdateCodexTicketRuntimeJoinsExistingTransaction(t *testing.T) {
	repo, client, mock := newCodexRuntimeTestRepo(t)
	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	mock.ExpectQuery(`(?s)SELECT.*FOR NO KEY UPDATE`).WithArgs(int64(41), "codex_ticket_runtime:292:gpt-6-astra").WillReturnRows(sqlmock.NewRows([]string{"access_token", "chatgpt_account_id", "runtime"}).AddRow("current-token", "org-1", nil))
	mock.ExpectExec(`UPDATE accounts SET extra`).WillReturnResult(sqlmock.NewResult(0, 1))
	ctx := dbent.NewTxContext(context.Background(), tx)
	require.NoError(t, repo.UpdateCodexTicketRuntime(ctx, 41, "292", "gpt-6-astra", map[string]any{"credential_hash": codexRuntimeTestHash("current-token"), "updated_at": time.Now()}))
	require.NoError(t, mock.ExpectationsWereMet(), "repository must leave ambient commit to its caller")
	mock.ExpectCommit()
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateCodexTicketRuntimeRejectsInvalidPayloadBeforeDatabase(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		id         int64
		runtime    map[string]any
	}{
		{"invalid account", "292", 0, map[string]any{}},
		{"invalid mode", "off", 41, map[string]any{}},
		{"missing hash", "292", 41, map[string]any{"updated_at": time.Now()}},
		{"invalid hash", "292", 41, map[string]any{"credential_hash": "not-an-identity", "updated_at": time.Now()}},
		{"invalid timestamp", "292", 41, map[string]any{"credential_hash": codexRuntimeTestHash("current-token"), "updated_at": "invalid"}},
		{"missing timestamp", "292", 41, map[string]any{"credential_hash": codexRuntimeTestHash("current-token")}},
		{"invalid next attempt", "292", 41, map[string]any{"credential_hash": codexRuntimeTestHash("current-token"), "updated_at": time.Now(), "next_attempt_at": "invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, mock := newCodexRuntimeTestRepo(t)
			require.Error(t, repo.UpdateCodexTicketRuntime(context.Background(), tc.id, tc.mode, "gpt-6-astra", tc.runtime))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
