package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupHandlerUltraFastCreateUpdateAndOmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	handler := NewGroupHandler(svc, nil, nil)
	router := gin.New()
	router.POST("/groups", handler.Create)
	router.PUT("/groups/:id", handler.Update)
	request := func(method, path, body string) {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	request(http.MethodPost, "/groups", `{"name":"ultrafast","platform":"openai","rate_multiplier":1,"force_openai_ultrafast":true}`)
	require.True(t, svc.createdGroups[0].ForceOpenAIUltrafast)
	request(http.MethodPut, "/groups/7", `{"name":"renamed"}`)
	require.Nil(t, svc.updatedGroups[0].ForceOpenAIUltrafast)
	request(http.MethodPut, "/groups/7", `{"force_openai_ultrafast":false}`)
	require.NotNil(t, svc.updatedGroups[1].ForceOpenAIUltrafast)
	require.False(t, *svc.updatedGroups[1].ForceOpenAIUltrafast)
	request(http.MethodPut, "/groups/7", `{"force_openai_ultrafast":true}`)
	require.NotNil(t, svc.updatedGroups[2].ForceOpenAIUltrafast)
	require.True(t, *svc.updatedGroups[2].ForceOpenAIUltrafast)
}
