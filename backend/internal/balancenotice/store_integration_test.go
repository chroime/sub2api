package balancenotice

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// All writes are confined to a fresh schema. No application row is read or changed.
func TestSQLStorePostgres(t *testing.T) {
	dsn := os.Getenv("BALANCE_NOTICE_TEST_DSN")
	if dsn == "" {
		t.Skip("set BALANCE_NOTICE_TEST_DSN for a loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	schema := fmt.Sprintf("balance_notice_fixture_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() {
		_, e := db.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		require.NoError(t, e)
	}()
	params := u.Query()
	params.Set("search_path", schema)
	u.RawQuery = params.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer fixture.Close()
	migration, err := os.ReadFile("../../migrations/275_user_balance_notification_records.sql")
	require.NoError(t, err)
	_, err = fixture.Exec(string(migration))
	require.NoError(t, err)
	precisionMigration, err := os.ReadFile("../../migrations/276_user_balance_notification_threshold_precision.sql")
	require.NoError(t, err)
	_, err = fixture.Exec(string(precisionMigration))
	require.NoError(t, err)
	s := NewService(NewSQLStore(fixture))
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	first := s.Begin(Input{TriggerType: "automatic", UserID: 7, UserEmail: "user@example.test", UserName: "原名称", Recipient: "notify@example.test", Balance: amount(0.065), Threshold: amount(1), DeliveryKey: "same-daily-key"})
	first.Content("实际余额提醒", "<p>实际正文0.065</p>")
	first.Attempt()
	first.Sent()
	second := s.Begin(Input{TriggerType: "automatic", UserID: 7, UserEmail: "user@example.test", Recipient: "notify@example.test", DeliveryKey: "same-daily-key"})
	second.Suppress("duplicate")
	third := s.Begin(Input{TriggerType: "manual_batch", BatchID: "batch-fixture", UserID: 8, UserEmail: "other@example.test", Recipient: "other@example.test"})
	third.Attempt()
	third.Fail(fmt.Errorf("smtp rcpt: 550 invalid mailbox"))
	page, err := s.List(t.Context(), Query{})
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.Equal(t, Summary{Total: 3, Sent: 1, Failed: 1, Suppressed: 1}, page.Summary)
	for _, row := range page.Items {
		require.Empty(t, row.Body)
		require.Empty(t, row.DeliveryKey)
	}
	filtered, err := s.List(t.Context(), Query{UserID: 7, Status: "sent", UserEmail: "USER@", Recipient: "NOTIFY@"})
	require.NoError(t, err)
	require.EqualValues(t, 1, filtered.Total)
	id := filtered.Items[0].ID
	actual, err := s.Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "<p>实际正文0.065</p>", actual.Body)
	require.Equal(t, "原名称", actual.UserName)
	require.Equal(t, 0.065, *actual.Balance)
	require.Equal(t, 1, actual.Attempts)
	for _, query := range []Query{{UserEmail: "user' OR TRUE--"}, {Recipient: "%"}, {TriggerType: "manual_batch", Status: "sent"}} {
		page, err = s.List(t.Context(), query)
		require.NoError(t, err)
		require.Zero(t, page.Total)
	}
	page, err = s.List(t.Context(), Query{Page: 5, PageSize: 2})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.EqualValues(t, 3, page.Total)
	require.Equal(t, 2, page.Pages)
	_, err = fixture.Exec(`UPDATE user_balance_notification_records SET expires_at=$1 WHERE id=$2`, now, id)
	require.NoError(t, err)
	_, err = s.Get(t.Context(), id)
	require.ErrorIs(t, err, ErrNotFound)
	page, err = s.List(t.Context(), Query{})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.NoError(t, s.Cleanup(context.Background()))
	var count int
	require.NoError(t, fixture.QueryRow(`SELECT COUNT(*) FROM user_balance_notification_records`).Scan(&count))
	require.Equal(t, 2, count)
	tiny := s.Begin(Input{TriggerType: "automatic", UserID: 9, Recipient: "tiny@example.test", Balance: amount(0), Threshold: amount(0.0000000001)})
	tiny.Suppress("duplicate")
	tinyPage, err := s.List(t.Context(), Query{UserID: 9})
	require.NoError(t, err)
	require.Len(t, tinyPage.Items, 1)
	require.Equal(t, 0.0000000001, *tinyPage.Items[0].Threshold)
	_, err = fixture.Exec(`UPDATE user_balance_notification_records SET status='delivered'`)
	require.Error(t, err)
}
