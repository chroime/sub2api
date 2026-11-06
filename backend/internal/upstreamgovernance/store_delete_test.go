package upstreamgovernance

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

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

func TestSQLStoreDeleteSiteKeepingAccountsDetachesImportedAccounts(t *testing.T) {
	s, m := storeFixture(t)
	expectDeleteSiteLock(m, 1)
	m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM upstream_governance_key_repairs WHERE site_id=\$1\)`).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"repairing"}).AddRow(false))
	m.ExpectQuery(`UPDATE accounts SET extra=COALESCE\(extra,'\{\}'::jsonb\)-'upstream_governance_rate_owner'-'upstream_governance_pause'-'upstream_governance_reconcile_receipt'[\s\S]*RETURNING id`).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(31)).AddRow(int64(32)))
	m.ExpectExec(`INSERT INTO scheduler_outbox \(event_type, account_id\) VALUES \('account_changed', \$1\)`).WithArgs(int64(31)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(`INSERT INTO scheduler_outbox \(event_type, account_id\) VALUES \('account_changed', \$1\)`).WithArgs(int64(32)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(`DELETE FROM upstream_governance_bindings WHERE site_id=\$1`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 3))
	m.ExpectExec(`DELETE FROM upstream_governance_keys WHERE site_id=\$1`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec(`DELETE FROM upstream_governance_sites WHERE id=\$1`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	kept, err := s.(SiteDetacher).DeleteSiteKeepingAccounts(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 2, kept)
}

func TestSQLStoreDeleteSiteKeepingAccountsWaitsForKeyRepairs(t *testing.T) {
	s, m := storeFixture(t)
	expectDeleteSiteLock(m, 1)
	m.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM upstream_governance_key_repairs WHERE site_id=\$1\)`).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"repairing"}).AddRow(true))
	m.ExpectRollback()

	_, err := s.(SiteDetacher).DeleteSiteKeepingAccounts(t.Context(), 1)
	require.Equal(t, "site_in_use", ErrorCode(err))
}

func TestSQLStoreListDeliveryRecordsReadsEitherChannel(t *testing.T) {
	sent := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		channel, table string
		row            []driver.Value
		want           DeliveryRecord
	}{
		{DeliveryChannelEmail, "upstream_governance_change_notifications",
			[]driver.Value{int64(9), int64(3), "DD批发站", int64(0), "", "group_changes", "分组变化", "ops@example.com", "sent", 1, "", sent, sent},
			DeliveryRecord{ID: 9, Channel: DeliveryChannelEmail, SiteID: 3, SiteName: "DD批发站", Kind: "group_changes", Subject: "分组变化", Recipient: "ops@example.com", Status: "sent", Attempts: 1, CreatedAt: sent, SentAt: &sent}},
		{DeliveryChannelAstrBot, "upstream_governance_local_group_changes",
			[]driver.Value{int64(4), int64(0), "", int64(52), "Grok Heavy", "updated", "", "", "pending", 2, "timeout", sent, nil},
			DeliveryRecord{ID: 4, Channel: DeliveryChannelAstrBot, GroupID: 52, GroupName: "Grok Heavy", Kind: "updated", Status: "pending", Attempts: 2, LastError: "timeout", CreatedAt: sent}},
	} {
		s, m := storeFixture(t)
		m.ExpectQuery(`SELECT COUNT\(\*\) FROM ` + tc.table).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(41)))
		m.ExpectQuery(`FROM `+tc.table+`[\s\S]*ORDER BY [a-z]\.id DESC LIMIT \$1 OFFSET \$2`).WithArgs(20, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id", "site_id", "site_name", "group_id", "group_name", "kind", "subject", "recipient", "status", "attempts", "last_error", "created_at", "sent_at"}).AddRow(tc.row...))
		records, total, err := s.(DeliveryRecordReader).ListDeliveryRecords(t.Context(), tc.channel, 2, 20)
		require.NoError(t, err, tc.channel)
		require.Equal(t, int64(41), total)
		require.Equal(t, []DeliveryRecord{tc.want}, records, tc.channel)
	}
}

func TestDeliveryRecordsRejectsUnknownChannels(t *testing.T) {
	_, _, err := NewService(nil, nil, nil, nil, false).DeliveryRecords(t.Context(), "sms", 1, 20)
	require.ErrorIs(t, err, ErrInvalid)
}
