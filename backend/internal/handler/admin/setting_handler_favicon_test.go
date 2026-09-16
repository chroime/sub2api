//go:build unit

package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingsSiteFaviconIndependentUpdateAndClear(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		"site_logo":                      "/page-logo.svg",
		"site_favicon":                   "/old-tab.png",
		"oidc_connect_use_pkce":          "true",
		"oidc_connect_validate_id_token": "true",
	})

	get := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(get)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, get.Code)
	assertFaviconSettingsResponse(t, get, "/old-tab.png", "/page-logo.svg")

	before, err := h.settingService.GetAllSettings(context.Background())
	require.NoError(t, err)
	updated := doUpdateSettings(t, h, map[string]any{"site_favicon": "/new-tab.svg"}, nil)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Equal(t, "/new-tab.svg", repo.values["site_favicon"])
	require.Equal(t, "/page-logo.svg", repo.values["site_logo"])
	assertFaviconSettingsResponse(t, updated, "/new-tab.svg", "/page-logo.svg")
	after, err := h.settingService.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"site_favicon"}, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}))

	partial := doUpdateSettings(t, h, map[string]any{"site_name": "New title"}, nil)
	require.Equal(t, http.StatusOK, partial.Code, partial.Body.String())
	require.Equal(t, "/new-tab.svg", repo.values["site_favicon"])

	logoOnly := doUpdateSettings(t, h, map[string]any{"site_logo": "/new-page-logo.png"}, nil)
	require.Equal(t, http.StatusOK, logoOnly.Code, logoOnly.Body.String())
	require.Equal(t, "/new-tab.svg", repo.values["site_favicon"])

	cleared := doUpdateSettings(t, h, map[string]any{"site_favicon": ""}, nil)
	require.Equal(t, http.StatusOK, cleared.Code, cleared.Body.String())
	require.Empty(t, repo.values["site_favicon"])
	require.Equal(t, "/new-page-logo.png", repo.values["site_logo"])
	assertFaviconSettingsResponse(t, cleared, "", "/new-page-logo.png")
}

func assertFaviconSettingsResponse(t *testing.T, recorder *httptest.ResponseRecorder, favicon, logo string) {
	t.Helper()
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Contains(t, body.Data, "site_favicon")
	require.Equal(t, favicon, body.Data["site_favicon"])
	require.Equal(t, logo, body.Data["site_logo"])
}

func TestUpdateSettingsSiteFaviconUploadSize(t *testing.T) {
	for _, tt := range []struct {
		name string
		size int
		code int
	}{
		{name: "one_MiB_image_with_base64_overhead", size: 1 << 20, code: http.StatusOK},
		{name: "over_one_MiB_image", size: 1<<20 + 1, code: http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{"site_favicon": "/old-tab.png"})
			value := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, tt.size))
			rec := doUpdateSettings(t, h, map[string]any{"site_favicon": value}, nil)
			require.Equal(t, tt.code, rec.Code)
			if tt.code == http.StatusOK {
				require.True(t, value == repo.values["site_favicon"], "the complete data URI must be stored")
			} else {
				require.Equal(t, "/old-tab.png", repo.values["site_favicon"])
			}
		})
	}
}

func TestUpdateSettingsSiteFaviconRejectsInvalidOrOversizedData(t *testing.T) {
	for _, value := range []string{
		"data:image/png;base64,not!base64",
		"data:image/svg+xml,%invalid",
		"data:image/svg+xml," + string(make([]byte, 1<<20+1)),
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 2<<20)),
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{"site_favicon": "/old-tab.png"})
		rec := doUpdateSettings(t, h, map[string]any{"site_favicon": value}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "/old-tab.png", repo.values["site_favicon"])
	}
}
