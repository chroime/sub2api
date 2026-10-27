//go:build integration

package accountquality

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecentSummariesPostgresPreservesEvidenceAndUnicodeSuffix(t *testing.T) {
	f := newQualityPostgresFixture(t)
	base := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	const bindingID int64 = 11
	texts := make(map[string]string)
	originals := make(map[string]string)
	for i := 1; i <= 35; i++ {
		id := fmt.Sprintf("%08x-0000-4000-8000-000000000000", i)
		text := strings.Repeat("鹈鹕🙂", 100) + fmt.Sprintf("\n最后一行：%d", i)
		raw := encode(map[string]any{
			"id": id, "binding_id": bindingID, "account_id": 7,
			"prompt": "private diagnostic prompt", "expected": "private expected result",
			"response_text": text, "config": map[string]any{"name": "private snapshot"},
			"identity_version": "private identity", "result_preview": "ignore obsolete stored preview",
		})
		// Tied timestamps verify that both selection and chronological return order
		// use the immutable ID as their deterministic secondary key.
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO account_quality_runs
			(id,request_id,account_id,binding_id,policy_id,execution_status,evidence,created_at)
			VALUES($1,$1,7,$2,1,'completed',$3,$4)`, id, bindingID, raw, base.Add(time.Duration(i/2)*time.Minute))
		require.NoError(t, err)
		texts[id], originals[id] = text, raw
	}
	for i, response := range []any{nil, "", "short output"} {
		id := fmt.Sprintf("no-full-output-%d", i)
		raw := encode(map[string]any{"id": id, "binding_id": 22, "response_text": response})
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO account_quality_runs
			(id,request_id,account_id,binding_id,policy_id,execution_status,evidence,created_at)
			VALUES($1,$1,8,22,1,'completed',$2,$3)`, id, raw, base.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO account_quality_runs
		(id,request_id,account_id,binding_id,policy_id,execution_status,evidence,created_at)
		VALUES('missing-output','missing-output',8,22,1,'completed','{"id":"missing-output","binding_id":22}',$1)`, base.Add(3*time.Minute))
	require.NoError(t, err)

	got, err := f.store.RecentSummaries(t.Context(), []int64{bindingID, 22})
	require.NoError(t, err)
	require.Len(t, got[bindingID], 30)
	for i, run := range got[bindingID] {
		id := fmt.Sprintf("%08x-0000-4000-8000-000000000000", i+6)
		require.Equal(t, id, run.ID)
		runes := []rune(texts[id])
		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		var summary map[string]any
		require.NoError(t, json.Unmarshal(encoded, &summary))
		require.Equal(t, string(runes[len(runes)-240:]), summary["result_preview"])
		require.Len(t, []rune(summary["result_preview"].(string)), 240)
		require.Empty(t, run.Prompt)
		require.Empty(t, run.Expected)
		require.Empty(t, run.ResponseText)
		require.Empty(t, run.Config)
		require.Empty(t, run.IdentityVersion)

		var stored string
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT evidence::text FROM account_quality_runs WHERE id=$1", id).Scan(&stored))
		require.JSONEq(t, originals[id], stored, "preview reads never alter immutable evidence")
	}
	require.Len(t, got[22], 4)
	for i, run := range got[22] {
		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		var summary map[string]any
		require.NoError(t, json.Unmarshal(encoded, &summary))
		if i == 2 {
			require.Equal(t, "short output", summary["result_preview"])
		} else {
			require.NotContains(t, summary, "result_preview")
		}
	}
}
