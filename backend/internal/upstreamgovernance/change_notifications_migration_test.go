package upstreamgovernance

import (
	"context"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This opt-in test applies the queue and group metadata migrations to a throwaway loopback schema.
// It complements sqlmock queue coverage with the real unique constraint,
// status checks and site foreign key used by durable delivery.
func TestChangeNotificationMigrationPostgres(t *testing.T) {
	store, _ := notificationPrivacyFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.EnqueueChangeNotification(context.Background(), ChangeNotification{
		SiteID: 5, GroupResource: "42", LocalGroupID: 7, DedupKey: "migration:rate:1", Recipient: "admin@example.test", Kind: "rate_change", Severity: "warning", Subject: "rate", Body: "changed", CreatedAt: now, NextAttemptAt: now,
	}))
	require.NoError(t, store.EnqueueChangeNotification(context.Background(), ChangeNotification{
		SiteID: 5, DedupKey: "migration:rate:1", Recipient: "admin@example.test", Kind: "rate_change", Severity: "warning", Subject: "duplicate", Body: "duplicate", CreatedAt: now, NextAttemptAt: now,
	}))
	items, err := store.DueChangeNotifications(context.Background(), now.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "rate", items[0].Subject)
	require.Equal(t, "42", items[0].GroupResource)
	require.Equal(t, int64(7), items[0].LocalGroupID)
}
