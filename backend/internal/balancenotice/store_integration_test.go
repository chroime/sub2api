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
	// A row written by the previous version must stay visible and retain its
	// low-balance classification after the additive migration.
	var legacyID int64
	require.NoError(t, fixture.QueryRow(`INSERT INTO user_balance_notification_records(notification_id,trigger_type,user_id,recipient,subject,status) VALUES('00000000-0000-0000-0000-000000000001','automatic',77,'legacy@example.test','Existing balance notice','sent') RETURNING id`).Scan(&legacyID))
	eventMigration, err := os.ReadFile("../../migrations/287_user_notification_record_event_type.sql")
	require.NoError(t, err)
	_, err = fixture.Exec(string(eventMigration))
	require.NoError(t, err)
	_, err = fixture.Exec(`INSERT INTO user_balance_notification_records(notification_id,trigger_type,user_id,recipient,subject,status,event_type) VALUES('00000000-0000-0000-0000-000000000002','automatic',77,'admin@example.test','Recharge admin notice','sent','payment.recharge_success_admin')`)
	require.Error(t, err, "the previous event constraint must reject the newly introduced event")
	adminEventMigration, err := os.ReadFile("../../migrations/288_payment_recharge_admin_notification_event.sql")
	require.NoError(t, err)
	_, err = fixture.Exec(string(adminEventMigration))
	require.NoError(t, err)
	var legacyEvent, legacySubject string
	require.NoError(t, fixture.QueryRow(`SELECT event_type,subject FROM user_balance_notification_records WHERE id=$1`, legacyID).Scan(&legacyEvent, &legacySubject))
	require.Equal(t, EventBalanceLow, legacyEvent)
	require.Equal(t, "Existing balance notice", legacySubject)
	_, err = fixture.Exec(`DELETE FROM user_balance_notification_records WHERE id=$1`, legacyID)
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
	health := s.Begin(Input{EventType: EventSystemHealthSnapshot, TriggerType: "manual_batch", BatchID: "health-batch-fixture", UserID: 7, UserEmail: "user@example.test", Recipient: "user@example.test"})
	health.Content("Actual status", "<p>Actual system status</p>")
	health.Attempt()
	health.Sent()
	healthPage, err := s.List(t.Context(), Query{EventType: EventSystemHealthSnapshot, TriggerType: "manual_batch", Status: "sent"})
	require.NoError(t, err)
	require.EqualValues(t, 1, healthPage.Total)
	require.Equal(t, Summary{Total: 1, Sent: 1}, healthPage.Summary)
	require.Equal(t, EventSystemHealthSnapshot, healthPage.Items[0].EventType)
	require.Nil(t, healthPage.Items[0].Balance)
	require.Nil(t, healthPage.Items[0].Threshold)
	require.Empty(t, healthPage.Items[0].Body)
	healthDetail, err := s.Get(t.Context(), healthPage.Items[0].ID)
	require.NoError(t, err)
	require.Equal(t, EventSystemHealthSnapshot, healthDetail.EventType)
	require.Equal(t, "<p>Actual system status</p>", healthDetail.Body)
	balancePage, err := s.List(t.Context(), Query{EventType: EventBalanceLow})
	require.NoError(t, err)
	require.EqualValues(t, 3, balancePage.Total)
	for _, r := range balancePage.Items {
		require.Equal(t, EventBalanceLow, r.EventType)
	}
	_, err = fixture.Exec(`UPDATE user_balance_notification_records SET event_type='auth.verify_code' WHERE id=$1`, healthDetail.ID)
	require.Error(t, err)
	for _, outcome := range []string{"sent", "failed"} {
		admin := s.Begin(Input{EventType: EventPaymentRechargeSuccessAdmin, TriggerType: "automatic", UserID: 42, UserEmail: "payer@example.test", UserName: "充值用户", Recipient: outcome + "-admin@example.test", DeliveryKey: "payment-order-123-" + outcome})
		require.NotNil(t, admin)
		admin.Content("用户充值成功", "<p>订单123：实付 ¥10.00，到账 $10.00</p>")
		admin.Attempt()
		if outcome == "sent" {
			admin.Sent()
		} else {
			admin.Fail(fmt.Errorf("smtp temporary failure"))
		}
	}
	adminPage, err := s.List(t.Context(), Query{EventType: EventPaymentRechargeSuccessAdmin, TriggerType: "automatic", UserID: 42})
	require.NoError(t, err)
	require.EqualValues(t, 2, adminPage.Total)
	require.Equal(t, Summary{Total: 2, Sent: 1, Failed: 1}, adminPage.Summary)
	for _, row := range adminPage.Items {
		require.Equal(t, EventPaymentRechargeSuccessAdmin, row.EventType)
		require.Equal(t, "payer@example.test", row.UserEmail)
		require.Equal(t, "充值用户", row.UserName)
		require.Nil(t, row.Balance)
		require.Nil(t, row.Threshold)
		detail, err := s.Get(t.Context(), row.ID)
		require.NoError(t, err)
		require.Equal(t, "<p>订单123：实付 ¥10.00，到账 $10.00</p>", detail.Body)
		require.Equal(t, "payment-order-123-"+row.Status, detail.DeliveryKey)
	}
	_, err = fixture.Exec(`UPDATE user_balance_notification_records SET event_type='payment.recharge_success_admin.unknown' WHERE id=$1`, healthDetail.ID)
	require.Error(t, err)
}
