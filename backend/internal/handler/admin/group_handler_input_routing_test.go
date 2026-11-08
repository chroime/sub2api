package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupHandlerInputRoutingCreateUpdateOmissionAndClear(t *testing.T) {
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
	request(http.MethodPost, "/groups", `{"name":"routing","platform":"openai","rate_multiplier":1,"input_routing":{"enabled":true,"rules":[{"id":"small","name":"Small","enabled":true,"request_type":"stream","min_input_tokens":100,"max_input_tokens":2000,"account_ids":[11],"group_ids":[21],"fallback_to_original":true}]}}`)
	require.Len(t, svc.createdGroups, 1)
	require.True(t, svc.createdGroups[0].InputRouting.Enabled)
	rule := svc.createdGroups[0].InputRouting.Rules[0]
	require.Equal(t, "stream", rule.RequestType)
	require.Equal(t, 100, rule.MinInputTokens)
	require.Equal(t, 2000, rule.MaxInputTokens)
	require.Equal(t, []int64{11}, rule.AccountIDs)
	require.Equal(t, []int64{21}, rule.GroupIDs)
	require.True(t, rule.FallbackToOriginal)
	request(http.MethodPut, "/groups/7", `{"name":"renamed"}`)
	require.Nil(t, svc.updatedGroups[0].InputRouting)
	request(http.MethodPut, "/groups/7", `{"input_routing":{"enabled":false,"rules":[]}}`)
	require.NotNil(t, svc.updatedGroups[1].InputRouting)
	require.False(t, svc.updatedGroups[1].InputRouting.Enabled)
	require.Empty(t, svc.updatedGroups[1].InputRouting.Rules)
	request(http.MethodPut, "/groups/7", `{"input_routing":{"enabled":true,"rules":[{"id":"all-lengths","enabled":true,"request_type":"sync","input_tokens_mode":"all","group_ids":[21]}]}}`)
	allRule := svc.updatedGroups[2].InputRouting.Rules[0]
	require.Equal(t, "all", allRule.InputTokensMode)
	require.Equal(t, "sync", allRule.RequestType)
	require.Equal(t, 0, allRule.MinInputTokens)
	require.Equal(t, 0, allRule.MaxInputTokens)
}
