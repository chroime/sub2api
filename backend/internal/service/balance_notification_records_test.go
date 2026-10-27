package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/stretchr/testify/require"
)

type balanceRecordStore struct {
	rows map[string]balancenotice.Record
	err  error
}

func (m *balanceRecordStore) Save(_ context.Context, r *balancenotice.Record) error {
	if m.err != nil {
		return m.err
	}
	if m.rows == nil {
		m.rows = map[string]balancenotice.Record{}
	}
	m.rows[r.NotificationID+r.Recipient] = *r
	return nil
}
func (m *balanceRecordStore) List(context.Context, balancenotice.Query, time.Time) (*balancenotice.Page, error) {
	return nil, nil
}
func (m *balanceRecordStore) Get(context.Context, int64, time.Time) (*balancenotice.Record, error) {
	return nil, balancenotice.ErrNotFound
}
func (m *balanceRecordStore) Prune(context.Context, time.Time, int) (int64, error) { return 0, nil }
func (m *balanceRecordStore) only(t *testing.T) balancenotice.Record {
	t.Helper()
	require.Len(t, m.rows, 1)
	for _, r := range m.rows {
		return r
	}
	panic("no row")
}

func balanceRecordFixture(t *testing.T) (*NotificationEmailService, *BalanceNotifyService, *notificationEmailMemorySettingRepo, *balanceRecordStore, *notificationEmailTestSMTPServer) {
	t.Helper()
	smtp := startNotificationEmailTestSMTPServer(t)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(t.Context(), smtp.settings()))
	for k, v := range map[string]string{SettingKeySiteName: "测试平台", notificationEmailUnsubscribeSecretKey: "test-unsubscribe-secret"} {
		require.NoError(t, repo.Set(t.Context(), k, v))
	}
	mail := NewEmailService(repo, nil)
	notifications := NewNotificationEmailService(repo, mail)
	store := &balanceRecordStore{}
	notifications.SetBalanceNotificationRecords(balancenotice.NewService(store))
	balance := NewBalanceNotifyService(mail, repo, nil)
	balance.SetNotificationEmailService(notifications)
	return notifications, balance, repo, store, smtp
}

func TestBalanceRecordsAutomaticSMTPAndDailySuppression(t *testing.T) {
	_, svc, _, store, smtp := balanceRecordFixture(t)
	svc.sendBalanceLowEmails([]string{"notify@example.test"}, 7, "用户7", "user@example.test", 0.065, 1, "Test", "https://example.test/pay")
	r := store.only(t)
	require.Equal(t, "sent", r.Status)
	require.Equal(t, "user@example.test", r.UserEmail)
	require.Equal(t, "notify@example.test", r.Recipient)
	require.Equal(t, 0.065, *r.Balance)
	require.NotEmpty(t, r.Body)
	require.NotContains(t, r.Body, "{{")
	require.Equal(t, 1, r.Attempts)
	require.Equal(t, int64(1), smtp.messageCount())
	// SMTP normalizes line endings to CRLF; the stored HTML is the rendered
	// content before transport encoding.
	require.Equal(t, strings.TrimSpace(r.Body), strings.TrimSpace(strings.ReplaceAll(smtp.lastMessageBody(t), "\r\n", "\n")))
	svc.sendBalanceLowEmails([]string{"notify@example.test"}, 7, "用户7", "user@example.test", 0.06, 1, "Test", "https://example.test/pay")
	require.Len(t, store.rows, 2)
	states := map[string]int{}
	for _, r := range store.rows {
		states[r.Status]++
	}
	require.Equal(t, map[string]int{"sent": 1, "suppressed": 1}, states)
	require.Equal(t, int64(1), smtp.messageCount())
}

func TestBalanceRecordsUnsubscribeNoRecipientAndFallback(t *testing.T) {
	n, svc, repo, store, smtp := balanceRecordFixture(t)
	require.NoError(t, repo.Set(t.Context(), notificationEmailPreferenceKey(NotificationEmailEventBalanceLow, "notify@example.test"), "unsubscribed"))
	svc.sendBalanceLowEmails([]string{"notify@example.test"}, 7, "用户", "u@example.test", 0.1, 1, "Test", "")
	r := store.only(t)
	require.Equal(t, "suppressed", r.Status)
	require.Equal(t, "unsubscribed", r.SuppressionReason)
	require.Zero(t, r.Attempts)
	require.Equal(t, int64(0), smtp.messageCount())
	store.rows = nil
	svc.sendBalanceLowEmails(nil, 7, "用户", "u@example.test", 0.1, 1, "Test", "")
	require.Equal(t, "no_recipient", store.only(t).SuppressionReason)
	store.rows = nil
	require.NoError(t, repo.Delete(t.Context(), notificationEmailPreferenceKey(NotificationEmailEventBalanceLow, "notify@example.test")))
	require.NoError(t, repo.Set(t.Context(), notificationEmailTemplateKey(NotificationEmailEventBalanceLow, "en"), `{"subject":"{{unsupported}}","html":"invalid"}`))
	svc.sendBalanceLowEmails([]string{"notify@example.test"}, 7, "用户", "u@example.test", 0.1, 1, "Test", "")
	r = store.only(t)
	require.Equal(t, "sent", r.Status)
	require.Equal(t, 1, r.Attempts)
	require.Contains(t, r.Subject, "Balance Low Alert")
	require.NotEmpty(t, r.Body)
	require.Equal(t, int64(1), smtp.messageCount())
	_ = n
}

