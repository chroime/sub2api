//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/stretchr/testify/require"
)

type paymentAdminMailRecords struct {
	mu   sync.Mutex
	rows map[string]balancenotice.Record
}

func (s *paymentAdminMailRecords) Save(_ context.Context, r *balancenotice.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = make(map[string]balancenotice.Record)
	}
	s.rows[r.NotificationID] = *r
	return nil
}
func (s *paymentAdminMailRecords) List(context.Context, balancenotice.Query, time.Time) (*balancenotice.Page, error) {
	return nil, nil
}
func (s *paymentAdminMailRecords) Get(context.Context, int64, time.Time) (*balancenotice.Record, error) {
	return nil, balancenotice.ErrNotFound
}
func (s *paymentAdminMailRecords) Prune(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
func (s *paymentAdminMailRecords) snapshot() []balancenotice.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]balancenotice.Record, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, row)
	}
	return rows
}

func adminRechargeFulfillmentFixture(t *testing.T, hooks ...notificationEmailSMTPHooks) (*PaymentService, *dbent.PaymentOrder, *notificationEmailMemorySettingRepo, *paymentAdminMailRecords, *notificationEmailTestSMTPServer) {
	t.Helper()
	client := newPaymentConfigServiceTestClient(t)
	user := client.User.Create().SetEmail("payer@example.test").SetUsername("充值用户").SetPasswordHash("fixture").SetBalance(25.14).SaveX(t.Context())
	order := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).
		SetAmount(0.14).SetPayAmount(0.10).SetRechargeCode("ADMIN-NOTICE-FIXTURE").SetOutTradeNo("20261111ADMINNOTICE").
		SetPaymentType("alipay").SetPaymentTradeNo("fixture-trade").SetStatus(OrderStatusRecharging).
		SetProviderSnapshot(map[string]any{"currency": "CNY"}).SetExpiresAt(time.Now().Add(time.Minute)).
		SetClientIP("127.0.0.1").SetSrcHost("fixture.test").SaveX(t.Context())
	smtp := startNotificationEmailTestSMTPServer(t, hooks...)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(t.Context(), smtp.settings()))
	require.NoError(t, repo.SetMultiple(t.Context(), map[string]string{
		"PAYMENT_ADMIN_RECHARGE_NOTIFY_ENABLED": "true",
		"PAYMENT_ADMIN_RECHARGE_NOTIFY_EMAILS":  `["admin@example.test","second@example.test"]`,
		SettingKeyAccountQuotaNotifyEnabled:     "false",
		SettingKeyAccountQuotaNotifyEmails:      `[{"email":"quota-only@example.test","verified":true}]`,
	}))
	records := &paymentAdminMailRecords{}
	notifications := NewNotificationEmailService(repo, NewEmailService(repo, nil))
	notifications.SetBalanceNotificationRecords(balancenotice.NewService(records))
	svc := &PaymentService{entClient: client, configService: NewPaymentConfigService(client, repo, nil), notificationEmailService: notifications}
	return svc, order, repo, records, smtp
}

func TestPaymentAdminRechargeSendsAfterCompletionDespiteUserEmailFailure(t *testing.T) {
	hooks := notificationEmailSMTPHooks{beforeCommand: func(command string) (string, bool, bool) {
		if command == "RCPT TO:<PAYER@EXAMPLE.TEST>" {
			return "550 5.1.1 User mailbox rejected", false, true
		}
		return "", false, false
	}}
	svc, order, _, records, smtp := adminRechargeFulfillmentFixture(t, hooks)
	require.Empty(t, records.snapshot(), "an unpaid or unfulfilled order must not send administrator email")
	lease := &paymentFulfillmentLease{version: order.UpdatedAt}
	require.NoError(t, svc.markCompleted(t.Context(), order, lease, "RECHARGE_SUCCESS"))
	require.Eventually(t, func() bool {
		rows := records.snapshot()
		return len(rows) == 2 && rows[0].Status == "sent" && rows[1].Status == "sent"
	}, 3*time.Second, 5*time.Millisecond, "both configured payment recipients must receive their own recorded notification")
	for _, row := range records.snapshot() {
		require.Equal(t, "payment.recharge_success_admin", row.EventType)
		require.Equal(t, order.UserID, row.UserID)
		require.Equal(t, order.UserEmail, row.UserEmail)
		require.Contains(t, []string{"admin@example.test", "second@example.test"}, row.Recipient)
		require.Contains(t, row.Body, "payer@example.test")
		require.Contains(t, row.Body, "CNY 0.10")
		require.Contains(t, row.Body, "USD 0.14")
		require.Contains(t, row.Body, "20261111ADMINNOTICE")
		require.NotContains(t, row.Body, "{{")
	}
	require.Equal(t, int64(2), smtp.messageCount(), "failed user notification must not block administrators")
	reloaded := svc.entClient.PaymentOrder.GetX(t.Context(), order.ID)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.NotNil(t, reloaded.CompletedAt)
	require.NoError(t, svc.markCompleted(t.Context(), order, lease, "RECHARGE_SUCCESS"))
	require.Eventually(t, func() bool { return smtp.connections.Load() >= 3 }, time.Second, 5*time.Millisecond)
	require.Len(t, records.snapshot(), 2, "repeated completion callbacks must not start a second administrator notification")
}

