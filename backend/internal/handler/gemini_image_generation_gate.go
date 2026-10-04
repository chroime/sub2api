package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// geminiRequestUsesImageModality reports whether a native Gemini request asks
// for an IMAGE response modality. Malformed or missing modality fields are
// treated as non-image requests so existing text-only compatibility is kept.
func geminiRequestUsesImageModality(body []byte) bool {
	var request struct {
		GenerationConfig *struct {
			ResponseModalities []string `json:"responseModalities"`
		} `json:"generationConfig"`
	}
	if len(body) == 0 || json.Unmarshal(body, &request) != nil || request.GenerationConfig == nil {
		return false
	}
	for _, modality := range request.GenerationConfig.ResponseModalities {
		if strings.EqualFold(strings.TrimSpace(modality), "IMAGE") {
			return true
		}
	}
	return false
}

// requireGeminiImageGenerationPermission rejects image-modality requests for
// groups that have image generation disabled. A nil group keeps legacy
// ungrouped-key behavior and is therefore allowed.
func requireGeminiImageGenerationPermission(c *gin.Context, apiKey *service.APIKey, body []byte) bool {
	if !geminiRequestUsesImageModality(body) || apiKey == nil || service.GroupAllowsImageGeneration(apiKey.Group) {
		return true
	}
	googleError(c, http.StatusForbidden, "Image generation is not allowed for this API key group")
	return false
}
