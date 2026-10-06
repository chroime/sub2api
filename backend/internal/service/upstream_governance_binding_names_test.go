package service

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceAccountNamesSelectLiveAndDeletedMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	local := &governanceLocalAccounts{db: db}
	query := regexp.QuoteMeta(`SELECT id, name, deleted_at IS NOT NULL FROM accounts WHERE id = ANY($1)`)
	mock.ExpectQuery(query).WithArgs("{8,9}").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "deleted"}).AddRow(8, "Renamed live account", false).AddRow(9, "Historical account", true))
	names, err := local.AccountNames(t.Context(), []int64{8, 9})
	require.NoError(t, err)
	require.Equal(t, map[int64]gov.LocalAccountName{8: {Name: "Renamed live account"}, 9: {Name: "Historical account", Deleted: true}}, names)
	names, err = local.AccountNames(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, names)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGovernanceAccountNamesReportReadFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	local := &governanceLocalAccounts{db: db}
	readErr := errors.New("fixture read failure")
	mock.ExpectQuery("SELECT id, name, deleted_at IS NOT NULL FROM accounts").WithArgs("{8}").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "deleted"}).AddRow(8, "Live account", false).RowError(0, readErr))
	_, err = local.AccountNames(t.Context(), []int64{8})
	require.ErrorIs(t, err, readErr)
	require.NoError(t, mock.ExpectationsWereMet())
}
