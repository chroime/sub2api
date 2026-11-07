package upstreamgovernance

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func notificationPrivacyFixture(t *testing.T) (*sqlStore, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("notification_privacy_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY);
CREATE TABLE groups(id BIGSERIAL PRIMARY KEY,name TEXT NOT NULL DEFAULT '',is_exclusive BOOLEAN NOT NULL DEFAULT false,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	for _, name := range []string{"247_upstream_governance.sql", "252_upstream_governance_multiple_target_groups.sql", "261_upstream_governance_seconds_observation.sql", "262_upstream_governance_change_notifications.sql", "273_upstream_governance_local_group_changes.sql", "277_upstream_governance_notification_group_privacy.sql"} {
		raw, readErr := os.ReadFile("../../migrations/" + name)
		require.NoError(t, readErr)
		_, err = db.Exec(string(raw))
		require.NoError(t, err, name)
	}
	_, err = db.Exec(`INSERT INTO upstream_governance_sites(id,name,platform,base_url) VALUES(5,'fixture','sub2api','https://fixture.example'); INSERT INTO groups(id,name) VALUES(7,'local public')`)
	require.NoError(t, err)
	return NewSQLStore(db).(*sqlStore), db
}

func TestChangeNotificationPrivacyPostgres(t *testing.T) {
	t.Run("fast transitions and restart suppress old mail and QQ while public notices continue", func(t *testing.T) {
		store, db := notificationPrivacyFixture(t)
		now := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
		public := RemoteGroup{ID: "42", Name: "public", ResolvedRateMultiplier: floatPtr(1)}
		private := RemoteGroup{ID: "99", Name: "exclusive", IsExclusive: true, ResolvedRateMultiplier: floatPtr(1)}
		observe := func(groups []RemoteGroup) {
			_, err := store.ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: groups})
			require.NoError(t, err)
		}
		observe([]RemoteGroup{public, private})
		// Incomplete old snapshots must not break the current public lookup.
		_, err := db.Exec(`INSERT INTO upstream_governance_snapshots(site_id,site_version,catalog) VALUES(5,1,'{"groups":null}')`)
		require.NoError(t, err)
		sender := &changeNotificationMemorySender{}
		queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
		for _, recipient := range []string{"admin@example.test", "qq:123456"} {
			item := publicChangeFixture()
			item.GroupResource, item.Recipient, item.CreatedAt = "42", recipient, now
			require.NoError(t, queue.Enqueue(t.Context(), item))
			item.GroupResource, item.DedupKey = "99", "private"
			require.NoError(t, queue.Enqueue(t.Context(), item))
		}
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM upstream_governance_change_notifications`).Scan(&count))
		require.Equal(t, 2, count)
		now = now.Add(time.Minute)
		public.IsExclusive = true
		observe([]RemoteGroup{public, private})
		now = now.Add(time.Minute)
		public.IsExclusive = false
		observe([]RemoteGroup{public, private})
		_, err = db.Exec(`INSERT INTO upstream_governance_events(site_id,kind) SELECT 5,'unrelated' FROM generate_series(1,1100)`)
		require.NoError(t, err)
		_, err = db.Exec(pruneEvents, int64(5))
		require.NoError(t, err)
		restarted := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
		require.NoError(t, restarted.Dispatch(t.Context(), 20))
		require.Empty(t, sender.sent)
		now = now.Add(time.Minute)
		item := publicChangeFixture()
		item.GroupResource, item.DedupKey = "42", "new public change"
		require.NoError(t, restarted.Enqueue(t.Context(), item))
		item.GroupResource, item.Kind, item.DedupKey = "", "balance_low", "site balance"
		require.NoError(t, restarted.Enqueue(t.Context(), item))
		require.NoError(t, restarted.Dispatch(t.Context(), 20))
		require.Len(t, sender.sent, 2)
	})

	t.Run("deleted exclusive source stays private for later local notices", func(t *testing.T) {
		store, db := notificationPrivacyFixture(t)
		now := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
		group := RemoteGroup{ID: "42", Name: "exclusive", IsExclusive: true, ResolvedRateMultiplier: floatPtr(1)}
		_, err := store.ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{group}})
		require.NoError(t, err)
		now = now.Add(time.Minute)
		_, err = store.ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{}})
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,local_group_ids,marker) VALUES(5,'42','openai',7,'[7]','fixture-binding')`)
		require.NoError(t, err)
		allowed, err := store.ChangeNotificationIsPublic(t.Context(), ChangeNotification{SiteID: 5, LocalGroupID: 7, Kind: "pricing_change", CreatedAt: now.Add(time.Minute)})
		require.NoError(t, err)
		require.False(t, allowed)
		allowed, err = store.changeNotificationLocalGroupRemoteSourcesArePublic(t.Context(), 7, now.Add(time.Minute))
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("local exclusive history blocks queued pricing after return to public", func(t *testing.T) {
		store, db := notificationPrivacyFixture(t)
		now := time.Now().UTC().Add(-time.Minute)
		sender := &changeNotificationMemorySender{}
		queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
		notice := renderPricingOperationNotice(Site{ID: 5}, PricingOperation{LocalGroupID: 7, Status: "applied", CostFactRevision: 1})
		notice.ObservedAt = now
		require.NoError(t, queue.EnqueueNotice(t.Context(), notice, []string{"admin@example.test"}))
		_, err := db.Exec(`UPDATE groups SET is_exclusive=true WHERE id=7; UPDATE groups SET is_exclusive=false WHERE id=7`)
		require.NoError(t, err)
		now = time.Now().UTC().Add(time.Second)
		require.NoError(t, queue.Dispatch(t.Context(), 20))
		require.Empty(t, sender.sent)
		notice.DedupKey, notice.ObservedAt = "fresh public pricing", now
		require.NoError(t, queue.EnqueueNotice(t.Context(), notice, []string{"admin@example.test"}))
		require.NoError(t, queue.Dispatch(t.Context(), 20))
		require.Len(t, sender.sent, 1)
	})

	t.Run("public offline notices survive full snapshot rotation", func(t *testing.T) {
		store, _ := notificationPrivacyFixture(t)
		before := Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "public", ResolvedRateMultiplier: floatPtr(1)}}}
		after := Catalog{GroupsComplete: true, Groups: []RemoteGroup{}}
		require.NoError(t, store.SaveSnapshot(t.Context(), &Snapshot{SiteID: 5, SiteVersion: 1, Catalog: before}, nil))
		require.NoError(t, store.SaveSnapshot(t.Context(), &Snapshot{SiteID: 5, SiteVersion: 1, Catalog: after}, DiffCatalog(5, before, after)))
		now := time.Now().UTC()
		sender := &changeNotificationMemorySender{}
		queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
		item := publicChangeFixture()
		item.GroupResource = "42"
		require.NoError(t, queue.Enqueue(t.Context(), item))
		for i := 0; i < 21; i++ {
			require.NoError(t, store.SaveSnapshot(t.Context(), &Snapshot{SiteID: 5, SiteVersion: 1, Catalog: after}, nil))
		}
		require.NoError(t, queue.Dispatch(t.Context(), 20))
		require.Len(t, sender.sent, 1)
	})
}

