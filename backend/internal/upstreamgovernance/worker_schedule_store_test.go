package upstreamgovernance

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSQLNextSiteDueAtIncludesIndependentDeadlines(t *testing.T) {
	store, mock := storeFixture(t)
	next := time.Date(2026, 10, 3, 12, 0, 5, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(nextSiteDueQuery)).WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(next))
	got, err := store.(SiteScheduleStore).NextSiteDueAt(context.Background())
	require.NoError(t, err)
	require.Equal(t, next, *got)
	// Guard the contracts that otherwise allow a disabled or disconnected site
	// to keep the worker awake forever, or make inference probes require login.
	require.Contains(t, nextSiteDueQuery, "WHERE s.enabled AND s.session_cipher<>''")
	require.Contains(t, nextSiteDueQuery, "WHERE s.enabled AND b.probe_enabled AND b.account_id>0 AND b.key_cipher<>'' AND b.probe_model<>''")
	require.Contains(t, nextSiteDueQuery, "key_health'->>'next_check_at'")
	require.NotContains(t, nextSiteDueQuery, "fast_observe_enabled")
}

func TestSQLNextSiteDueAtEmptyAndFailure(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectQuery(regexp.QuoteMeta(nextSiteDueQuery)).WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(nil))
	got, err := store.(SiteScheduleStore).NextSiteDueAt(t.Context())
	require.NoError(t, err)
	require.Nil(t, got)
	want := errors.New("database unavailable")
	mock.ExpectQuery(regexp.QuoteMeta(nextSiteDueQuery)).WillReturnError(want)
	_, err = store.(SiteScheduleStore).NextSiteDueAt(t.Context())
	require.ErrorIs(t, err, want)
}
