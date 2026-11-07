package upstreamgovernance

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSQLStoreChangeNotificationQueuePersistsAndDeduplicates(t *testing.T) {
	store, mock := storeFixture(t)
	sqlStore := store.(*sqlStore)
	now := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	item := ChangeNotification{
		SiteID: 5, GroupResource: "42", LocalGroupID: 7, DedupKey: "site:5:rate:2", Recipient: "admin@example.test",
		Kind: "rate_change", Severity: "warning", Subject: "rate", Body: "changed",
		Status: ChangeNotificationPending, NextAttemptAt: now, CreatedAt: now,
	}
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO upstream_governance_change_notifications")).WithArgs(int64(5), "site:5:rate:2", "admin@example.test", "rate_change", "warning", "rate", "changed", false, "pending", 0, now, nil, "", now, "42", int64(7)).WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, sqlStore.EnqueueChangeNotification(context.Background(), item))

	// SQL uniqueness is the final cross-process deduplication boundary. A
	// duplicate insert is deliberately treated as success by the store.
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO upstream_governance_change_notifications")).WithArgs(int64(5), "site:5:rate:2", "admin@example.test", "rate_change", "warning", "rate", "changed", false, "pending", 0, now, nil, "", now, "42", int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, sqlStore.EnqueueChangeNotification(context.Background(), item))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+changeNotificationColumns+" FROM upstream_governance_change_notifications")).WithArgs(now, 10).WillReturnRows(sqlmock.NewRows(strings.Split(changeNotificationColumns, ",")).AddRow(1, 5, item.DedupKey, item.Recipient, item.Kind, item.Severity, item.Subject, item.Body, false, "pending", 0, now, nil, "", now, "42", 7).AddRow(2, 5, "legacy", item.Recipient, item.Kind, item.Severity, item.Subject, item.Body, false, "pending", 0, now, nil, "", now, nil, nil))
	items, err := sqlStore.DueChangeNotifications(t.Context(), now, 10)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "42", items[0].GroupResource)
	require.Equal(t, int64(7), items[0].LocalGroupID)
	require.Empty(t, items[1].GroupResource)
	require.Zero(t, items[1].LocalGroupID)
}

func TestSQLStoreChangeNotificationQueueClaimsAndCompletes(t *testing.T) {
	store, mock := storeFixture(t)
	sqlStore := store.(*sqlStore)
	now := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	claimUntil := now.Add(10 * time.Minute)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE upstream_governance_change_notifications SET status='sending'")).WithArgs(int64(9), claimUntil, now).WillReturnResult(sqlmock.NewResult(0, 1))
	claimed, err := sqlStore.ClaimChangeNotification(context.Background(), 9, now, claimUntil)
	require.NoError(t, err)
	require.True(t, claimed)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE upstream_governance_change_notifications SET status='sent'")).WithArgs(int64(9), now).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, sqlStore.CompleteChangeNotification(context.Background(), 9, now, nil, now))

	mock.ExpectExec(regexp.QuoteMeta("UPDATE upstream_governance_change_notifications SET status='pending'")).WithArgs(int64(9), "smtp unavailable", now.Add(15*time.Minute)).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, sqlStore.CompleteChangeNotification(context.Background(), 9, now, errSMTPUnavailable{}, now.Add(15*time.Minute)))
}

func TestSQLStoreLoadsChangeNotificationPolicy(t *testing.T) {
	store, mock := storeFixture(t)
	sqlStore := store.(*sqlStore)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT policy FROM upstream_governance_pricing_notifications WHERE site_id=$1")).WithArgs(int64(5)).WillReturnRows(sqlmock.NewRows([]string{"policy"}).AddRow(`{"enabled":true,"recipients":["ops@example.test"],"group_changes":false,"rate_changes":true,"pricing_changes":true,"protection_changes":true}`))
	policy, err := sqlStore.LoadChangeNotificationPolicy(context.Background(), 5)
	require.NoError(t, err)
	require.True(t, policy.Enabled)
	require.False(t, policy.GroupChanges)
	require.Equal(t, []string{"ops@example.test"}, policy.Recipients)
}

type errSMTPUnavailable struct{}

func (errSMTPUnavailable) Error() string { return "smtp unavailable" }
