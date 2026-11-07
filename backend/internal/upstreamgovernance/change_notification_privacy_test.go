package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type privacyChangeNotificationStore struct {
	*changeNotificationMemoryStore
	public  bool
	err     error
	checked []ChangeNotification
}

func (s *privacyChangeNotificationStore) ChangeNotificationIsPublic(_ context.Context, item ChangeNotification) (bool, error) {
	s.checked = append(s.checked, item)
	return s.public, s.err
}

func publicChangeFixture() ChangeNotification {
	return ChangeNotification{SiteID: 5, GroupResource: "remote-42", DedupKey: "group:42:changed", Recipient: "admin@example.test", Kind: "group_changes", Subject: "group", Body: "changed"}
}

func TestChangeNotificationPrivacyBlocksExclusiveEnqueueForEveryRecipient(t *testing.T) {
	for _, recipient := range []string{"admin@example.test", "qq:123456"} {
		t.Run(recipient, func(t *testing.T) {
			store := &privacyChangeNotificationStore{changeNotificationMemoryStore: &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}}
			queue := NewChangeNotificationQueue(store, nil, nil)
			item := publicChangeFixture()
			item.Recipient = recipient
			require.NoError(t, queue.Enqueue(t.Context(), item))
			require.Empty(t, store.items, "an exclusive group must never create a delivery")
		})
	}
}

func TestChangeNotificationPrivacyRechecksQueuedMessagesAndConsumesPrivateHistory(t *testing.T) {
	store := &privacyChangeNotificationStore{changeNotificationMemoryStore: &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}, public: true}
	sender := &changeNotificationMemorySender{}
	now := time.Unix(100, 0).UTC()
	queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
	require.NoError(t, queue.Enqueue(t.Context(), publicChangeFixture()))
	store.public = false // Became exclusive after enqueue, including a later return to public.
	require.NoError(t, queue.Dispatch(t.Context(), 10))
	require.Empty(t, sender.sent)
	require.Equal(t, ChangeNotificationSent, store.items[1].Status)
	store.public = true
	now = now.Add(time.Hour)
	require.NoError(t, queue.Dispatch(t.Context(), 10))
	require.Empty(t, sender.sent, "suppressed history must not be sent after visibility changes")
}

func TestChangeNotificationPrivacyReadFailuresRetryWithoutDelivery(t *testing.T) {
	store := &privacyChangeNotificationStore{changeNotificationMemoryStore: &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}, public: true}
	sender := &changeNotificationMemorySender{}
	queue := NewChangeNotificationQueue(store, sender, nil)
	require.NoError(t, queue.Enqueue(t.Context(), publicChangeFixture()))
	store.err = errors.New("visibility read unavailable")
	require.NoError(t, queue.Dispatch(t.Context(), 10))
	require.Empty(t, sender.sent)
	require.Equal(t, ChangeNotificationPending, store.items[1].Status)
	require.Equal(t, 1, store.items[1].Attempts)
	require.Contains(t, store.items[1].LastError, "visibility read unavailable")
	require.Error(t, queue.Enqueue(t.Context(), publicChangeFixture()))
}

func TestChangeNotificationPrivacyLegacyGroupMessagesFailClosedWithoutAffectingSiteNotices(t *testing.T) {
	for _, kind := range []string{"group_changes", "group_change", "catalog_change", "rate_change", "pricing_change", "protection_change", "balance_low", "account_authorization"} {
		t.Run(kind, func(t *testing.T) {
			store := &privacyChangeNotificationStore{changeNotificationMemoryStore: &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}, public: true}
			sender := &changeNotificationMemorySender{}
			now := time.Unix(100, 0).UTC()
			item := publicChangeFixture()
			item.GroupResource, item.Kind, item.CreatedAt, item.NextAttemptAt = "", kind, now, now
			require.NoError(t, store.EnqueueChangeNotification(t.Context(), item))
			queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
			require.NoError(t, queue.Dispatch(t.Context(), 10))
			if kind == "balance_low" || kind == "account_authorization" {
				require.Len(t, sender.sent, 1)
			} else {
				require.Empty(t, sender.sent)
			}
			require.Empty(t, store.checked, "unlocated group messages and unrelated site notices need no group lookup")
		})
	}
}

func TestChangeNotificationPrivacyCarriesLocationsAndRecoversLegacyPricing(t *testing.T) {
	store := &privacyChangeNotificationStore{changeNotificationMemoryStore: &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}, public: true}
	sender := &changeNotificationMemorySender{}
	queue := NewChangeNotificationQueue(store, sender, nil)
	notice := renderPricingOperationNotice(Site{ID: 5}, PricingOperation{LocalGroupID: 7, Status: "applied", CostFactRevision: 2})
	require.NoError(t, queue.EnqueueNotice(t.Context(), notice, []string{"admin@example.test", "qq:123456"}))
	require.Len(t, store.items, 2)
	for _, item := range store.items {
		require.Equal(t, int64(7), item.LocalGroupID)
	}
	item := publicChangeFixture()
	item.GroupResource, item.LocalGroupID, item.Kind, item.DedupKey = "", 0, "pricing_change", "site:5:pricing:group:9:revision:4"
	require.NoError(t, store.EnqueueChangeNotification(t.Context(), item))
	require.NoError(t, queue.Dispatch(t.Context(), 10))
	require.Len(t, sender.sent, 3)
	var recovered bool
	for _, checked := range store.checked {
		recovered = recovered || checked.LocalGroupID == 9
	}
	require.True(t, recovered)
}
