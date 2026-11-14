//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestMonitorUsageMetricsPreserveWindowsZeroAndMissingSamples(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(monitorUsageMetricsQuery)).WithArgs(sqlmock.AnyArg(), now).
		WillReturnRows(sqlmock.NewRows([]string{"monitor_id", "days", "cache_rate_pct", "first_token_ms"}).
			AddRow(5, 7, 0, nil).AddRow(5, 15, 86.2, 3000).AddRow(5, 30, nil, 0))
	result, err := (&channelMonitorRepository{db: db}).ListUsageMetricsForMonitors(context.Background(), []int64{5, 9}, now)
	require.NoError(t, err)
	require.Zero(t, *result[5]["7d"].CacheRatePct)
	require.Nil(t, result[5]["7d"].FirstTokenMs)
	require.Equal(t, 86.2, *result[5]["15d"].CacheRatePct)
	require.Equal(t, 3000.0, *result[5]["15d"].FirstTokenMs)
	require.Nil(t, result[5]["30d"].CacheRatePct)
	require.Zero(t, *result[5]["30d"].FirstTokenMs)
	require.Nil(t, result[9])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMonitorUserRatesAreScopedToAuthenticatedUser(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta(monitorUserRatesQuery)).WithArgs(sqlmock.AnyArg(), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"monitor_id", "rate_multiplier"}).AddRow(5, 0).AddRow(9, 0.075))
	result, err := (&channelMonitorRepository{db: db}).ListUserRatesForMonitors(context.Background(), []int64{5, 9}, 42)
	require.NoError(t, err)
	require.Zero(t, result[5])
	require.Equal(t, 0.075, result[9])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMonitorMetricsEmptyAndAnonymousDoNotQuery(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &channelMonitorRepository{db: db}
	_, err := repo.ListUsageMetricsForMonitors(context.Background(), nil, time.Now())
	require.NoError(t, err)
	_, err = repo.ListUserRatesForMonitors(context.Background(), []int64{5}, 0)
	require.NoError(t, err)
	_, err = repo.ListUserRatesForMonitors(context.Background(), nil, 42)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