func TestPaymentAdminRechargeSMTPFailureDoesNotChangeFulfillment(t *testing.T) {
	hooks := notificationEmailSMTPHooks{beforeCommand: func(command string) (string, bool, bool) {
		if command == "RCPT TO:<ADMIN@EXAMPLE.TEST>" {
			return "550 5.1.1 Admin mailbox rejected", false, true
		}
		return "", false, false
	}}
	svc, order, _, records, smtp := adminRechargeFulfillmentFixture(t, hooks)
	require.NoError(t, svc.markCompleted(t.Context(), order, &paymentFulfillmentLease{version: order.UpdatedAt}, "RECHARGE_SUCCESS"))
	require.Eventually(t, func() bool {
		states := map[string]int{}
		for _, row := range records.snapshot() {
			states[row.Status]++
		}
		return states["failed"] == 1 && states["sent"] == 1
	}, 3*time.Second, 5*time.Millisecond)
	require.Equal(t, OrderStatusCompleted, svc.entClient.PaymentOrder.GetX(t.Context(), order.ID).Status)
	require.Equal(t, 25.14, svc.entClient.User.GetX(t.Context(), order.UserID).Balance)
	require.Eventually(t, func() bool { return smtp.messageCount() == 2 }, time.Second, 5*time.Millisecond, "the second administrator and the user must still receive mail")
}

func TestPaymentAdminRechargeDisabledDoesNotSend(t *testing.T) {
	svc, order, repo, records, smtp := adminRechargeFulfillmentFixture(t)
	require.NoError(t, repo.Set(t.Context(), "PAYMENT_ADMIN_RECHARGE_NOTIFY_ENABLED", "false"))
	require.NoError(t, svc.markCompleted(t.Context(), order, &paymentFulfillmentLease{version: order.UpdatedAt}, "RECHARGE_SUCCESS"))
	require.Eventually(t, func() bool { return smtp.messageCount() == 1 }, time.Second, 5*time.Millisecond)
	require.Empty(t, records.snapshot(), "disabled administrator notifications must leave user email behavior intact")
}

func TestPaymentAdminRechargeRequiresCompletedBalanceOrder(t *testing.T) {
	svc, order, _, records, smtp := adminRechargeFulfillmentFixture(t)
	for _, status := range []string{OrderStatusPending, OrderStatusPaid, OrderStatusRecharging, OrderStatusCancelled, OrderStatusExpired, OrderStatusFailed} {
		candidate := *order
		candidate.Status = status
		completed := time.Now()
		candidate.CompletedAt = &completed
		require.NoError(t, svc.sendAdminRechargeSuccessNotifications(t.Context(), &candidate))
	}
	candidate := *order
	candidate.Status = OrderStatusCompleted
	candidate.CompletedAt = nil
	require.NoError(t, svc.sendAdminRechargeSuccessNotifications(t.Context(), &candidate))
	completed := time.Now()
	candidate.CompletedAt = &completed
	candidate.OrderType = "subscription"
	require.NoError(t, svc.sendAdminRechargeSuccessNotifications(t.Context(), &candidate))
	require.Zero(t, smtp.messageCount())
	require.Empty(t, records.snapshot())
}
