package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupHandlerIdentityBindsCreateUpdateAndSimpleMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, simple := range []bool{false, true} {
		svc := newStubAdminService()
		router := gin.New()
		if simple {
			router = newSimpleModeGroupRouter(svc)
		} else {
			h := NewGroupHandler(svc, nil, nil)
			router.POST("/groups", h.Create)
			router.PUT("/groups/:id", h.Update)
		}
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			path := "/groups"
			if method == http.MethodPut {
				path += "/7"
			}
			req := httptest.NewRequest(method, path, bytes.NewBufferString(`{"name":"group","platform":"anthropic","rate_multiplier":1,"identity":{"label":"自营","color":"teal"}}`))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
			var input any
			if method == http.MethodPost {
				input = svc.createdGroups[0]
			} else {
				input = svc.updatedGroups[0]
			}
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			var fields struct{ Identity map[string]string }
			require.NoError(t, json.Unmarshal(raw, &fields))
			require.Equal(t, map[string]string{"label": "自营", "color": "teal"}, fields.Identity)
		}
	}
}

func TestGroupHandlerSimpleModeIdentityResponse(t *testing.T) {
	group := &service.Group{ID: 7, Name: "group"}
	require.NoError(t, json.Unmarshal([]byte(`{"identity":{"label":"严选","color":"blue"}}`), group))
	data, err := json.Marshal(groupForSimpleMode(group))
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	require.Equal(t, map[string]any{"label": "严选", "color": "blue"}, fields["identity"])
	require.NotContains(t, fields, "input_routing")
}
