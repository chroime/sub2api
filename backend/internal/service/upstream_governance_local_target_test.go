package service

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceLocalModelTargetRejectsInvalidKeyStateAndBinding(t *testing.T) {
	cases := []struct {
		name      string
		keyStatus string
		groupID   int64
		expiresAt any
		quota     float64
		used      float64
		want      error
	}{
		{name: "disabled", keyStatus: StatusAPIKeyDisabled, groupID: 7, want: gov.ErrConflict},
		{name: "expired", keyStatus: StatusAPIKeyActive, groupID: 7, expiresAt: time.Now().Add(-time.Minute), want: gov.ErrConflict},
		{name: "quota exhausted", keyStatus: StatusAPIKeyActive, groupID: 7, quota: 1, used: 1, want: gov.ErrConflict},
		{name: "wrong group", keyStatus: StatusAPIKeyActive, groupID: 8, want: gov.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery(regexp.QuoteMeta(localModelTargetQuery)).
				WithArgs(int64(11), int64(42), StatusActive).
				WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "key", "name", "group_id", "status", "expires_at", "quota", "quota_used", "updated_at", "group_id", "group_name", "platform", "group_status", "group_updated_at"}).
					AddRow(int64(11), int64(42), "local-secret", "test key", tc.groupID, tc.keyStatus, tc.expiresAt, tc.quota, tc.used, time.Now(), tc.groupID, "Local", "openai", StatusActive, time.Now()))
			local := &governanceLocalAccounts{db: db}
			_, gotErr := local.ResolveLocalModelTarget(t.Context(), 42, 7, 11)
			require.ErrorIs(t, gotErr, tc.want)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
