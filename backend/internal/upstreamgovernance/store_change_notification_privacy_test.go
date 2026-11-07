package upstreamgovernance

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSQLStoreChangeNotificationPrivacyUsesGroupAttributesAndPrivateHistory(t *testing.T) {
	for _, tt := range []struct {
		name, payload string
		history, want bool
	}{
		{"public", `{"is_exclusive":false}`, false, true},
		{"exclusive", `{"is_exclusive":true}`, false, false},
		{"public after private", `{"is_exclusive":false}`, true, false},
		{"unknown group", "", false, false},
		{"invalid attribute", `{"is_exclusive":"false"}`, false, false},
		{"newapi without attribute", `{"id":"default"}`, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, mock := storeFixture(t)
			reader, ok := store.(ChangeNotificationPrivacyReader)
			require.True(t, ok, "SQL outbox must verify group visibility")
			now := time.Unix(100, 0).UTC()
			mock.ExpectQuery("WITH remote_facts AS").WithArgs(int64(5), "42", now).WillReturnRows(sqlmock.NewRows([]string{"payload", "private_history"}).AddRow(tt.payload, tt.history))
			if tt.want {
				mock.ExpectQuery("SELECT NOT EXISTS").WithArgs(int64(5), "42", int64(0), now).WillReturnRows(sqlmock.NewRows([]string{"public"}).AddRow(true))
				mock.ExpectQuery("SELECT DISTINCT target.value::bigint").WithArgs(int64(5), "42").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
			}
			public, err := reader.ChangeNotificationIsPublic(t.Context(), ChangeNotification{SiteID: 5, GroupResource: "42", Kind: "group_changes", CreatedAt: now})
			require.NoError(t, err)
			require.Equal(t, tt.want, public)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLStoreChangeNotificationPrivacyChecksEveryLocalTargetOfRemoteGroup(t *testing.T) {
	store, mock := storeFixture(t)
	reader, ok := store.(ChangeNotificationPrivacyReader)
	require.True(t, ok)
	now := time.Unix(100, 0).UTC()
	mock.ExpectQuery("WITH remote_facts AS").WithArgs(int64(5), "42", now).WillReturnRows(sqlmock.NewRows([]string{"payload", "private_history"}).AddRow(`{"is_exclusive":false}`, false))
	mock.ExpectQuery("SELECT NOT EXISTS").WithArgs(int64(5), "42", int64(0), now).WillReturnRows(sqlmock.NewRows([]string{"public"}).AddRow(true))
	mock.ExpectQuery("SELECT DISTINCT target.value::bigint").WithArgs(int64(5), "42").WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(7).AddRow(8))
	mock.ExpectQuery("SELECT \\(SELECT is_exclusive FROM groups").WithArgs(int64(7), now).WillReturnRows(sqlmock.NewRows([]string{"exclusive", "private_history"}).AddRow(false, false))
	mock.ExpectQuery("SELECT \\(SELECT is_exclusive FROM groups").WithArgs(int64(8), now).WillReturnRows(sqlmock.NewRows([]string{"exclusive", "private_history"}).AddRow(true, false))
	public, err := reader.ChangeNotificationIsPublic(t.Context(), ChangeNotification{SiteID: 5, GroupResource: "42", Kind: "group_changes", CreatedAt: now})
	require.NoError(t, err)
	require.False(t, public)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLStoreChangeNotificationPrivacyPricingChecksLocalGroupAndRemoteSources(t *testing.T) {
	for _, tt := range []struct {
		name                                      string
		exclusive, history, remoteExclusive, want bool
	}{
		{"public", false, false, false, true},
		{"exclusive local", true, false, false, false},
		{"formerly exclusive local", false, true, false, false},
		{"exclusive upstream source", false, false, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, mock := storeFixture(t)
			reader, ok := store.(ChangeNotificationPrivacyReader)
			require.True(t, ok)
			now := time.Unix(100, 0).UTC()
			mock.ExpectQuery("SELECT \\(SELECT is_exclusive FROM groups").WithArgs(int64(7), now).WillReturnRows(sqlmock.NewRows([]string{"exclusive", "private_history"}).AddRow(tt.exclusive, tt.history))
			if !tt.exclusive && !tt.history {
				mock.ExpectQuery("SELECT NOT EXISTS").WithArgs(int64(0), "", int64(7), now).WillReturnRows(sqlmock.NewRows([]string{"public"}).AddRow(true))
				mock.ExpectQuery("SELECT DISTINCT site_id,remote_group_id").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"site_id", "remote_group_id"}).AddRow(5, "42"))
				payload := `{"is_exclusive":false}`
				if tt.remoteExclusive {
					payload = `{"is_exclusive":true}`
				}
				mock.ExpectQuery("WITH remote_facts AS").WithArgs(int64(5), "42", now).WillReturnRows(sqlmock.NewRows([]string{"payload", "private_history"}).AddRow(payload, false))
				if !tt.remoteExclusive {
					mock.ExpectQuery("SELECT NOT EXISTS").WithArgs(int64(5), "42", int64(0), now).WillReturnRows(sqlmock.NewRows([]string{"public"}).AddRow(true))
				}
			}
			public, err := reader.ChangeNotificationIsPublic(t.Context(), ChangeNotification{SiteID: 5, LocalGroupID: 7, Kind: "pricing_change", CreatedAt: now})
			require.NoError(t, err)
			require.Equal(t, tt.want, public)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLStoreChangeNotificationPrivacyReadFailureDoesNotAuthorizeDelivery(t *testing.T) {
	store, mock := storeFixture(t)
	reader, ok := store.(ChangeNotificationPrivacyReader)
	require.True(t, ok)
	now := time.Unix(100, 0).UTC()
	mock.ExpectQuery("WITH remote_facts AS").WithArgs(int64(5), "42", now).WillReturnError(errors.New("read failed"))
	public, err := reader.ChangeNotificationIsPublic(t.Context(), ChangeNotification{SiteID: 5, GroupResource: "42", Kind: "group_changes", CreatedAt: now})
	require.Error(t, err)
	require.False(t, public)
}
