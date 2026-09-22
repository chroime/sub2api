package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var geminiSSECommentClients = []struct {
	name, userAgent, apiClient string
	allowComments              bool
}{
	{"go user agent", "google-genai-sdk/1.71.0 gl-go/go1.28", "", false},
	{"python user agent", "google-genai-sdk/1.20.0 gl-python/3.12.4", "", false},
	{"go api client", "", "google-genai-sdk/1.71.0 gl-go/go1.28", false},
	{"python api client", "", "google-genai-sdk/1.20.0 gl-python/3.12.4", false},
	{"rejecting hint wins", "google-genai-sdk/1.9.0 gl-node/22.3.0", "google-genai-sdk/1.71.0 gl-go/go1.28", false},
	{"javascript SDK", "google-genai-sdk/1.9.0 gl-node/22.3.0", "", true},
	{"ordinary client", "curl/8.7.1", "", true},
}

func TestGeminiStreamingACKRespectsClientSSECommentCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{
		"/v1beta/models/gemini:streamGenerateContent?alt=sse",
		"/antigravity/v1beta/models/gemini:streamGenerateContent?alt=sse",
	} {
		for _, tc := range geminiSSECommentClients {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				c, recorder := syntheticFirstResponseHandlerContext()
				c.Request = httptest.NewRequest(http.MethodPost, path, nil)
				c.Request.Header.Set("User-Agent", tc.userAgent)
				c.Request.Header.Set("X-Goog-Api-Client", tc.apiClient)
				eligible := geminiStreamingACKEligible(c, true)
				require.Equal(t, tc.allowComments, eligible)

				h := &GatewayHandler{cfg: gatewayACKTestConfig(5)}
				stop := h.startStreamingACK(c, eligible, time.Now(), gatewayACKTestAccount(service.PlatformGemini, true), nil)
				t.Cleanup(stop)
				if tc.allowComments {
					require.Eventually(t, func() bool { return service.StreamingACKCommitted(c) }, time.Second, time.Millisecond)
				}
				stop()
				if tc.allowComments {
					require.Equal(t, ":\n\n", recorder.Body.String())
				} else {
					require.Empty(t, recorder.Body.String())
					require.False(t, c.Writer.Written(), "an incompatible client must not receive an early stream commit")
					require.Nil(t, service.StreamingACKMs(c))
				}
			})
		}
	}
}

func TestGeminiPrechargeWaitCommentCompatibilityKeepsBillingContext(t *testing.T) {
	for _, tc := range geminiSSECommentClients {
		t.Run(tc.name, func(t *testing.T) {
			turns, wallet, admit := newWSPrechargeFixture(t)
			t.Cleanup(turns.finish)
			parent := turns.context(1)
			c, recorder := newBalancePrechargeHeartbeatContext()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:streamGenerateContent?alt=sse", nil).WithContext(parent)
			c.Request.Header.Set("User-Agent", tc.userAgent)
			c.Request.Header.Set("X-Goog-Api-Client", tc.apiClient)
			started := false
			ctx := balancePrechargeWaitContext(parent, c, geminiStreamingACKEligible(c, true), &started)
			if tc.allowComments {
				require.NotSame(t, parent, ctx, "compatible streams retain their waiting heartbeat")
			} else {
				require.Same(t, parent, ctx, "skip only the heartbeat callback, retaining the existing admission context")
			}
			admit(ctx)
			require.True(t, service.HasBalancePrecharge(ctx), "comment compatibility must never bypass actual precharge")
			require.Equal(t, service.BalancePrechargeBillingID(parent, 1, 2), service.BalancePrechargeBillingID(ctx, 1, 2))
			require.Equal(t, 4.0, wallet.balance)
			require.Len(t, wallet.holds, 1)
			require.False(t, started)
			require.Empty(t, recorder.Body.String())
			turns.finish()
			require.Equal(t, 5.0, wallet.balance)
			require.Empty(t, wallet.holds)
		})
	}
}
