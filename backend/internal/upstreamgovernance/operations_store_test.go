package upstreamgovernance

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOperationsSQLWorkbenchIsReadOnlyAndDoesNotCapSitesAt1000(t *testing.T) {
	s, m := storeFixture(t)
	now := time.Now().UTC()
	m.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id", "name", "base_url", "enabled", "has_credential", "status", "version", "full_seconds", "last_attempt", "snapshot_at", "snapshot_version", "fast_enabled", "fast_seconds", "fast_status", "fast_at", "next_fast", "balance_enabled", "balance_state", "key_count", "fast_source_user_id", "fast_complete", "fast_revision", "snapshot_user_id", "key_pending", "key_unknown", "key_protection"})
	for i := 1; i <= 1005; i++ {
		rows.AddRow(i, "Fixture", "https://example.test", true, true, "connected", 1, 60, nil, now, 1, false, 5, "idle", nil, nil, false, "disabled", 1, 1, true, 1, "1", 0, 0, 0)
	}
	m.ExpectQuery("SELECT s.id,s.name,s.base_url").WithArgs(int64(0)).WillReturnRows(rows)
	m.ExpectQuery("SELECT DISTINCT p.local_group_id").WithArgs(int64(0)).WillReturnRows(sqlmock.NewRows([]string{"group_id", "group_name", "reason", "updated_at", "site_id"}))
	m.ExpectQuery("SELECT site_id,COUNT").WithArgs(int64(0)).WillReturnRows(sqlmock.NewRows([]string{"site_id", "count", "status", "next_attempt_at"}))
	m.ExpectCommit()
	result, err := s.(*sqlStore).ReadWorkbench(context.Background(), OperationsQuery{Page: 51, PageSize: 20}, now)
	require.NoError(t, err)
	require.Equal(t, int64(1005), result.Total)
	require.Len(t, result.Items, 5)
	require.Equal(t, 1005, result.Summary.Critical)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestOperationsSQLTimelineStrictScopeAndEmptyPageRetainsTotal(t *testing.T) {
	s, m := storeFixture(t)
	now := time.Now().UTC()
	m.ExpectBegin()
	m.ExpectQuery("SELECT id,name,base_url FROM upstream_governance_sites WHERE id=\\$1").WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "base_url"}).AddRow(2, "Other", "https://other.example.test"))
	m.ExpectQuery("WITH scoped_groups AS[\\s\\S]+SELECT COUNT").WithArgs(int64(2), "notification").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
	m.ExpectQuery("WITH scoped_groups AS[\\s\\S]+ORDER BY created_at DESC,kind,record_id DESC LIMIT \\$3 OFFSET \\$4").WithArgs(int64(2), "notification", 20, 20).WillReturnRows(sqlmock.NewRows([]string{"record_id", "kind", "resource_id", "resource_name", "status", "reason", "created_at", "shared", "acknowledged", "before_value", "after_value", "before_rate", "after_rate", "before_cost", "after_cost", "attempts", "next_attempt_at", "sent_at", "has_error"}))
	m.ExpectCommit()
	result, err := s.(*sqlStore).ReadTimeline(context.Background(), OperationsQuery{SiteID: 2, Page: 2, PageSize: 20, Kind: "notification"}, now)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.Total)
	require.Empty(t, result.Items)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestOperationsSQLRejectsMissingSite(t *testing.T) {
	s, m := storeFixture(t)
	m.ExpectBegin()
	m.ExpectQuery("SELECT s.id,s.name,s.base_url").WithArgs(int64(98)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	m.ExpectRollback()
	_, err := s.(*sqlStore).ReadWorkbench(context.Background(), OperationsQuery{SiteID: 98, Page: 1, PageSize: 20}, time.Now())
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, m.ExpectationsWereMet())
}
