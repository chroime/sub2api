package upstreamgovernance

import (
	"context"
	"encoding/json"
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
	raw, err := json.Marshal(observation.Groups)
	require.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,revision,groups FROM upstream_governance_fast_observations WHERE site_id=$1 FOR UPDATE")).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "revision", "groups"}).AddRow(fingerprint, int64(4), raw))
	mock.ExpectExec("INSERT INTO upstream_governance_fast_observations").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE upstream_governance_sites SET fast_observe_status").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := store.(FastObservationStore).ObserveFastResult(context.Background(), 7, now, now.Add(time.Second), "healthy", "", observation)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.Revision)
}

func TestSQLStoreNextFastObservationSkipsActiveReservations(t *testing.T) {
	store, mock := storeFixture(t)
	next := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT MIN(next_fast_observe_at) FROM upstream_governance_sites")).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(next))
	got, err := store.(FastObservationScheduleStore).NextFastObservationAt(context.Background())
	require.NoError(t, err)
	require.Equal(t, next, *got)
}

func floatPtr(value float64) *float64 { return &value }

func TestSQLStoreFastObservationKeepsVisibilityHistoryAtomically(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		before, after         []RemoteGroup
		beforeFlag, afterFlag string
	}{
		{"became exclusive", []RemoteGroup{{ID: "7", Name: "public", ResolvedRateMultiplier: floatPtr(1)}}, []RemoteGroup{{ID: "7", Name: "private", IsExclusive: true, ResolvedRateMultiplier: floatPtr(1)}}, "false", "true"},
		{"became public", []RemoteGroup{{ID: "7", IsExclusive: true}}, []RemoteGroup{{ID: "7", Name: "public", ResolvedRateMultiplier: floatPtr(1)}}, "true", "false"},
		{"public group removed", []RemoteGroup{{ID: "7"}}, []RemoteGroup{}, "false", "false"},
		{"exclusive group removed", []RemoteGroup{{ID: "7", IsExclusive: true}}, []RemoteGroup{}, "true", "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, mock := storeFixture(t)
			now := time.Unix(100, 0).UTC()
			raw, err := json.Marshal(tt.before)
			require.NoError(t, err)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT fingerprint,revision,groups").WithArgs(int64(5)).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "revision", "groups"}).AddRow("previous", 1, raw))
			mock.ExpectExec("INSERT INTO upstream_governance_fast_observations").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO upstream_governance_events").WithArgs(int64(5), "7", tt.beforeFlag, tt.afterFlag, now).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE upstream_governance_sites").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			_, err = store.(FastObservationStore).ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: tt.after})
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
