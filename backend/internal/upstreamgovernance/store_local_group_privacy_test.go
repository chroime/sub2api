package upstreamgovernance

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSQLStoreLocalGroupVisibilityUsesCurrentStateAndPrivateHistory(t *testing.T) {
	for _, tt := range []struct {
		name      string
		kind      LocalGroupChangeKind
		current   driver.Value
		private   bool
		wantAllow bool
	}{
		{"public unchanged", LocalGroupChangeCreated, false, false, true},
		{"currently exclusive", LocalGroupChangeCreated, true, false, false},
		{"public again after private period", LocalGroupChangeCreated, false, true, false},
		{"deleted since queued", LocalGroupChangeCreated, nil, false, false},
		{"public deletion", LocalGroupChangeDeleted, nil, false, true},
		{"public deletion after private period", LocalGroupChangeDeleted, nil, true, false},
		{"soft deleted exclusive", LocalGroupChangeDeleted, true, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, mock := storeFixture(t)
			event := publicLocalGroupPrivacyEvent()
			event.ID, event.GroupID, event.Kind = 17, 42, tt.kind
			if tt.kind == LocalGroupChangeDeleted {
				event.Before, event.After = event.After, ""
			}
			// The later-history predicate intentionally has no pending/sent
			// status filter: even an already consumed private audit is a barrier.
			mock.ExpectQuery(`SELECT \(SELECT is_exclusive FROM groups WHERE id=\$1 AND deleted_at IS NULL\), EXISTS \( SELECT 1 FROM upstream_governance_local_group_changes WHERE group_id=\$1 AND id>\$2 AND \(before_value->'is_exclusive' = 'true'::jsonb OR before_value->'IsExclusive' = 'true'::jsonb OR after_value->'is_exclusive' = 'true'::jsonb OR after_value->'IsExclusive' = 'true'::jsonb\) \)`).
				WithArgs(int64(42), int64(17)).
				WillReturnRows(sqlmock.NewRows([]string{"is_exclusive", "private_history"}).AddRow(tt.current, tt.private))
			if tt.wantAllow {
				mock.ExpectQuery(`SELECT NOT EXISTS`).WithArgs(int64(0), "", int64(42), event.CreatedAt).
					WillReturnRows(sqlmock.NewRows([]string{"public"}).AddRow(true))
				mock.ExpectQuery(`SELECT DISTINCT site_id,remote_group_id`).WithArgs(int64(42)).
					WillReturnRows(sqlmock.NewRows([]string{"site_id", "remote_group_id"}))
			}
			allowed, err := store.(*sqlStore).LocalGroupChangeIsPublic(t.Context(), event)
			require.NoError(t, err)
			require.Equal(t, tt.wantAllow, allowed)
		})
	}
}

func TestSQLStoreLocalGroupVisibilityFailsClosedOnLookupError(t *testing.T) {
	store, mock := storeFixture(t)
	event := publicLocalGroupPrivacyEvent()
	event.ID, event.GroupID = 17, 42
	mock.ExpectQuery(`SELECT \(SELECT is_exclusive FROM groups`).WithArgs(int64(42), int64(17)).WillReturnError(errors.New("database unavailable"))
	allowed, err := store.(*sqlStore).LocalGroupChangeIsPublic(t.Context(), event)
	require.False(t, allowed)
	require.ErrorContains(t, err, "database unavailable")
}

func TestSQLStoreLocalGroupVisibilityRejectsPrivateSnapshotsWithoutLookup(t *testing.T) {
	store, _ := storeFixture(t)
	allowed, err := store.(*sqlStore).LocalGroupChangeIsPublic(t.Context(), LocalGroupChangeEvent{
		ID: 17, GroupID: 42, Kind: LocalGroupChangeCreated,
		After: `{"name":"Private","status":"active","is_exclusive":true}`,
	})
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestSQLStoreLocalGroupVisibilityChecksExclusiveUpstreamSources(t *testing.T) {
	store, mock := storeFixture(t)
	event := publicLocalGroupPrivacyEvent()
	event.ID, event.GroupID = 17, 42
	mock.ExpectQuery(`SELECT \(SELECT is_exclusive FROM groups`).WithArgs(int64(42), int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{"is_exclusive", "private_history"}).AddRow(false, false))
	mock.ExpectQuery(`SELECT NOT EXISTS`).WithArgs(int64(0), "", int64(42), event.CreatedAt).
		WillReturnRows(sqlmock.NewRows([]string{"public"}).AddRow(true))
	mock.ExpectQuery(`SELECT DISTINCT site_id,remote_group_id`).WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"site_id", "remote_group_id"}).AddRow(5, "remote-private"))
	mock.ExpectQuery(`WITH remote_facts AS`).WithArgs(int64(5), "remote-private", event.CreatedAt).
		WillReturnRows(sqlmock.NewRows([]string{"payload", "private_history"}).AddRow(`{"is_exclusive":true}`, false))
	allowed, err := store.(*sqlStore).LocalGroupChangeIsPublic(t.Context(), event)
	require.NoError(t, err)
	require.False(t, allowed, "a public local target must not disclose changes from an exclusive upstream source")
}
