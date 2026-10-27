//go:build unit

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

func TestHomepageTemplateRequestRejectsInvalidExplicitValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []string{`""`, `null`, `"unknown"`, `"CLASSIC"`} {
		t.Run(value, func(t *testing.T) {
			handler := &SettingHandler{}
			router := gin.New()
			router.PUT("/settings", handler.UpdateSettings)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewBufferString(`{"homepage_template":`+value+`}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}

func TestHomepageTemplateOmissionAndAudit(t *testing.T) {
	require.Contains(t, omittedSettingKeys(map[string]json.RawMessage{"site_name": json.RawMessage(`"New name"`)}), service.SettingKeyHomepageTemplate)
	require.NotContains(t, omittedSettingKeys(map[string]json.RawMessage{"homepage_template": json.RawMessage(`"brand"`)}), service.SettingKeyHomepageTemplate)
	changes := diffSettings(&service.SystemSettings{HomepageTemplate: "classic"}, &service.SystemSettings{HomepageTemplate: "brand"}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changes, service.SettingKeyHomepageTemplate)
}
