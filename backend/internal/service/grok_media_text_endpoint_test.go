//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// xAI answers a text endpoint (chat/completions, responses) with this 404 for
// every Imagine model on every account; the media endpoints serve the same
// model on the same account.
const grokImagineTextEndpointNotFoundBody = `{"code":"not-found","error":"The model grok-imagine-image-quality does not exist or your team 781e8ddf-3da6-4048-912c-56b5196922eb does not have access to it. If you believe this is a mistake, please contact support and quote your team ID and the model name."}`

func grokMediaTextEndpointAccount() *Account {
	return &Account{
		ID:          301,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{},
	}
}

func TestRateLimitService_GrokImagineModelOnTextEndpointSkipsModelCooldown(t *testing.T) {
	for _, model := range []string{"grok-imagine", "grok-imagine-image-quality", "grok-imagine-image-2.0", "grok-imagine-video-1.5"} {
		t.Run(model, func(t *testing.T) {
			repo := &modelNotFoundAccountRepoStub{}
			svc := &RateLimitService{accountRepo: repo}

			handled := svc.HandleUpstreamError(
				context.Background(),
				grokMediaTextEndpointAccount(),
				http.StatusNotFound,
				http.Header{},
				[]byte(grokImagineTextEndpointNotFoundBody),
				model,
			)

			require.False(t, handled, "endpoint mismatch fails on every account; switching accounts only multiplies the 404")
			require.Empty(t, repo.modelRateLimitCalls,
				"a text-endpoint 404 must not hide the account from the media endpoints that can serve the model")
			require.Zero(t, repo.tempCalls)
		})
	}
}

func TestRateLimitService_GrokImagineModelOnImagesEndpointKeepsModelCooldown(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}

	handled := svc.HandleUpstreamError(
		WithOpenAIImagesEndpoint(context.Background()),
		grokMediaTextEndpointAccount(),
		http.StatusNotFound,
		http.Header{},
		[]byte(grokImagineTextEndpointNotFoundBody),
		"grok-imagine-image-quality",
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1, "on the media endpoint the 404 means this account really lacks the model")
	require.Equal(t, upstreamModelNotFoundReason, repo.modelRateLimitCalls[0].reason)
}

func TestRateLimitService_GrokTextModelNotFoundKeepsModelCooldown(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}

	handled := svc.HandleUpstreamError(
		context.Background(),
		grokMediaTextEndpointAccount(),
		http.StatusNotFound,
		http.Header{},
		[]byte(`{"code":"not-found","error":"The model grok-4.9 does not exist or your team t does not have access to it."}`),
		"grok-4.9",
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "grok-4.9", repo.modelRateLimitCalls[0].scope)
}

func TestGrokChatCompletionsImagineModelReturnsEndpointHintWithoutFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &modelNotFoundAccountRepoStub{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(grokImagineTextEndpointNotFoundBody)),
	}

	_, err := svc.handleChatCompletionsErrorResponse(resp, c, grokMediaTextEndpointAccount(), "grok-imagine-image-quality")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "the request must not walk the account pool")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_request_error")
	require.Contains(t, recorder.Body.String(), "/v1/images/generations")
	require.NotContains(t, recorder.Body.String(), "781e8ddf", "the upstream team id must not leak to the client")
	require.Empty(t, repo.modelRateLimitCalls)
	require.Zero(t, repo.tempCalls)
}
