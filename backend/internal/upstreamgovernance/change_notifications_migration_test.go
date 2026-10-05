package upstreamgovernance

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This opt-in test applies only migration 262 to a throwaway loopback schema.
// It complements sqlmock queue coverage with the real unique constraint,
// status checks and site foreign key used by durable delivery.
func TestChangeNotificationMigrationPostgres(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	if u.Hostname() != "127.0.0.1" || u.User == nil || u.User.Username() != "governance_fixture" {
		t.Fatal("requires isolated loopback governance_fixture database")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	schema := fmt.Sprintf("governance_change_notifications_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE upstream_governance_sites(id BIGSERIAL PRIMARY KEY)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "262_upstream_governance_change_notifications.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO upstream_governance_sites(id) VALUES (5)`)
	require.NoError(t, err)
	store := NewSQLStore(db).(ChangeNotificationStore)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.EnqueueChangeNotification(context.Background(), ChangeNotification{
		SiteID: 5, DedupKey: "migration:rate:1", Recipient: "admin@example.test", Kind: "rate_change", Severity: "warning", Subject: "rate", Body: "changed", CreatedAt: now, NextAttemptAt: now,
	}))
	require.NoError(t, store.EnqueueChangeNotification(context.Background(), ChangeNotification{
		SiteID: 5, DedupKey: "migration:rate:1", Recipient: "admin@example.test", Kind: "rate_change", Severity: "warning", Subject: "duplicate", Body: "duplicate", CreatedAt: now, NextAttemptAt: now,
	}))
	items, err := store.DueChangeNotifications(context.Background(), now.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "rate", items[0].Subject)
}
