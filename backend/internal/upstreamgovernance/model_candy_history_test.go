package upstreamgovernance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestModelCandyHistoryProjection(t *testing.T) {
	for _, field := range []string{"response_text", "candy_rejudge_text"} {
		for _, review := range []string{"pending", "pass", "fail"} {
			t.Run(field+"/"+review, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer db.Close()
				raw, _ := json.Marshal(map[string]any{"success": true, "completed": true, "candy_verdict": "needs_review", field: "因此，最少需要取出 **21颗**。"})
				mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(strings.Split(modelRunColumns, ",")).AddRow("r", "b", 1, nil, 1, `{"template":"candy"}`, "succeeded", raw, review, "original review", time.Now(), nil, nil, nil, nil, 2))
				run, err := scanModelRun(db.QueryRow("SELECT"))
				require.NoError(t, err)
				require.Equal(t, "numeric_correct", run.Result.CandyVerdict)
				require.Equal(t, "trusted", run.Result.Security.TrustState)
				require.Equal(t, review, run.Review)
				require.Equal(t, "original review", run.ReviewNote)
				encoded, err := json.Marshal(run)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "candy_rejudge_text")
				if field == "candy_rejudge_text" {
					require.Empty(t, run.Result.ResponseText)
				} else {
					require.Equal(t, "因此，最少需要取出 **21颗**。", run.Result.ResponseText)
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestModelCandyHistoryDoesNotScoreFailedResponses(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(strings.Split(modelRunColumns, ",")).AddRow("r", "b", 1, nil, 1, `{"template":"candy"}`, "failed", `{"success":false,"candy_verdict":"needs_review","response_text":"答案：21颗。"}`, "pending", "", time.Now(), nil, nil, nil, nil, 0))
	run, err := scanModelRun(db.QueryRow("SELECT"))
	require.NoError(t, err)
	require.Equal(t, "needs_review", run.Result.CandyVerdict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestModelCandyHistoryKeepsOversizedEvidenceConsistentWithSummary(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	text := strings.Repeat("x", 131073) + "\n因此，最少需要取出21颗。"
	raw, err := json.Marshal(map[string]any{"success": true, "completed": true, "candy_verdict": "needs_review", "response_text": text})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(strings.Split(modelRunColumns, ",")).AddRow("r", "b", 1, nil, 1, `{"template":"candy"}`, "succeeded", raw, "pending", "", time.Now(), nil, nil, nil, nil, 0))
	run, err := scanModelRun(db.QueryRow("SELECT"))
	require.NoError(t, err)
	require.Equal(t, "needs_review", run.Result.CandyVerdict)
	require.Equal(t, text, run.Result.ResponseText)
	require.NoError(t, mock.ExpectationsWereMet())
}
