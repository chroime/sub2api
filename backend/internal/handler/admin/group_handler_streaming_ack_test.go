package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupHandlerStreamingACKPassesPolicyForEveryPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{"openai", "anthropic", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "composite"} {
		t.Run(platform, func(t *testing.T) {
			svc := newStubAdminService()
			h := NewGroupHandler(svc, nil, nil)
			router := gin.New()
			router.POST("/groups", h.Create)
			router.PUT("/groups/:id", h.Update)
			for _, enabled := range []bool{false, true} {
				for _, method := range []string{http.MethodPost, http.MethodPut} {
					path := "/groups"
					if method == http.MethodPut {
						path += "/7"
					}
					body := fmt.Sprintf(`{"name":"group","platform":%q,"rate_multiplier":1,"streaming_ack_enabled":%t}`, platform, enabled)
					req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
					req.Header.Set("Content-Type", "application/json")
					res := httptest.NewRecorder()
					router.ServeHTTP(res, req)
					require.Equal(t, http.StatusOK, res.Code, res.Body.String())
					if method == http.MethodPost {
						require.Equal(t, enabled, svc.createdGroups[len(svc.createdGroups)-1].StreamingACKEnabled)
					} else {
						require.Equal(t, &enabled, svc.updatedGroups[len(svc.updatedGroups)-1].StreamingACKEnabled)
					}
				}
			}
		})
	}
}

func TestGroupRequestsStreamingACKDefaultAndNull(t *testing.T) {
	for _, body := range []string{`{}`, `{"streaming_ack_enabled":null}`} {
		var create CreateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(body), &create))
		require.False(t, create.StreamingACKEnabled)
		var update UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(body), &update))
		require.Nil(t, update.StreamingACKEnabled)
	}
}

func TestGroupHandlerStreamingACKSimpleModePreservesPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	router := newSimpleModeGroupRouter(svc)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		path := "/groups"
		if method == http.MethodPut {
			path += "/7"
		}
		req := httptest.NewRequest(method, path, bytes.NewBufferString(`{"name":"group","platform":"anthropic","streaming_ack_enabled":true}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		require.Contains(t, res.Body.String(), `"streaming_ack_enabled":null`, "simple-mode admin responses must expose the stored tri-state policy")
		if method == http.MethodPost {
			require.True(t, svc.createdGroups[0].StreamingACKEnabled)
		} else {
			require.NotNil(t, svc.updatedGroups[0].StreamingACKEnabled)
			require.True(t, *svc.updatedGroups[0].StreamingACKEnabled)
		}
	}
}

func TestGroupHandlerSimpleModeSerializesExplicitStreamingACK(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		encoded, err := json.Marshal(groupForSimpleMode(&service.Group{StreamingACKEnabled: &enabled}))
		require.NoError(t, err)
		require.Contains(t, string(encoded), fmt.Sprintf(`"streaming_ack_enabled":%t`, enabled))
	}
}
