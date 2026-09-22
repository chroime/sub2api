package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthFrozenFundsReachBillingAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"openai", "google"} {
		for _, tc := range []struct {
			name    string
			balance float64
			frozen  float64
			want    int
		}{
			{"temporarily frozen", 0, .08, http.StatusOK},
			{"remaining refundable funds", -.01, .08, http.StatusOK},
			{"truly depleted", 0, 0, http.StatusForbidden},
			{"debt exceeds frozen", -.10, .08, http.StatusForbidden},
			{"no net funds", -.08, .08, http.StatusForbidden},
			{"positive balance", .01, 0, http.StatusOK},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				keyService := newTestAPIKeyService(fakeAPIKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
					return &service.APIKey{ID: 1, Status: service.StatusActive, User: &service.User{
						ID: 123, Status: service.StatusActive, Balance: tc.balance, FrozenBalance: tc.frozen,
					}}, nil
				}})
				cfg := &config.Config{RunMode: config.RunModeStandard}
				router := gin.New()
				if protocol == "google" {
					router.Use(APIKeyAuthWithSubscriptionGoogle(keyService, nil, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keyService, nil, cfg)))
				}
				router.POST("/v1/responses", func(c *gin.Context) { c.Status(http.StatusOK) })
				request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				request.Header.Set("Authorization", "Bearer pending-funds")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, tc.want, recorder.Code, recorder.Body.String())
			})
		}
	}
}
