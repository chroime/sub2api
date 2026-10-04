package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageGenerationPermission(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		allowImage       bool
		wantAllowed      bool
		wantStatus       int
		wantErrorMessage string
	}{
		{
			name:        "text-only remains allowed for a group without image permission",
			body:        `{"generationConfig":{"responseModalities":["TEXT"]}}`,
			allowImage:  false,
			wantAllowed: true,
		},
		{
			name:             "mixed text and image is rejected when group disallows images",
			body:             `{"generationConfig":{"responseModalities":["TEXT","IMAGE"]}}`,
			allowImage:       false,
			wantAllowed:      false,
			wantStatus:       403,
			wantErrorMessage: "Image generation is not allowed for this API key group",
		},
		{
			name:             "lowercase image is rejected case-insensitively",
			body:             `{"generationConfig":{"responseModalities":["image"]}}`,
			allowImage:       false,
			wantAllowed:      false,
			wantStatus:       403,
			wantErrorMessage: "Image generation is not allowed for this API key group",
		},
		{
			name:        "image is allowed for a group with image permission",
			body:        `{"generationConfig":{"responseModalities":["TEXT","IMAGE"]}}`,
			allowImage:  true,
			wantAllowed: true,
		},
		{
			name:        "malformed modality field is not treated as image generation",
			body:        `{"generationConfig":{"responseModalities":"IMAGE"}}`,
			allowImage:  false,
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			apiKey := &service.APIKey{Group: &service.Group{AllowImageGeneration: tt.allowImage}}

			allowed := requireGeminiImageGenerationPermission(c, apiKey, []byte(tt.body))

			require.Equal(t, tt.wantAllowed, allowed)
			if tt.wantStatus == 0 {
				require.Equal(t, http.StatusOK, recorder.Code)
				return
			}

			require.Equal(t, tt.wantStatus, recorder.Code)
			var response struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Status  string `json:"status"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, tt.wantStatus, response.Error.Code)
			require.Equal(t, tt.wantErrorMessage, response.Error.Message)
			require.Equal(t, "PERMISSION_DENIED", response.Error.Status)
		})
	}
}

func TestGeminiImageGenerationPermissionUngroupedKeyRemainsAllowed(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	require.True(t, requireGeminiImageGenerationPermission(c, &service.APIKey{}, []byte(`{"generationConfig":{"responseModalities":["IMAGE"]}}`)))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestGeminiImageGenerationRequestUsesImageModality(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "missing generation config", body: `{}`, want: false},
		{name: "trimmed uppercase image", body: `{"generationConfig":{"responseModalities":[" text "," IMAGE "]}}`, want: true},
		{name: "text only", body: `{"generationConfig":{"responseModalities":["TEXT"]}}`, want: false},
		{name: "invalid json", body: `{`, want: false},
		{name: "non-array modalities", body: `{"generationConfig":{"responseModalities":null}}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, geminiRequestUsesImageModality([]byte(tt.body)))
		})
	}
}
