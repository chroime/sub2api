package upstreamgovernance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalGroupNotificationPrivacyAfterBindingRemovalPostgres(t *testing.T) {
	for _, mutation := range []string{"delete binding", "replace targets", "delete site"} {
		t.Run(mutation, func(t *testing.T) {
			store, db := notificationPrivacyFixture(t)
			now := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			_, err := store.ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{
				GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "Private source", IsExclusive: true, ResolvedRateMultiplier: floatPtr(1)}},
			})
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,local_group_ids,marker)
VALUES(5,'42','openai',7,'[7]','private-source-binding')`)
			require.NoError(t, err)
			sender := &localGroupPolicySender{}
			queue := NewLocalGroupChangeQueue(store, sender, func() time.Time { return now })
			event := LocalGroupChangeEvent{GroupID: 7, Kind: LocalGroupChangeUpdated,
				Before: `{"name":"Public local","is_exclusive":false}`,
				After:  `{"name":"Local changed during private binding","is_exclusive":false}`,
			}
			require.NoError(t, queue.Enqueue(t.Context(), event))
			switch mutation {
			case "delete binding":
				_, err = db.Exec(`DELETE FROM upstream_governance_bindings WHERE marker='private-source-binding'`)
			case "replace targets":
				_, err = db.Exec(`INSERT INTO groups(id,name) VALUES(8,'another public');
UPDATE upstream_governance_bindings SET local_group_id=8,local_group_ids='[8]' WHERE marker='private-source-binding'`)
			case "delete site":
				_, err = db.Exec(`DELETE FROM upstream_governance_bindings WHERE site_id=5; DELETE FROM upstream_governance_sites WHERE id=5`)
			}
			require.NoError(t, err)
			now = time.Now().UTC().Add(time.Second)
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Zero(t, sender.delivered, "removing a private association must not make its queued local changes public")
			var pending int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM upstream_governance_local_group_changes WHERE status<>'sent'`).Scan(&pending))
			require.Zero(t, pending, "old private changes must be consumed permanently")
			event.After = `{"name":"Fresh public change after unlink","is_exclusive":false}`
			require.NoError(t, queue.Enqueue(t.Context(), event))
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Equal(t, 1, sender.delivered, "new changes after unlink must retain ordinary public delivery")
			require.Contains(t, sender.notices[0].Body, "Fresh public change after unlink")
		})
	}
}

func TestLocalGroupBindingBarrierPreservesPublicChangesPostgres(t *testing.T) {
	for _, wasExclusive := range []bool{false, true} {
		t.Run(map[bool]string{false: "always public", true: "public again before unlink"}[wasExclusive], func(t *testing.T) {
			store, db := notificationPrivacyFixture(t)
			start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			group := RemoteGroup{ID: "42", Name: "Source", IsExclusive: wasExclusive, ResolvedRateMultiplier: floatPtr(1)}
			_, err := store.ObserveFastResult(t.Context(), 5, start, start.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{group}})
			require.NoError(t, err)
			publicAt := start.Add(5 * time.Minute)
			group.IsExclusive = false
			_, err = store.ObserveFastResult(t.Context(), 5, publicAt, publicAt.Add(time.Minute), "healthy", "", GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{group}})
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,local_group_ids,marker)
VALUES(5,'42','openai',7,'[7]','source-binding')`)
			require.NoError(t, err)
			sender := &localGroupPolicySender{}
			now := time.Now().UTC().Add(time.Second)
			queue := NewLocalGroupChangeQueue(store, sender, func() time.Time { return now })
			for _, observedAt := range []time.Time{start.Add(time.Minute), publicAt.Add(time.Minute)} {
				require.NoError(t, queue.Enqueue(t.Context(), LocalGroupChangeEvent{
					GroupID: 7, Kind: LocalGroupChangeUpdated, CreatedAt: observedAt,
					Before: `{"name":"Public local","is_exclusive":false}`,
					After:  `{"name":"Changed local","is_exclusive":false}`,
				}))
			}
			_, err = db.Exec(`DELETE FROM upstream_governance_bindings WHERE marker='source-binding'`)
			require.NoError(t, err)
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			want := 2
			if wasExclusive {
				want = 1
			}
			require.Equal(t, want, sender.delivered, "unlink must preserve changes observed after the source became public")
		})
	}
}

func TestLocalGroupBindingBarrierIgnoresUnrelatedBindingUpdatesPostgres(t *testing.T) {
	store, db := notificationPrivacyFixture(t)
	now := time.Now().UTC()
	_, err := store.ObserveFastResult(t.Context(), 5, now, now.Add(time.Minute), "healthy", "", GroupObservation{
		GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "Private", IsExclusive: true, ResolvedRateMultiplier: floatPtr(1)}},
	})
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO upstream_governance_bindings(site_id,remote_group_id,platform,local_group_id,local_group_ids,marker)
VALUES(5,'42','openai',7,'[7]','source-binding');
UPDATE upstream_governance_bindings SET probe_enabled=true,marker='renamed-binding' WHERE marker='source-binding'`)
	require.NoError(t, err)
	var barriers int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM upstream_governance_notification_visibility_barriers`).Scan(&barriers))
	require.Zero(t, barriers, "routine binding writes must not create a permanent privacy cutoff")
}
