//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compatACKBeforeRead struct {
	io.Reader
	once sync.Once
	ack  func()
}

func (r *compatACKBeforeRead) Read(data []byte) (int, error) {
	r.once.Do(r.ack)
	return r.Reader.Read(data)
}

func (*compatACKBeforeRead) Close() error { return nil }

func TestForwardAsCompatStreamingACKDoesNotPreventOverloadFailover(t *testing.T) {
	for _, endpoint := range []string{"chat_completions", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"stream":true}`
			path := "/v1/chat/completions"
			if endpoint == "responses" {
				body = `{"model":"claude-sonnet-4-6","input":"hi","stream":true}`
				path = "/v1/responses"
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			ack := &openAISyntheticFirstResponse{writer: c.Writer, startedAt: time.Now(), stop: make(chan struct{})}
			c.Set(openAISyntheticFirstResponseKey, ack)
			stream := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &compatACKBeforeRead{Reader: strings.NewReader(stream), ack: func() { ack.beat() }}}
			svc := &GatewayService{
				cfg:              rawChatCompletionsTestConfig(),
				httpUpstream:     &anthropicHTTPUpstreamRecorder{resp: response},
				rateLimitService: &RateLimitService{accountRepo: &errorPolicyRepoStub{}, cfg: &config.Config{}},
			}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
			var result *ForwardResult
			var err error
			if endpoint == "responses" {
				result, err = svc.ForwardAsResponses(context.Background(), c, account, []byte(body), nil)
			} else {
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(body), nil)
			}
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, 529, failover.StatusCode, "ACK is a heartbeat, so an overload before model output remains retryable")
			require.Nil(t, result, "a retryable failed attempt must not be billed")
			require.Equal(t, ":\n\n", recorder.Body.String(), "the failed attempt must not append a terminal event before retry")
		})
	}
}
