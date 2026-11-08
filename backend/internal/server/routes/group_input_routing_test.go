package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupInputRoutingPrecedesCompositeTargetWithoutChangingBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary-composite", true: "input-rule"}[enabled], func(t *testing.T) {
			group := &service.Group{ID: 7, Platform: service.PlatformComposite, RateMultiplier: 0.35, InputRouting: service.GroupInputRoutingConfig{
				Enabled: enabled, Rules: []service.GroupInputRoutingRule{{ID: "cross-provider", Enabled: true, RequestType: "all", MaxInputTokens: 1000, AccountIDs: []int64{30}}},
			}}
			key := &service.APIKey{Group: group, GroupID: &group.ID}
			resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{{
				ID: 1, GroupID: 7, PublicModel: "public-model", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformAnthropic, UpstreamModel: "source-only-model", Endpoint: service.CompositeRouteEndpointAny, Enabled: true,
			}}})
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyAPIKey), key); c.Next() })
			r.Use((&handler.GatewayHandler{}).GroupInputRoutingMiddleware(), compositeTargetPlatformMiddleware(resolver))
			r.POST("/v1/responses", func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, enabled, handler.UseGroupInputRoutingGateway(c))
				_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
				require.Equal(t, !enabled, resolved)
				if enabled {
					require.Contains(t, string(body), `"model":"public-model"`)
				} else {
					require.Contains(t, string(body), `"model":"source-only-model"`)
				}
				stored, _ := middleware.GetAPIKeyFromContext(c)
				require.Same(t, key, stored)
				require.Equal(t, 0.35, stored.Group.RateMultiplier)
				c.Status(http.StatusNoContent)
			})
			res := httptest.NewRecorder()
			r.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"public-model","input":"hello"}`)))
			require.Equal(t, http.StatusNoContent, res.Code)
		})
	}
}
