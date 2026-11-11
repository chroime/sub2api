package service

import (
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"

	"github.com/stretchr/testify/require"
)

func TestUserBulkEmailReusesAuthenticatedSMTPSession(t *testing.T) {
	n, _, repo, store, smtp := balanceRecordFixture(t)
	users := &bulkEmailUsersStub{users: map[int64]*User{}}
	for id := int64(1); id <= 3; id++ {
		users.users[id] = &User{ID: id, Email: fmt.Sprintf("campaign%d@example.test", id)}
	}
	svc := &UserBulkEmailService{users: users, settings: repo, sender: n, records: n.BalanceNotificationRecords()}
	links := bulkEmailTestLinks()
	result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1, 2, 3}, Event: NotificationEmailEventSystemHealthSnapshot, Locale: "zh", Links: &links})
	require.NoError(t, err)
	require.Equal(t, 3, result.Sent)
	require.Equal(t, int64(3), smtp.messageCount())
	require.Equal(t, int64(1), smtp.authentications.Load(), "one administrator batch must not reconnect and authenticate for every recipient")
	require.Len(t, store.rows, 3)
}

func TestBulkSMTPClosesAndReauthenticatesAfterTwentyMessages(t *testing.T) {
	n, _, _, store, smtp := balanceRecordFixture(t)
	transport, err := newBulkSMTPTransport(t.Context(), n.emailService)
	require.NoError(t, err)
	defer transport.Close()
	var waits []time.Duration
	transport.wait = func(ctx context.Context, delay time.Duration) error { waits = append(waits, delay); return ctx.Err() }
	for id := int64(1); id <= 25; id++ {
		to := fmt.Sprintf("twenty%d@example.test", id)
		delivery := n.BalanceNotificationRecords().Begin(balancenotice.Input{UserID: id, Recipient: to, EventType: balancenotice.EventSystemHealthSnapshot, TriggerType: "manual_batch"})
		require.NoError(t, transport.Send(t.Context(), delivery, to, "Actual status", "<p>Actual body</p>"))
	}
	require.Equal(t, int64(25), smtp.messageCount())
	require.Equal(t, int64(2), smtp.authentications.Load())
	require.Equal(t, int64(2), smtp.connections.Load())
	require.Len(t, store.rows, 25)
	require.Len(t, waits, 25)
	require.Greater(t, waits[1], 900*time.Millisecond)
}

func TestBulkSMTPDoesNotRetryDataAcknowledgementOrRetryPermanentSMTP(t *testing.T) {
	for _, kind := range []string{"ack", "permanent", "data_rejected_451", "data_rejected_554"} {
		t.Run(kind, func(t *testing.T) {
			var dataCalls atomic.Int64
			var rcptCalls atomic.Int64
			hooks := notificationEmailSMTPHooks{
				beforeCommand: func(cmd string) (string, bool, bool) {
					if strings.HasPrefix(cmd, "RCPT TO:") {
						rcptCalls.Add(1)
						if kind == "permanent" {
							return "550 5.1.1 rejected", false, true
						}
					}
					if strings.HasPrefix(cmd, "DATA") {
						dataCalls.Add(1)
					}
					return "", false, false
				},
				afterData: func() (string, bool) {
					if kind == "ack" {
						return "", true
					}
					if kind == "data_rejected_451" {
						return "451 4.7.1 deferred", false
					}
					if kind == "data_rejected_554" {
						return "554 5.7.1 rejected", false
					}
					return "250 2.0.0 OK", false
				},
			}
			n, _, repo, store, _ := balanceRecordFixtureWithSMTPHooks(t, hooks)
			users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "edge@example.test"}}}
			svc := &UserBulkEmailService{users: users, settings: repo, sender: n, records: n.BalanceNotificationRecords()}
			links := bulkEmailTestLinks()
			result, err := svc.Send(t.Context(), UserBulkEmailInput{UserIDs: []int64{1}, Event: NotificationEmailEventSystemHealthSnapshot, Locale: "zh", Links: &links})
			require.NoError(t, err)
			require.Equal(t, 1, result.Failed)
			require.Len(t, store.rows, 1)
			row := store.only(t)
			require.Equal(t, "failed", row.Status)
			if kind == "ack" {
				require.Equal(t, "email_delivery_unconfirmed", result.Items[0].Error)
				require.Equal(t, 1, row.Attempts)
				require.Equal(t, int64(1), dataCalls.Load(), "DATA body must not be resent after ACK loss")
			} else if kind == "permanent" {
				require.Equal(t, "email_delivery_failed", result.Items[0].Error)
				require.Equal(t, int64(1), rcptCalls.Load(), "permanent 550 must not retry")
				require.Zero(t, dataCalls.Load())
			} else {
				require.Equal(t, "email_delivery_failed", result.Items[0].Error)
				require.Equal(t, int64(1), dataCalls.Load(), "even explicit rejection after DATA must not automatically resend")
				require.Equal(t, 1, row.Attempts)
			}
		})
	}
}

