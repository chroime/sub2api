package upstreamgovernance

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectDeleteSiteLock(m sqlmock.Sqlmock, id int64) {
	m.ExpectBegin()
	m.ExpectQuery(`SELECT id FROM upstream_governance_sites WHERE id=\$1 FOR UPDATE`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
}

func TestSQLStoreDeleteSiteRemovesOrphanBindings(t *testing.T) {
	s, m := storeFixture(t)
	expectDeleteSiteLock(m, 1)
	m.ExpectQuery(`SELECT EXISTS[\s\S]*upstream_governance_bindings[\s\S]*upstream_governance_keys`).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"in_use"}).AddRow(false))
	m.ExpectExec(`DELETE FROM upstream_governance_bindings[\s\S]*account_id>0[\s\S]*deleted_at IS NULL`).WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec(`DELETE FROM upstream_governance_sites WHERE id=\$1`).WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	require.NoError(t, s.DeleteSite(t.Context(), 1))
}

func TestSQLStoreDeleteSiteInUseLeavesOrphansUntouched(t *testing.T) {
	s, m := storeFixture(t)
	expectDeleteSiteLock(m, 1)
	m.ExpectQuery(`SELECT EXISTS[\s\S]*upstream_governance_bindings[\s\S]*upstream_governance_keys`).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"in_use"}).AddRow(true))
	m.ExpectRollback()
	require.Equal(t, "site_in_use", ErrorCode(s.DeleteSite(t.Context(), 1)))
}

func TestSQLStoreDeleteSiteMissing(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectBegin()
	m.ExpectQuery(`SELECT id FROM upstream_governance_sites WHERE id=\$1 FOR UPDATE`).WithArgs(int64(1)).
		WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	require.ErrorIs(t, s.DeleteSite(t.Context(), 1), ErrNotFound)
}

func TestSQLStoreDeleteSiteRollsBackCleanupFailure(t *testing.T) {
	for _, step := range []string{"blockers", "orphans", "site", "commit"} {
		t.Run(step, func(t *testing.T) {
			s, m := storeFixture(t)
			expectDeleteSiteLock(m, 1)
			failure := errors.New("delete fixture failure")
			blockers := m.ExpectQuery(`SELECT EXISTS[\s\S]*upstream_governance_bindings[\s\S]*upstream_governance_keys`).WithArgs(int64(1))
			if step == "blockers" {
				blockers.WillReturnError(failure)
			} else {
				blockers.WillReturnRows(sqlmock.NewRows([]string{"in_use"}).AddRow(false))
				orphans := m.ExpectExec(`DELETE FROM upstream_governance_bindings`).WithArgs(int64(1))
				if step == "orphans" {
					orphans.WillReturnError(failure)
				} else {
					orphans.WillReturnResult(sqlmock.NewResult(0, 1))
					site := m.ExpectExec(`DELETE FROM upstream_governance_sites WHERE id=\$1`).WithArgs(int64(1))
					if step == "site" {
						site.WillReturnError(failure)
					} else {
						site.WillReturnResult(sqlmock.NewResult(0, 1))
						m.ExpectCommit().WillReturnError(failure)
					}
				}
			}
			if step != "commit" {
				m.ExpectRollback()
			}
			require.ErrorIs(t, s.DeleteSite(t.Context(), 1), failure)
		})
	}
}
