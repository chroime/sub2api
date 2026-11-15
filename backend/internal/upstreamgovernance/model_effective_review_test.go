package upstreamgovernance

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestModelEffectiveReviewFilterBeforePagination(t *testing.T) {
	for _, review := range []string{"pass", "pending", "fail"} {
		t.Run(review, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery("SELECT status,count").WithArgs(int64(7), "").WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).AddRow("succeeded", 4))
			rows := sqlmock.NewRows(strings.Split(modelRunColumns, ","))
			for i, item := range []struct{ id, review, answer string }{
				{"auto-one", "pending", "答案是21颗。"},
				{"pending", "pending", "无法确定"},
				{"auto-two", "fail", "因此，最少需要取出 **21颗**。"},
				{"failed", "fail", "无法确定"},
			} {
				rows.AddRow(item.id, "b", 7, nil, i, `{"template":"candy"}`, "succeeded", `{"success":true,"candy_verdict":"needs_review","candy_rejudge_text":"`+item.answer+`"}`, item.review, "original", time.Now(), nil, nil, nil, nil, 1)
			}
			mock.ExpectQuery("SELECT .* FROM upstream_governance_model_runs WHERE").WillReturnRows(rows)
			p, err := (&modelStore{db: db}).listRuns(t.Context(), 7, 2, 1, "", ModelRunFilters{Review: review})
			require.NoError(t, err)
			require.EqualValues(t, 4, p.ScopeTotal)
			if review == "pass" {
				require.EqualValues(t, 2, p.Total)
				require.Len(t, p.Items, 1)
				require.Equal(t, "auto-two", p.Items[0].ID)
				require.Equal(t, "fail", p.Items[0].Review, "manual audit is preserved")
			} else {
				require.EqualValues(t, 1, p.Total)
				require.Empty(t, p.Items)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
