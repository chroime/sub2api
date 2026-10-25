package accountquality

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRecentSummariesReadsBoundedPreviewInChronologicalOrder(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Assert projection and ordering at the DB boundary. Full evidence must never
	// be fetched just to obtain a compact preview, and row ranking must not leave
	// PostgreSQL free to return the latest 30 samples in arbitrary order.
	mock.ExpectQuery(`SELECT
		(evidence || jsonb_build_object('result_preview', RIGHT(COALESCE(evidence->>'response_text',''),240)))
		-'prompt'-'expected'-'response_text'-'config'-'identity_version',execution_status,lease_token
		FROM (
			SELECT binding_id,evidence,execution_status,lease_token,
			ROW_NUMBER() OVER(PARTITION BY binding_id ORDER BY created_at DESC,id DESC) n
			FROM account_quality_runs WHERE binding_id=ANY($1::bigint[])
		) recent WHERE n<=30 ORDER BY binding_id,n DESC`).
		WithArgs("{7,9}").
		WillReturnRows(sqlmock.NewRows([]string{"evidence", "execution_status", "lease_token"}).
			AddRow(`{"id":"older","binding_id":7,"result_preview":"最后一行：21🙂","execution_status":"queued"}`, "completed", "").
			AddRow(`{"id":"newer","binding_id":7,"result_preview":"42"}`, "completed", "").
			AddRow(`{"id":"no-output","binding_id":9,"result_preview":""}`, "execution_error", ""))

	got, err := NewStore(db).RecentSummaries(context.Background(), []int64{7, 9})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, got[7], 2)
	require.Equal(t, "older", got[7][0].ID)
	require.Equal(t, "newer", got[7][1].ID)
	require.Equal(t, "completed", got[7][0].ExecutionStatus)
	require.Empty(t, got[7][0].Prompt)
	require.Empty(t, got[7][0].Expected)
	require.Empty(t, got[7][0].ResponseText)
	require.Empty(t, got[7][0].Config)
	require.Empty(t, got[7][0].IdentityVersion)

	encoded, err := json.Marshal(got[7][0])
	require.NoError(t, err)
	var summary map[string]any
	require.NoError(t, json.Unmarshal(encoded, &summary))
	require.Equal(t, "最后一行：21🙂", summary["result_preview"])
	encoded, err = json.Marshal(got[9][0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "result_preview", "missing output does not manufacture diagnostic text")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRecentSummariesWithoutBindingsDoesNotQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	got, err := NewStore(db).RecentSummaries(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
