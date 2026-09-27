package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestBalanceMonitorRuntimeWriteDoesNotChangeSiteVersion(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE upstream_governance_sites SET balance_monitor_state=$2::jsonb WHERE id=$1`)).WithArgs(int64(1), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	err := store.SaveBalanceMonitorState(context.Background(), 1, BalanceMonitorState{Status: BalanceMonitorStatus{State: "low"}}, nil)
	require.NoError(t, err)
}

func TestBalanceMonitorStateAndTransitionEventRollBackTogether(t *testing.T) {
	store, mock := storeFixture(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE upstream_governance_sites SET balance_monitor_state`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO upstream_governance_events`).WillReturnError(errors.New("event unavailable"))
	mock.ExpectRollback()
	err := store.SaveBalanceMonitorState(context.Background(), 1, BalanceMonitorState{Status: BalanceMonitorStatus{State: "low"}}, []Event{{Kind: "balance_low", CreatedAt: time.Now()}})
	require.Error(t, err)
}

func TestBalanceMonitorSQLScanKeepsNativeUnitAndHidesRecipientReservations(t *testing.T) {
	store, mock := storeFixture(t)
	columns := []string{"id", "name", "platform", "base_url", "proxy_id", "enabled", "interval_minutes", "version", "session_cipher", "status", "last_error", "last_sync_at", "next_sync_at", "created_at", "updated_at", "balance_monitor", "balance_monitor_state", "login_cipher"}
	now := time.Now()
	mock.ExpectQuery(`SELECT .* FROM upstream_governance_sites WHERE id`).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows(columns).AddRow(1, "Fixture", "newapi", "https://example.com", nil, true, 15, 1, "", "healthy", "", nil, now, now, now, `{"threshold":10,"cooldown_minutes":1440}`, `{"status":{"state":"low","last_error":"email_delivery_failed"},"recipients":{"fixture-hash":{"failed":true}}}`, ""))
	site, err := store.GetSite(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "quota", site.BalanceMonitor.Unit)
	require.Equal(t, "email_delivery_failed", site.BalanceMonitorStatus.LastError)
	require.True(t, site.balanceState.Recipients["fixture-hash"].Failed)
	raw, err := json.Marshal(site)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fixture-hash")
	require.NotContains(t, string(raw), "next_attempt_at")
}
