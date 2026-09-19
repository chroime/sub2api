package repository

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type codexTicketInvalidator interface {
	InvalidateCodexTicket(context.Context, int64, string, string, string, string) (bool, error)
}

func TestInvalidateCodexTicketMatchesUsedStateAndCredential(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows int64
	}{
		{"current ticket removed", 1},
		{"replacement or changed credential preserved", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo, ok := NewAccountRepository(client, db, nil).(codexTicketInvalidator)
			require.True(t, ok, "production repository must conditionally invalidate the exact used ticket")
			mock.ExpectExec(`(?s)UPDATE accounts SET extra = .* - \$1.*WHERE id = \$2 AND deleted_at IS NULL.*extra -> \$1 ->> 'state' = \$3.*extra -> \$1 ->> 'credential_hash' = \$4`).
				WithArgs("codex_turn_ticket:332:gpt-6-astra", int64(41), "used-ticket", "used-identity").
				WillReturnResult(sqlmock.NewResult(0, tc.rows))
			removed, err := repo.InvalidateCodexTicket(context.Background(), 41, "332", "gpt-6-astra", "used-ticket", "used-identity")
			require.NoError(t, err)
			require.Equal(t, tc.rows == 1, removed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestInvalidateCodexTicketPropagatesDatabaseFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo, ok := NewAccountRepository(client, db, nil).(codexTicketInvalidator)
	require.True(t, ok)
	dbErr := errors.New("database unavailable")
	mock.ExpectExec(`UPDATE accounts SET extra`).WillReturnError(dbErr)
	removed, err := repo.InvalidateCodexTicket(context.Background(), 41, "292", "gpt-6-astra", "used-ticket", "used-identity")
	require.ErrorIs(t, err, dbErr)
	require.False(t, removed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexRuntimeExtraDoesNotRebuildSchedulerBuckets(t *testing.T) {
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"codex_ticket_runtime:332:gpt-6-astra": map[string]any{"status": "cooldown"},
	}))
}
