package admin

import (
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGovernanceInputBoundsAndSecretMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"name":"valid","has_credential":true}`, `{"session_cipher":"canary"}`, `{"name":"` + strings.Repeat("x", 128<<10) + `"}`, `{} {}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		var in governanceSiteInput
		require.False(t, governanceBody(c, &in))
	}
}
func TestGovernanceErrorDoesNotExposeCause(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	governanceError(c, errors.New("canary-password upstream-body"))
	require.NotContains(t, w.Body.String(), "canary")
	require.Contains(t, w.Body.String(), `"reason":"operation_failed"`)
}

func TestGovernanceErrorExplainsSiteInUse(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.True(t, governanceError(c, gov.ErrSiteInUse))
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), `"reason":"site_in_use"`)
	require.Contains(t, w.Body.String(), "active account bindings, pending imports, or managed keys")
	require.NotContains(t, w.Body.String(), "stale_preview")
}
func TestGovernanceIDsAndPaginationAreStrict(t *testing.T) {
	for _, id := range []string{"0", "-1", "+1", "01", "9223372036854775808"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "id", Value: id}}
		_, ok := governanceID(c, "id")
		require.False(t, ok)
	}
	for _, q := range []string{"?page=0", "?page_size=101", "?page=no"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/"+q, nil)
		_, _, ok := governancePage(c)
		require.False(t, ok)
	}
}

func TestGovernanceHandlerPassesPageAndSizeToSQLStore(t *testing.T) {
	for _, kind := range []string{"events", "checks"} {
		t.Run(kind, func(t *testing.T) {
			db, m, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			table := "upstream_governance_" + kind
			m.ExpectQuery("SELECT COUNT\\(\\*\\) FROM " + table).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(41))
			m.ExpectQuery("SELECT .* FROM "+table+" .*LIMIT \\$2 OFFSET \\$3").WithArgs(int64(1), 20, 20).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			h := NewUpstreamGovernanceHandler(gov.NewService(gov.NewSQLStore(db), nil, nil, nil, false))
			r := gin.New()
			if kind == "events" {
				r.GET("/sites/:id/events", h.Events)
			} else {
				r.GET("/sites/:id/checks", h.Checks)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/sites/1/"+kind+"?page=2&page_size=20", nil))
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), `"items":[]`)
			require.Contains(t, w.Body.String(), `"page":2`)
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