func TestBulkSMTPRetriesOnlyPreDataTransientRejectionOrClosedConnection(t *testing.T) {
	for _, kind := range []string{"451", "closed", "always451"} {
		t.Run(kind, func(t *testing.T) {
			var rcpts atomic.Int64
			hooks := notificationEmailSMTPHooks{beforeCommand: func(cmd string) (string, bool, bool) {
				if strings.HasPrefix(cmd, "RCPT TO:") {
					call := rcpts.Add(1)
					if call == 1 || kind == "always451" {
						if kind == "closed" {
							return "", true, true
						}
						return "451 4.7.1 slow down", false, true
					}
				}
				return "", false, false
			}}
			n, _, _, store, smtp := balanceRecordFixtureWithSMTPHooks(t, hooks)
			transport, err := newBulkSMTPTransport(t.Context(), n.emailService)
			require.NoError(t, err)
			defer transport.Close()
			var waits []time.Duration
			transport.wait = func(ctx context.Context, delay time.Duration) error { waits = append(waits, delay); return ctx.Err() }
			delivery := n.BalanceNotificationRecords().Begin(balancenotice.Input{UserID: 7, Recipient: "retry@example.test", EventType: balancenotice.EventSystemHealthSnapshot, TriggerType: "manual_batch"})
			err = transport.Send(t.Context(), delivery, "retry@example.test", "Actual status", "<p>Actual body</p>")
			if kind == "always451" {
				require.Error(t, err)
				require.Equal(t, "failed", store.only(t).Status)
				require.Zero(t, smtp.messageCount())
			} else {
				require.NoError(t, err)
				require.Equal(t, "sent", store.only(t).Status)
				require.Equal(t, int64(1), smtp.messageCount())
			}
			require.Equal(t, 2, store.only(t).Attempts)
			require.Equal(t, int64(2), rcpts.Load())
			require.Contains(t, waits, bulkSMTPRetryBackoff)
		})
	}
}

func TestBulkSMTPGateHonorsContextAndDoesNotDelayOrdinaryMail(t *testing.T) {
	n, _, _, _, smtp := balanceRecordFixture(t)
	transport, err := newBulkSMTPTransport(t.Context(), n.emailService)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	other, err := newBulkSMTPTransport(ctx, n.emailService)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, other)
	require.NoError(t, n.emailService.SendEmail(t.Context(), "automatic@example.test", "Ordinary notice", "<p>Ordinary</p>"), "campaign gate must not block authentication/automatic email")
	require.Equal(t, int64(1), smtp.messageCount())
	acquired := make(chan *bulkSMTPTransport, 1)
	go func() {
		next, _ := newBulkSMTPTransport(t.Context(), n.emailService)
		acquired <- next
	}()
	select {
	case <-acquired:
		t.Fatal("a second batch acquired the active batch's transport gate")
	case <-time.After(20 * time.Millisecond):
	}
	transport.Close()
	select {
	case next := <-acquired:
		require.NotNil(t, next)
		next.Close()
	case <-time.After(time.Second):
		t.Fatal("the next batch did not acquire the released transport gate")
	}
}

func TestBulkSMTPRefreshesSocketDeadlineBeforeReusingSession(t *testing.T) {
	n, _, _, _, smtp := balanceRecordFixture(t)
	transport, err := newBulkSMTPTransport(t.Context(), n.emailService)
	require.NoError(t, err)
	defer transport.Close()
	transport.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	require.NoError(t, transport.Send(t.Context(), nil, "first@example.test", "First", "<p>One</p>"))
	require.NoError(t, transport.conn.SetDeadline(time.Now().Add(-time.Second)))
	require.NoError(t, transport.Send(t.Context(), nil, "second@example.test", "Second", "<p>Two</p>"))
	require.Equal(t, int64(2), smtp.messageCount())
	require.Equal(t, int64(1), smtp.authentications.Load(), "expired per-message deadline must be renewed rather than causing a reconnect")
}

func TestBulkSMTPConfigFailureDoesNotCountAttempt(t *testing.T) {
	n, _, repo, store, _ := balanceRecordFixture(t)
	require.NoError(t, repo.Set(t.Context(), SettingKeySMTPHost, ""))
	transport, err := newBulkSMTPTransport(t.Context(), n.emailService)
	require.NoError(t, err)
	defer transport.Close()
	delivery := n.BalanceNotificationRecords().Begin(balancenotice.Input{UserID: 7, Recipient: "not-configured@example.test", TriggerType: "manual_batch"})
	require.Error(t, transport.Send(t.Context(), delivery, "not-configured@example.test", "No config", "<p>Body</p>"))
	require.Zero(t, store.only(t).Attempts)
	require.Equal(t, "failed", store.only(t).Status)
}

func TestBulkSMTPRetryClassifierKeepsPermanentAndTimeoutFinal(t *testing.T) {
	require.True(t, retryableBulkSMTPError(fmt.Errorf("wrapped: %w", &textproto.Error{Code: 451, Msg: "temporarily refused"})))
	require.False(t, retryableBulkSMTPError(fmt.Errorf("wrapped: %w", &textproto.Error{Code: 550, Msg: "permanently refused"})))
	require.False(t, retryableBulkSMTPError(context.DeadlineExceeded))
	require.True(t, retryableBulkSMTPError(fmt.Errorf("wrapped: %w", net.ErrClosed)))
}
