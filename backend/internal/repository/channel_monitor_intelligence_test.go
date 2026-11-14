//go:build unit

package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"regexp"
	"testing"
	"time"
)

func TestMonitorIntelligenceProjectionPreservesActualChecks(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}
	now := time.Date(2026, 11, 14, 3, 0, 30, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(monitorIntelligenceQuery)).WithArgs(sqlmock.AnyArg(), now).
		WillReturnRows(sqlmock.NewRows([]string{"monitor_id", "model", "effort", "checked_at", "status", "id"}).
			AddRow(4, "quality-model", "medium", now, "pass", "check-pass").
			AddRow(4, "quality-model", "medium", now.Add(-2*time.Minute), "fail", "check-fail"))
	result, err := repo.ListIntelligenceForMonitors(context.Background(), []int64{4, 9}, now)
	require.NoError(t, err)
	require.Len(t, result[4].Series, 1)
	require.Len(t, result[4].Series[0].Points, 2, "missing minutes must not be synthesized as passes")
	require.Nil(t, result[9], "a monitor without diagnostics must remain untested")
	require.Equal(t, "pass", result[4].Series[0].Points[0].Status)
	require.Equal(t, "check-pass", result[4].Series[0].Points[0].ID)
	require.Equal(t, now, result[4].WindowEnd)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestMonitorIntelligenceEmptyListDoesNotQuery(t *testing.T) {
	db, mock := newSQLMock(t)
	result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), nil, time.Now())
	require.NoError(t, err)
	require.Empty(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMonitorIntelligenceKeepsConfiguredIdentityWithoutRecentRuns(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(monitorIntelligenceQuery)).WithArgs(sqlmock.AnyArg(), now).
		WillReturnRows(sqlmock.NewRows([]string{"monitor_id", "model", "effort", "checked_at", "status", "id"}).
			AddRow(4, "claude-opus-5.5", "medium", nil, nil, nil))
	result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{4, 9}, now)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-5.5", result[4].Series[0].Model)
	require.Equal(t, "medium", result[4].Series[0].Effort)
	require.Equal(t, now, result[4].WindowEnd)
	require.NotNil(t, result[4].Series[0].Points)
	require.Empty(t, result[4].Series[0].Points, "configuration must never create a successful history point")
	require.Nil(t, result[9], "unconfigured monitors must not inherit another monitor's model")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMonitorIntelligenceSeparatesModelAndEffortHistories(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(monitorIntelligenceQuery)).WithArgs(sqlmock.AnyArg(), now).
		WillReturnRows(sqlmock.NewRows([]string{"monitor_id", "model", "effort", "checked_at", "status", "id"}).
			AddRow(4, "model-a", "high", now, "pending", "check-pending").
			AddRow(4, "model-a", "medium", now, "pass", "check-pass").
			AddRow(4, "model-b", "medium", nil, nil, nil))
	result, err := (&channelMonitorRepository{db: db}).ListIntelligenceForMonitors(context.Background(), []int64{4}, now)
	require.NoError(t, err)
	require.Len(t, result[4].Series, 3)
	require.Equal(t, "pending", result[4].Series[0].Points[0].Status)
	require.Equal(t, "high", result[4].Series[0].Effort)
	require.Equal(t, "pass", result[4].Series[1].Points[0].Status)
	require.Equal(t, "medium", result[4].Series[1].Effort)
	require.Equal(t, "model-b", result[4].Series[2].Model)
	require.Empty(t, result[4].Series[2].Points)
	require.NoError(t, mock.ExpectationsWereMet())
}