func TestChangeNotificationBindingPrivacyPostgres(t *testing.T) {
	for _, scope := range []string{"local pricing", "remote group", "public binding"} {
		t.Run(scope, func(t *testing.T) {
			store, db := notificationPrivacyFixture(t)
			now := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			group := RemoteGroup{ID: "42", Name: "public", ResolvedRateMultiplier: floatPtr(1)}
			observe := func() {
				_, err := store.ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{group}})
				require.NoError(t, err)
			}
			observe()
			_, err := db.Exec(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,local_group_ids,marker) VALUES(5,'42','openai',7,'[7]','fixture-binding')`)
			require.NoError(t, err)
			sender := &changeNotificationMemorySender{}
			queue := NewChangeNotificationQueue(store, sender, func() time.Time { return now })
			item := publicChangeFixture()
			item.GroupResource = "42"
			if scope == "local pricing" {
				item.GroupResource, item.LocalGroupID, item.Kind = "", 7, "pricing_change"
			}
			require.NoError(t, queue.Enqueue(t.Context(), item))
			if scope == "local pricing" {
				now = now.Add(time.Minute)
				group.IsExclusive = true
				observe()
			} else if scope == "remote group" {
				_, err = db.Exec(`UPDATE groups SET is_exclusive=true WHERE id=7`)
				require.NoError(t, err)
			}
			_, err = db.Exec(`DELETE FROM upstream_governance_bindings WHERE site_id=5`)
			require.NoError(t, err)
			now = time.Now().UTC().Add(time.Second)
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			if scope == "public binding" {
				require.Len(t, sender.sent, 1)
			} else {
				require.Empty(t, sender.sent)
			}
			item.DedupKey = "fresh after unlink"
			require.NoError(t, queue.Enqueue(t.Context(), item))
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			if scope == "public binding" {
				require.Len(t, sender.sent, 2)
			} else {
				require.Len(t, sender.sent, 1)
			}
		})
	}
}