func TestBalanceRecordsBatchSkipsAndOtherTemplatesAreExcluded(t *testing.T) {
	n, _, repo, store, smtp := balanceRecordFixture(t)
	require.NoError(t, repo.Set(t.Context(), SettingKeyBalanceLowNotifyThreshold, "1"))
	users := &bulkEmailUsersStub{users: map[int64]*User{7: {ID: 7, Email: "user7@example.test", Balance: 0.065, BalanceNotifyEnabled: true}, 8: {ID: 8, Email: "user8@example.test", Balance: 2, BalanceNotifyEnabled: true}, 9: {ID: 9, Email: "user9@example.test", Balance: 0.1, BalanceNotifyEnabled: false}}}
	svc := &UserBulkEmailService{users: users, settings: repo, sender: n, records: n.BalanceNotificationRecords()}
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{7, 8, 9}, Event: NotificationEmailEventBalanceLow, Locale: "zh"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 2, result.Skipped)
	require.Len(t, store.rows, 3)
	batches := map[string]bool{}
	for _, r := range store.rows {
		require.Equal(t, "manual_batch", r.TriggerType)
		require.NotEmpty(t, r.BatchID)
		batches[r.BatchID] = true
		if r.UserID == 7 {
			require.Equal(t, 0.065, *r.Balance)
		}
		if r.UserID == 9 {
			require.NotNil(t, r.Threshold)
			require.Equal(t, 1.0, *r.Threshold)
		}
	}
	require.Len(t, batches, 1)
	require.Equal(t, int64(1), smtp.messageCount())
	store.rows = nil
	_, err = svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{7}, Event: NotificationEmailEventSystemHealthSnapshot, Locale: "zh"})
	require.NoError(t, err)
	require.Empty(t, store.rows)
}

func TestBalanceRecordsEarlyBatchOutcomesAreRecorded(t *testing.T) {
	n, _, repo, store, _ := balanceRecordFixture(t)
	svc := &UserBulkEmailService{users: &bulkEmailUsersStub{users: map[int64]*User{}}, settings: repo, sender: n, records: n.BalanceNotificationRecords()}
	item := svc.sendUser(t.Context(), 999, NotificationEmailEventBalanceLow, "zh", nil, nil, "missing-user-batch")
	require.Equal(t, "user_not_found", item.Error)
	r := store.only(t)
	require.Equal(t, int64(999), r.UserID)
	require.Equal(t, "user_not_found", r.SuppressionReason)
	require.Empty(t, r.UserEmail)
	require.Empty(t, r.Recipient)
	store.rows = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	item = svc.sendUser(ctx, 7, NotificationEmailEventBalanceLow, "zh", nil, nil, "cancelled-batch")
	require.Equal(t, "request_cancelled", item.Error)
	require.Equal(t, "failed", store.only(t).Status)
	require.Zero(t, store.only(t).Attempts)
}

type balanceMarkerFailureRepo struct {
	*notificationEmailMemorySettingRepo
}

func (r balanceMarkerFailureRepo) Set(ctx context.Context, key, value string) error {
	if strings.HasPrefix(key, notificationEmailDeliveryKeyPrefix) {
		return errors.New("dedup marker write failed")
	}
	return r.notificationEmailMemorySettingRepo.Set(ctx, key, value)
}

func TestBalanceRecordsSMTPFailureAndMarkerFailurePreserveFacts(t *testing.T) {
	n, _, repo, store, smtp := balanceRecordFixture(t)
	input := NotificationEmailSendInput{Event: NotificationEmailEventBalanceLow, UserID: 7, RecipientEmail: "notify@example.test", SourceType: "balance_low", SourceID: "7", ReminderKey: "fixture", Variables: map[string]string{"current_balance": "0.10", "threshold": "1.00"}}
	input.BalanceDelivery = n.BalanceNotificationRecords().Begin(balancenotice.Input{UserID: 7, Recipient: input.RecipientEmail, TriggerType: "automatic"})
	n.settingRepo = balanceMarkerFailureRepo{repo}
	delivered, err := n.SendDelivered(t.Context(), input)
	require.True(t, delivered)
	require.ErrorContains(t, err, "dedup marker write failed")
	require.Equal(t, "sent", store.only(t).Status)
	require.Equal(t, int64(1), smtp.messageCount())
	store.rows = nil
	n.settingRepo = repo
	// The SMTP fixture deliberately disconnects after greeting when closed;
	// dialling its closed ephemeral port produces a real transport failure.
	smtp.close()
	input.BalanceDelivery = n.BalanceNotificationRecords().Begin(balancenotice.Input{UserID: 7, Recipient: input.RecipientEmail, TriggerType: "automatic"})
	delivered, err = n.SendDelivered(t.Context(), input)
	require.False(t, delivered)
	require.Error(t, err)
	r := store.only(t)
	require.Equal(t, "failed", r.Status)
	require.Equal(t, 1, r.Attempts)
	require.NotEmpty(t, r.LastError)
	require.Nil(t, r.SentAt)
}

func TestBalanceRecordStorageFailureStillSendsMail(t *testing.T) {
	_, svc, _, store, smtp := balanceRecordFixture(t)
	store.err = errors.New("audit storage offline")
	svc.sendBalanceLowEmails([]string{"user@example.test"}, 7, "用户", "user@example.test", 0.1, 1, "Test", "")
	require.Equal(t, int64(1), smtp.messageCount())
}
