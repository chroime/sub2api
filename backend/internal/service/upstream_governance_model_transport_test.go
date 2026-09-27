package service

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type governanceModelTransport struct {
	HTTPUpstream
	concurrency            int
	profile                HTTPUpstreamProfile
	publicOnly, noRedirect bool
}

func (t *governanceModelTransport) Do(req *http.Request, _ string, _ int64, concurrency int) (*http.Response, error) {
	t.concurrency = concurrency
	t.profile = HTTPUpstreamProfileFromContext(req.Context())
	t.publicOnly = HTTPUpstreamPublicHostsOnly(req.Context())
	t.noRedirect = HTTPUpstreamRedirectsDisabled(req.Context())
	answer := "OK"
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		if nonce := regexp.MustCompile(`GOV_PROBE_[0-9a-f]+`).Find(body); len(nonce) > 0 {
			answer = string(nonce)
		}
	}
	stream := fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", answer)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, nil
}

func TestGovernanceModelTransportDoesNotCapBatchAtTwoConnections(t *testing.T) {
	upstream := &governanceModelTransport{}
	runner := gov.NewConnector(governanceClientFactory(upstream, nil)).(gov.ModelRunner)
	_, err := runner.RunModel(t.Context(), gov.Site{ID: 1, Platform: "sub2api", BaseURL: "https://fixture.example"}, gov.RemoteKey{Key: "fixture-key"}, gov.ModelRunRequest{Config: gov.ModelTestConfig{Platform: "openai", Model: "gpt-5", APIMode: "chat_completions", Concurrency: 8, MaxOutputTokens: 64, TimeoutSeconds: 10, Tokenizer: "o200k_base"}, Template: "probe", Effort: "low"})
	require.NoError(t, err)
	require.Equal(t, 32, upstream.concurrency)
	require.Equal(t, HTTPUpstreamProfileLongStream, upstream.profile)
	require.True(t, upstream.publicOnly)
	require.True(t, upstream.noRedirect)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://fixture.example", nil)
	resp, err := (governanceHTTPClient{upstream: upstream}).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, 2, upstream.concurrency)
	require.Equal(t, HTTPUpstreamProfileDefault, upstream.profile)
}
