//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupImageDefaultsRepository struct {
	service.AdminGroupRepository
	created *service.Group
}

func (r *groupImageDefaultsRepository) Create(_ context.Context, group *service.Group) error {
	group.ID = 1
	r.created = group
	return nil
}

func TestGroupHandlerCreateImageGenerationDefaultsAndExplicitValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []struct {
		name         string
		defaultValue bool
	}{
		{name: "gemini", defaultValue: true},
		{name: "antigravity", defaultValue: true},
		{name: "grok", defaultValue: true},
		{name: "openai", defaultValue: false},
		{name: "anthropic", defaultValue: false},
		{name: "composite", defaultValue: false},
	} {
		for _, test := range []struct {
			name  string
			field string
			want  bool
		}{
			{name: "omitted", want: platform.defaultValue},
			{name: "null", field: `,"allow_image_generation":null`, want: platform.defaultValue},
			{name: "disabled", field: `,"allow_image_generation":false`, want: false},
			{name: "enabled", field: `,"allow_image_generation":true`, want: true},
		} {
			t.Run(platform.name+"/"+test.name, func(t *testing.T) {
				repo := &groupImageDefaultsRepository{}
				svc := service.NewAdminService(nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				h := NewGroupHandler(svc, nil, nil)
				router := gin.New()
				router.POST("/groups", h.Create)
				body := fmt.Sprintf(`{"name":"images","platform":%q,"rate_multiplier":1,"allow_batch_image_generation":true,"audio_realtime_price_per_min":0.125,"audio_tts_price_per_million_chars":9.5,"audio_stt_price_per_hour":0.4,"audio_rate_independent":true,"audio_rate_multiplier":0.75%s}`, platform.name, test.field)
				req := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)

				require.Equal(t, http.StatusOK, res.Code, res.Body.String())
				require.NotNil(t, repo.created)
				require.Equal(t, test.want, repo.created.AllowImageGeneration)
				if !test.want {
					require.False(t, repo.created.AllowBatchImageGeneration)
				}
				require.True(t, repo.created.AudioRateIndependent)
				require.Equal(t, 0.75, repo.created.AudioRateMultiplier)
				require.NotNil(t, repo.created.AudioRealtimePricePerMin)
				require.NotNil(t, repo.created.AudioTTSPricePerMillionChars)
				require.NotNil(t, repo.created.AudioSTTPricePerHour)
				require.Equal(t, 0.125, *repo.created.AudioRealtimePricePerMin)
				require.Equal(t, 9.5, *repo.created.AudioTTSPricePerMillionChars)
				require.Equal(t, 0.4, *repo.created.AudioSTTPricePerHour)
				var response struct {
					Data struct {
						AllowImageGeneration bool `json:"allow_image_generation"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
				require.Equal(t, test.want, response.Data.AllowImageGeneration)
			})
		}
	}
}
