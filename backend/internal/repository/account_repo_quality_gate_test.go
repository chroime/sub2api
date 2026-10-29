package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountQualityGateSchedulerMetadataPreservesAdmission(t *testing.T) {
	a := service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Extra: map[string]any{"account_quality_gates": map[string]any{
			"12": map[string]any{"scope": "model", "model": "gpt-fixture", "blocked": true},
		}, "private_evidence": "do-not-cache"},
	}
	payload, err := json.Marshal(buildSchedulerMetadataAccount(a))
	require.NoError(t, err)
	var restored service.Account
	require.NoError(t, json.Unmarshal(payload, &restored))
	require.Contains(t, restored.Extra, "account_quality_gates")
	require.NotContains(t, restored.Extra, "private_evidence")
	require.False(t, restored.IsSchedulableForModel("gpt-fixture"))
	require.True(t, restored.IsSchedulableForModel("other-model"))
}

func TestAccountQualityGateGeneralEditUsesLockedRuntimeState(t *testing.T) {
	for _, currentJSON := range []string{`{"account_quality_gates":{"17":{"scope":"account","blocked":true}}}`, `{}`} {
		t.Run(currentJSON, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectQuery(`(?s)SELECT.*FOR NO KEY UPDATE`).
				WithArgs(int64(7), service.PlatformOpenAI, service.AccountTypeAPIKey, `{"api_key":"fixture"}`, nil).
				WillReturnRows(sqlmock.NewRows([]string{"identity_unchanged", "ollama_group_unchanged", "ollama_proxy_unchanged", "enabled", "rate_sync_enabled", "snapshot", "ollama_session", "ollama_auto", "ollama_snapshot", "opencode_group_unchanged", "opencode_auto", "opencode_snapshot", "grok_observed_models", "upstream_model_metadata", "current_extra"}).
					AddRow(true, false, true, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil, []byte(currentJSON)))
			a := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "fixture"},
				Extra:       map[string]any{"account_quality_gates": map[string]any{"forged": true}, "ordinary": "keep"},
			}
			got, err := lockAndMergeAccountProbeExtra(context.Background(), client, a, nil, nil)
			require.NoError(t, err)
			var current map[string]any
			require.NoError(t, json.Unmarshal([]byte(currentJSON), &current))
			require.Equal(t, current["account_quality_gates"], got["account_quality_gates"])
			require.Equal(t, "keep", got["ordinary"])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountQualityGateUpdateExtraRejectsReservedRuntimeWrites(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	require.NoError(t, repo.UpdateExtra(context.Background(), 7, map[string]any{"account_quality_gates": nil}))
	require.NoError(t, mock.ExpectationsWereMet(), "reserved-only edits should perform no SQL")
}

func TestAccountQualityGateBulkUpdateRejectsReservedRuntimeWrites(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	_, err := repo.BulkUpdate(context.Background(), []int64{7, 8}, service.AccountBulkUpdate{Extra: map[string]any{
		"account_quality_gates": nil, "ordinary": "keep",
	}})
	require.NoError(t, err)
	require.Len(t, exec.execArgs, 1)
	payload, ok := exec.execArgs[0][0].([]byte)
	require.True(t, ok)
	require.JSONEq(t, `{"ordinary":"keep"}`, string(payload))
}

func TestAccountQualityGateCreateRejectsImportedRuntimeState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "accounts"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	a := &service.Account{Name: "fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Extra: map[string]any{"account_quality_gates": map[string]any{"17": map[string]any{"scope": "account", "blocked": true}}, "ordinary": "keep"},
	}
	require.NoError(t, createAccountRecord(context.Background(), client, a))
	require.NotContains(t, a.Extra, "account_quality_gates", "new account identities must never import another account's gates")
	require.Equal(t, "keep", a.Extra["ordinary"])
	require.NoError(t, mock.ExpectationsWereMet())
}
