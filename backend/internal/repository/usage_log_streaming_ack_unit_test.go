//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogStreamingAckPrepareAndScan(t *testing.T) {
	ttft, ack := 22580, 720
	for _, tc := range []struct {
		name string
		ttft *int
		ack  *int
	}{
		{name: "real output and ACK", ttft: &ttft, ack: &ack},
		{name: "no ACK", ttft: &ttft},
		{name: "ACK without real output", ack: &ack},
		{name: "legacy missing metrics"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &service.UsageLog{
				ID: 12, UserID: 1, APIKeyID: 2, AccountID: 3,
				RequestID: "ack-roundtrip", Model: "gpt-5.4",
				FirstTokenMs: tc.ttft, StreamingAckMs: tc.ack,
				CreatedAt: time.Now().UTC(),
			}
			prepared := prepareUsageLogInsert(log)
			columns := strings.Split(usageLogSelectColumns, ", ")
			require.Contains(t, columns, "streaming_ack_ms")
			require.Len(t, prepared.args, len(usageLogInsertArgTypes))
			for i, column := range columns[1:] {
				if column == "streaming_ack_ms" {
					require.Equal(t, "integer", usageLogInsertArgTypes[i])
					require.Equal(t, nullInt(tc.ack), prepared.args[i])
				}
			}

			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			values := append([]driver.Value{log.ID}, anySliceToDriverValues(prepared.args)...)
			mock.ExpectQuery("SELECT .* FROM usage_logs WHERE id = \\$1").
				WithArgs(log.ID).
				WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
			got, err := repo.GetByID(context.Background(), log.ID)
			require.NoError(t, err)
			require.Equal(t, tc.ttft, got.FirstTokenMs)
			require.Equal(t, tc.ack, got.StreamingAckMs)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageLogStreamingAckEveryInsertColumnList(t *testing.T) {
	ack := 730
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID: 1, APIKeyID: 2, RequestID: "ack-insert", Model: "gpt-5.4", StreamingAckMs: &ack,
	})
	key := usageLogBatchKey(prepared.requestID, 2)
	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	bestEffortQuery, bestEffortArgs := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	columns := strings.Split(usageLogSelectColumns, ", ")[1:]
	for name, query := range map[string]string{"batch": batchQuery, "best effort": bestEffortQuery} {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, query, "streaming_ack_ms")
			lists := regexp.MustCompile(`(?s)(?:WITH input|INSERT INTO usage_logs)\s*\((.*?)\)`).FindAllStringSubmatch(query, -1)
			require.NotEmpty(t, lists)
			for _, match := range lists {
				got := strings.Split(match[1], ",")
				for i := range got {
					got[i] = strings.TrimSpace(got[i])
				}
				if got[0] == "input_idx" {
					got = got[1:]
				}
				require.Equal(t, columns, got)
			}
		})
	}
	require.Len(t, batchArgs, len(columns)+1)
	require.Len(t, bestEffortArgs, len(columns))
	require.Contains(t, bestEffortArgs, sql.NullInt64{Int64: 730, Valid: true})
}
