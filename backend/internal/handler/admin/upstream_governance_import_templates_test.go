package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const importTemplateRequest = `{"version":0,"templates":[{"id":"basic","name":"Basic","is_default":false,"settings":{"concurrency":1,"priority":0,"quota_enabled":false,"quota_daily_limit":0,"quota_weekly_limit":0,"quota_limit":0,"upstream_billing_rate_sync_enabled":false,"openai_long_context_billing_enabled":false}}]}`

func TestGovernanceImportTemplatesStrictRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(gov.NewService(nil, nil, nil, nil, false))
	r := gin.New()
	r.PUT("/templates", h.SaveImportTemplates)
	for _, body := range []string{
		`{}`, `null`, `{"version":0}`, `{"version":0,"templates":null}`, `{"version":0,"templates":[null]}`, `{"version":null,"templates":[]}`, `{"version":-1,"templates":[]}`, `{"version":9223372036854775807,"templates":[]}`,
		strings.Replace(importTemplateRequest, `"settings":{`, `"settings":{"credentials":{"password":"secret-canary"}},"other":{`, 1),
		strings.Replace(importTemplateRequest, `"settings":{`, `"settings":{"model_mapping":{},`, 1),
		strings.Replace(importTemplateRequest, `"settings":{`, `"settings":{"local_group_id":7,`, 1),
		strings.Replace(importTemplateRequest, `"settings":{`, `"settings":{"cost_multiplier":0.2,`, 1),
		strings.Replace(importTemplateRequest, `"quota_enabled":false`, `"quota_enabled":null`, 1),
		strings.Replace(importTemplateRequest, `"is_default":false,`, "", 1),
		strings.Replace(importTemplateRequest, `"concurrency":1`, `"concurrency":1.5`, 1),
		strings.Replace(importTemplateRequest, `"concurrency":1`, `"concurrency":2147483648`, 1),
		strings.Replace(importTemplateRequest, `"priority":0`, `"priority":-1`, 1),
		strings.Replace(importTemplateRequest, `"quota_daily_limit":0`, `"quota_daily_limit":-1`, 1),
		strings.Replace(importTemplateRequest, `"quota_daily_limit":0`, `"quota_daily_limit":1e999`, 1),
		strings.Replace(importTemplateRequest, `"name":"Basic"`, `"name":"`+strings.Repeat("x", 128<<10)+`"`, 1),
		importTemplateRequest + ` {}`, importTemplateRequest + ` null`,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/templates", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code, body[:min(len(body), 200)])
		require.NotContains(t, w.Body.String(), "secret-canary")
	}
}

func TestGovernanceImportTemplatesInitialRead(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	m.ExpectQuery("SELECT value FROM settings").WithArgs("upstream_governance_import_templates").WillReturnRows(sqlmock.NewRows([]string{"value"}))
	h := NewUpstreamGovernanceHandler(gov.NewService(gov.NewSQLStore(db), nil, nil, nil, false))
	r := gin.New()
	r.GET("/templates", h.ImportTemplates)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/templates", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"version":0`)
	require.Contains(t, w.Body.String(), `"templates":[]`)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestGovernanceImportTemplatesPreservesExplicitFalseAndReturnsConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "conflict"}[conflict], func(t *testing.T) {
			db, m, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			m.ExpectBegin()
			m.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("upstream_governance_import_templates").WillReturnResult(sqlmock.NewResult(0, 1))
			rows := sqlmock.NewRows([]string{"value"})
			if conflict {
				rows.AddRow(`{"version":1,"templates":[]}`)
			}
			m.ExpectQuery("SELECT value FROM settings WHERE key=\\$1 FOR UPDATE").WithArgs("upstream_governance_import_templates").WillReturnRows(rows)
			if conflict {
				m.ExpectRollback()
			} else {
				m.ExpectExec("INSERT INTO settings").WithArgs("upstream_governance_import_templates", strings.Replace(importTemplateRequest, `"version":0`, `"version":1`, 1)).WillReturnResult(sqlmock.NewResult(1, 1))
				m.ExpectCommit()
			}
			h := NewUpstreamGovernanceHandler(gov.NewService(gov.NewSQLStore(db), nil, nil, nil, false))
			r := gin.New()
			r.PUT("/templates", h.SaveImportTemplates)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/templates", strings.NewReader(importTemplateRequest)))
			if conflict {
				require.Equal(t, http.StatusConflict, w.Code)
				require.Contains(t, w.Body.String(), `"reason":"stale_preview"`)
			} else {
				require.Equal(t, http.StatusOK, w.Code)
				require.Contains(t, w.Body.String(), `"quota_enabled":false`)
				require.Contains(t, w.Body.String(), `"priority":0`)
				require.Contains(t, w.Body.String(), `"version":1`)
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
