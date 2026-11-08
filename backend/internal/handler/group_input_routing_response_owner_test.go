//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type inputRoutingResponseOwnerUpstream struct {
	service.HTTPUpstream
	responseBody string
	contentType  string
}

func (u *inputRoutingResponseOwnerUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{u.contentType}},
		Body:       io.NopCloser(strings.NewReader(u.responseBody)),
	}, nil
}

func TestGroupInputRoutingResponsesBindsAuthenticatedResponseOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			groupID := int64(9400)
			group := &service.Group{
				ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic,
				Status: service.StatusActive, RateMultiplier: 1,
				InputRouting: service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{
					ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 500, AccountIDs: []int64{9401},
				}}},
			}
			account := &service.Account{
				ID: 9401, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://api.openai.com"},
			}
			repo := &openAIWSUsageHandlerAccountRepoStub{account: *account}
			snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
			responseID := "resp_input_routing_owner"
			responseJSON := `{"id":"` + responseID + `","object":"response","status":"completed","model":"gpt-5.1","output":[{"id":"msg_owner","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK","annotations":[]}]}],"usage":{"input_tokens":5,"output_tokens":1}}`
			upstream := &inputRoutingResponseOwnerUpstream{responseBody: responseJSON, contentType: "application/json"}
			if stream {
				upstream.contentType = "text/event-stream"
				upstream.responseBody = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + responseJSON + "}\n\n"
			}
			logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
			billing := service.NewBillingService(cfg, nil)
			billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			openai := service.NewOpenAIGatewayService(
				repo, logs, nil, nil, nil, nil, nil, cfg, snapshot, nil, billing,
				nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
			)
			gateway := service.NewGatewayService(
				repo, &fakeGroupRepo{group: group}, logs, nil, nil, nil, nil, nil, cfg,
				snapshot, nil, billing, nil, billingCache, nil, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			)
			gateway.SetInputRoutingOpenAIGateway(openai)
			h := &GatewayHandler{
				gatewayService: gateway, openAIGatewayService: openai, billingCacheService: billingCache,
				cfg: cfg, maxAccountSwitches: 1,
				concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
			}
			apiKey := &service.APIKey{ID: 9402, UserID: 9403, GroupID: &groupID, Group: group, User: &service.User{ID: 9403, Balance: 100}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5.1","input":"hello","stream":%t}`, stream)))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID})

			h.Responses(c)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), responseID)
			_, matched := service.GroupInputRoutingDecisionFromContext(c.Request.Context())
			require.True(t, matched, "the first request must use the public input-routing Responses path")
			for _, check := range []struct {
				name             string
				group, user, key int64
				want             bool
			}{
				{"same owner", groupID, 9403, 9402, true},
				{"same user other key", groupID, 9403, 9404, true},
				{"other user", groupID, 9405, 9402, false},
				{"other source group", groupID + 1, 9403, 9402, false},
			} {
				owned, err := openai.ValidateOpenAIHTTPResponseOwner(context.Background(), check.group, responseID, check.user, check.key)
				require.NoError(t, err)
				require.Equal(t, check.want, owned, check.name)
			}
		})
	}
}
