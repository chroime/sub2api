package balancenotice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	mu   sync.Mutex
	rows map[string]Record
	err  error
}

func (m *memoryStore) Save(_ context.Context, r *Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	if m.rows == nil {
		m.rows = map[string]Record{}
	}
	m.rows[r.NotificationID+"/"+r.Recipient] = *r
	return nil
}
func (m *memoryStore) List(context.Context, Query, time.Time) (*Page, error) {
	return &Page{Items: []Record{}}, m.err
}
func (m *memoryStore) Get(context.Context, int64, time.Time) (*Record, error) {
	return nil, ErrNotFound
}
func (m *memoryStore) Prune(context.Context, time.Time, int) (int64, error) { return 0, m.err }
func (m *memoryStore) only(t *testing.T) Record {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.rows, 1)
	for _, r := range m.rows {
		return r
	}
	panic("no record")
}

func TestDeliveryTracksActualContentAndSMTPAcceptance(t *testing.T) {
	store := &memoryStore{}
	svc := NewService(store)
	d := svc.Begin(Input{UserID: 7, UserEmail: "user@example.test", UserName: "测试用户", Recipient: "notify@example.test", Balance: amount(0.065), Threshold: amount(1), TriggerType: "automatic"})
	require.Equal(t, "pending", store.only(t).Status)
	d.Content("余额提醒", "<p>余额0.065</p>")
	d.Attempt()
	d.Sent()
	r := store.only(t)
	require.Equal(t, "sent", r.Status)
	require.Equal(t, "<p>余额0.065</p>", r.Body)
	require.Equal(t, "user@example.test", r.UserEmail)
	require.Equal(t, "notify@example.test", r.Recipient)
	require.Equal(t, 0.065, *r.Balance)
	require.Equal(t, 1, r.Attempts)
	require.NotNil(t, r.SentAt)
	require.Equal(t, Retention, r.ExpiresAt.Sub(r.CreatedAt))
	d.Fail(errors.New("legacy dedup marker could not be persisted"))
	require.Equal(t, "sent", store.only(t).Status, "SMTP acceptance remains authoritative")
}

func TestDeliveryFallbackReusesRecordAndCountsOnlySMTP(t *testing.T) {
	store := &memoryStore{}
	d := NewService(store).Begin(Input{UserID: 9, Recipient: "u@example.test", TriggerType: "automatic"})
	d.Fail(errors.New("template failed to render"))
	require.Zero(t, store.only(t).Attempts)
	d.Content("内置余额提醒", "<p>内置正文</p>")
	d.Attempt()
	d.Sent()
	r := store.only(t)
	require.Equal(t, "sent", r.Status)
	require.Empty(t, r.LastError)
	require.Equal(t, 1, r.Attempts)
	require.Equal(t, "<p>内置正文</p>", r.Body)
}

func TestSuppressedDeliveryIsSeparateFromSentWithSameDailyKey(t *testing.T) {
	store := &memoryStore{}
	s := NewService(store)
	in := Input{UserID: 7, Recipient: "u@example.test", TriggerType: "automatic", DeliveryKey: "daily-hash"}
	first := s.Begin(in)
	first.Attempt()
	first.Sent()
	second := s.Begin(in)
	second.Suppress("duplicate")
	require.Len(t, store.rows, 2)
	statuses := map[string]int{}
	for _, r := range store.rows {
		statuses[r.Status]++
		if r.Status == "suppressed" {
			require.Zero(t, r.Attempts)
			require.Empty(t, r.Body)
		}
	}
	require.Equal(t, map[string]int{"sent": 1, "suppressed": 1}, statuses)
}

func TestAuditStoreFailureAndNilTrackerDoNotPanic(t *testing.T) {
	s := NewService(&memoryStore{err: errors.New("database offline")})
	d := s.Begin(Input{UserID: 7, Recipient: "u@example.test", TriggerType: "automatic"})
	require.NotPanics(t, func() { d.Content("notice", "<p>body</p>"); d.Attempt(); d.Sent() })
	var noDelivery *Delivery
	require.NotPanics(t, func() {
		noDelivery.Content("", "")
		noDelivery.Attempt()
		noDelivery.Fail(errors.New("smtp"))
		noDelivery.Suppress("duplicate")
		noDelivery.Sent()
	})
}

func TestQueryRejectsInvalidFiltersAndKeepsPaginationBounded(t *testing.T) {
	now := time.Now().UTC()
	for _, q := range []Query{{Status: "invented"}, {TriggerType: "astrbot"}, {Page: -1}, {PageSize: 101}, {UserID: -2}, {From: now, To: now.Add(-time.Hour)}} {
		_, err := q.Normalize(now)
		require.ErrorIs(t, err, ErrInvalid)
	}
	q, err := (Query{}).Normalize(now)
	require.NoError(t, err)
	require.Equal(t, 1, q.Page)
	require.Equal(t, 20, q.PageSize)
	require.Equal(t, now.Add(-Retention), q.From)
}

func amount(v float64) *float64 { return &v }
