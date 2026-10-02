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

func TestGovernanceModelTemplatesValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamGovernanceHandler(gov.NewService(nil, nil, nil, nil, false))
	router := gin.New()
	router.PUT("/templates", h.SaveModelTemplates)
	for _, body := range []string{
		`{"version":-1,"templates":[]}`,
		`{"version":0,"templates":[],"extra":"not allowed"}`,
		`{"version":0,"templates":[{"id":"empty","name":"Empty","platform":"openai","models":[],"is_default":true}]}`,
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/templates", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code, body)
	}
}

func TestGovernanceModelTemplatesInitialRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT value FROM settings").WithArgs("upstream_governance_model_templates").WillReturnRows(sqlmock.NewRows([]string{"value"}))
	h := NewUpstreamGovernanceHandler(gov.NewService(gov.NewSQLStore(db), nil, nil, nil, false))
	router := gin.New()
	router.GET("/templates", h.ModelTemplates)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/templates", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"version":0`)
	require.Contains(t, w.Body.String(), `"templates":[]`)
	require.NoError(t, mock.ExpectationsWereMet())
}
