package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicHomeChannelRepo struct {
	service.ChannelRepository
	channels []service.Channel
	err      error
}

func (r *publicHomeChannelRepo) ListAll(context.Context) ([]service.Channel, error) {
	return r.channels, r.err
}

type publicHomeGroupRepo struct {
	service.GroupRepository
	groups []service.Group
}

func (r *publicHomeGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}

func newPublicHomeTestRouter(channels *publicHomeChannelRepo, groups *publicHomeGroupRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.Handlers{
		AvailableChannel: handler.NewAvailableChannelHandler(service.NewChannelService(channels, groups, nil, nil, nil), nil, nil),
		ModelPlaza:       handler.NewModelPlazaHandler(service.NewModelPlazaService(channels, groups, nil, nil, nil), nil, nil),
	}
	RegisterPublicHomeRoutes(r.Group("/api/v1"), h, nil)
	r.GET("/api/v1/channels/available", h.AvailableChannel.List)
	r.GET("/api/v1/model-plaza", h.ModelPlaza.Get)
	return r
}

func TestPublicHome_AnonymousSafeSummary(t *testing.T) {
	price := 0.000002
	channels := &publicHomeChannelRepo{channels: []service.Channel{
		{ID: 42, Name: "Public channel", Status: service.StatusActive, Description: "internal channel notes", GroupIDs: []int64{1, 2, 999},
			ModelMapping: map[string]map[string]string{"openai": {"public-alias": "internal-upstream-model"}, "anthropic": {"private-model": "private-target"}},
			ModelPricing: []service.ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-public"}, BillingMode: service.BillingModeToken, InputPrice: &price}}},
		{ID: 43, Name: "Private channel", Status: service.StatusActive, GroupIDs: []int64{2}},
		{ID: 44, Name: "Disabled channel", Status: service.StatusDisabled, GroupIDs: []int64{1}},
	}}
	groups := &publicHomeGroupRepo{groups: []service.Group{
		{ID: 1, Name: "Public group", Platform: "openai", Status: service.StatusActive, RateMultiplier: 1},
		{ID: 2, Name: "Exclusive group", Platform: "anthropic", Status: service.StatusActive, IsExclusive: true},
	}}
	r := newPublicHomeTestRouter(channels, groups)
	for _, path := range []string{"/api/v1/public/home/channels", "/api/v1/public/home/pricing"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			// A stale token cannot expand or block this strictly anonymous projection.
			req.Header.Set("Authorization", "Bearer expired-token")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.EqualValues(t, 0, body["code"])
			require.Contains(t, w.Body.String(), "gpt-public")
			for _, private := range []string{"Exclusive group", "Private channel", "Disabled channel", "private-model", "private-target", "internal-upstream-model", "internal channel notes", "user_rate_multiplier", "billing_model_source", "restrict_models"} {
				require.NotContains(t, w.Body.String(), private)
			}
			if path == "/api/v1/public/home/channels" {
				rows, ok := body["data"].([]any)
				require.True(t, ok)
				require.Len(t, rows, 1)
			} else {
				data, ok := body["data"].(map[string]any)
				require.True(t, ok)
				rows, ok := data["groups"].([]any)
				require.True(t, ok)
				require.Len(t, rows, 1)
				group, ok := rows[0].(map[string]any)
				require.True(t, ok)
				models, ok := group["models"].([]any)
				require.True(t, ok)
				require.NotEmpty(t, models)
				model, ok := models[0].(map[string]any)
				require.True(t, ok)
				pricing, ok := model["pricing"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, price, pricing["input_price"])
			}
		})
	}
	for path, expected := range map[string]int{"/api/v1/channels/available": http.StatusUnauthorized, "/api/v1/model-plaza": http.StatusNotFound} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, expected, w.Code, "existing endpoint permission must be unchanged")
	}
}

func TestPublicHome_EmptyAndFailure(t *testing.T) {
	for _, path := range []string{"/api/v1/public/home/channels", "/api/v1/public/home/pricing"} {
		repo := &publicHomeChannelRepo{}
		r := newPublicHomeTestRouter(repo, &publicHomeGroupRepo{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), "[]")
		repo.err = errors.New("database unavailable")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusInternalServerError, w.Code)
	}
}
