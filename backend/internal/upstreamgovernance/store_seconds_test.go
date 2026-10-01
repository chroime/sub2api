package upstreamgovernance

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSQLStoreReserveFastObservationUsesCAS(t *testing.T) {
	store, mock := storeFixture(t)
	now := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE upstream_governance_sites SET fast_observe_status='running'")).WithArgs(int64(7), now, now).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := store.(FastObservationStore).ReserveFastObservation(context.Background(), 7, now, now)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestSQLStoreUnchangedFastObservationKeepsRevision(t *testing.T) {
	store, mock := storeFixture(t)
	now := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	group := RemoteGroup{ID: "7", Name: "VIP", ResolvedRateMultiplier: floatPtr(.8), Models: []string{}, Prices: []RemotePrice{}}
	observation := GroupObservation{ObservedAt: now, SourceUserID: 42, GroupsComplete: true, Groups: []RemoteGroup{group}}
	fingerprint, err := groupObservationFingerprint(observation)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,revision FROM upstream_governance_fast_observations WHERE site_id=$1 FOR UPDATE")).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "revision"}).AddRow(fingerprint, int64(4)))
	mock.ExpectExec("INSERT INTO upstream_governance_fast_observations").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE upstream_governance_sites SET fast_observe_status").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := store.(FastObservationStore).ObserveFastResult(context.Background(), 7, now, now.Add(time.Second), "healthy", "", observation)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.Revision)
}

func floatPtr(value float64) *float64 { return &value }
