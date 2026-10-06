package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type changeNotificationMemoryStore struct {
	items  map[int64]ChangeNotification
	nextID int64
}

func (m *changeNotificationMemoryStore) EnqueueChangeNotification(_ context.Context, item ChangeNotification) error {
	for _, existing := range m.items {
		if existing.DedupKey == item.DedupKey && existing.Recipient == item.Recipient {
			return nil
		}
	}
	m.nextID++
	item.ID = m.nextID
	item.Status = ChangeNotificationPending
	m.items[item.ID] = item
	return nil
}
func (m *changeNotificationMemoryStore) DueChangeNotifications(_ context.Context, now time.Time, limit int) ([]ChangeNotification, error) {
	result := []ChangeNotification{}
	for _, item := range m.items {
		if item.Status == ChangeNotificationPending && !item.NextAttemptAt.After(now) {
			result = append(result, item)
		}
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}
func (m *changeNotificationMemoryStore) ClaimChangeNotification(_ context.Context, id int64, now, next time.Time) (bool, error) {
	item, ok := m.items[id]
	if !ok || item.Status != ChangeNotificationPending || item.NextAttemptAt.After(now) {
		return false, nil
	}
	item.Status = ChangeNotificationSending
	item.NextAttemptAt = next
	m.items[id] = item
	return true, nil
}
func (m *changeNotificationMemoryStore) CompleteChangeNotification(_ context.Context, id int64, sentAt time.Time, sendErr error, next time.Time) error {
	item := m.items[id]
	if sendErr == nil {
		item.Status = ChangeNotificationSent
		item.SentAt = &sentAt
	} else {
		item.Status = ChangeNotificationPending
		item.LastError = sendErr.Error()
		item.Attempts++
		item.NextAttemptAt = next
	}
	m.items[id] = item
	return nil
}

type changeNotificationMemorySender struct {
	fail bool
	sent []ChangeNotification
}

func (s *changeNotificationMemorySender) Send(_ context.Context, item ChangeNotification) error {
	s.sent = append(s.sent, item)
	if s.fail {
		return errors.New("smtp unavailable")
	}
	return nil
}

func TestChangeNotificationQueueSuppressesBaselineAndDeduplicates(t *testing.T) {
	store := &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}
	queue := NewChangeNotificationQueue(store, &changeNotificationMemorySender{}, func() time.Time { return time.Unix(100, 0).UTC() })
	baseline := ChangeNotification{SiteID: 5, DedupKey: "baseline:5", Recipient: "admin@example.test", InitialBaseline: true, Kind: "group_change", Subject: "baseline", Body: "baseline"}
	require.NoError(t, queue.Enqueue(context.Background(), baseline))
	require.NoError(t, queue.Enqueue(context.Background(), ChangeNotification{SiteID: 5, DedupKey: "rate:5:group:1:v2", Recipient: "admin@example.test", Kind: "rate_change", Subject: "rate", Body: "rate"}))
	require.NoError(t, queue.Enqueue(context.Background(), ChangeNotification{SiteID: 5, DedupKey: "rate:5:group:1:v2", Recipient: "admin@example.test", Kind: "rate_change", Subject: "rate duplicate", Body: "rate duplicate"}))
	require.Len(t, store.items, 1)
}

func TestChangeNotificationQueueRetriesFailedDeliveryAndSurvivesRestart(t *testing.T) {
	store := &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}
	sender := &changeNotificationMemorySender{fail: true}
	now := time.Unix(100, 0).UTC()
	queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
	require.NoError(t, queue.Enqueue(context.Background(), ChangeNotification{SiteID: 5, DedupKey: "rate:v2", Recipient: "admin@example.test", Kind: "rate_change", Subject: "rate", Body: "rate"}))
	require.NoError(t, queue.Dispatch(context.Background(), 10))
	require.Equal(t, ChangeNotificationPending, store.items[1].Status)
	require.NotEmpty(t, store.items[1].LastError)

	sender.fail = false
	now = now.Add(16 * time.Minute)
	restarted := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
	require.NoError(t, restarted.Dispatch(context.Background(), 10))
	require.Equal(t, ChangeNotificationSent, store.items[1].Status)
	require.Len(t, sender.sent, 2)
}
