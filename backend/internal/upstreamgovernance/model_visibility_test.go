package upstreamgovernance

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestHideModelRunRemovesOnlyPublicProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := NewService(NewSQLStore(db), nil, nil, nil, false)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE upstream_governance_model_runs SET public_visible=FALSE WHERE site_id=$1 AND id=$2`)).
		WithArgs(int64(7), "run-id").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, svc.HideModelRun(t.Context(), 7, "run-id"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHideModelRunReportsMissingAndRejectsInvalidInput(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := NewService(NewSQLStore(db), nil, nil, nil, false)
	for _, tc := range []struct {
		name   string
		siteID int64
		runID  string
	}{
		{name: "zero site", siteID: 0, runID: "run-id"},
		{name: "blank run", siteID: 7, runID: "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, svc.HideModelRun(t.Context(), tc.siteID, tc.runID), ErrInvalid)
		})
	}
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE upstream_governance_model_runs SET public_visible=FALSE WHERE site_id=$1 AND id=$2`)).
		WithArgs(int64(7), "missing").
		WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, svc.HideModelRun(t.Context(), 7, "missing"), ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
