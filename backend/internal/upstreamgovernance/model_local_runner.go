package upstreamgovernance

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// localGatewayModelRunner reuses the governance streaming parser while
// rewriting requests to a trusted loopback gateway address. The public-host
// restrictions used for remote upstreams are intentionally not involved.
type localGatewayModelRunner struct {
	base   *url.URL
	client *platformConnector
}

// NewLocalGatewayModelRunner creates a runner for a loopback-only gateway URL.
// The URL is server-configured; callers cannot provide it through a policy.
func NewLocalGatewayModelRunner(baseURL string) (LocalModelRunner, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, ErrInvalid
	}
	host := strings.ToLower(strings.TrimSuffix(base.Hostname(), "."))
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, ErrInvalid
		}
	}
	if base.Port() == "" {
		return nil, ErrInvalid
	}
	runner := &localGatewayModelRunner{base: base}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The gateway URL is loopback-only. Never let ambient proxy environment
	// variables send an administrator's API key to an external proxy.
	transport.Proxy = nil
	runner.client = &platformConnector{factory: func(context.Context, Site) (HTTPDoer, error) {
		return &http.Client{
			Transport: loopbackRewriteTransport{target: base, base: transport},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}, nil
	}}
	return runner, nil
}

func (r *localGatewayModelRunner) RunLocalModel(ctx context.Context, target LocalModelTarget, request ModelRunRequest) (ModelRunResult, error) {
	if r == nil || r.client == nil || target.Key == "" || target.GroupID <= 0 || target.APIKeyID <= 0 {
		return ModelRunResult{ErrorCode: "invalid_local_target"}, ErrInvalid
	}
	// RunModel requires a validated HTTPS governance Site. The transport
	// rewrites this synthetic origin to the configured loopback URL before dial.
	site := Site{BaseURL: "https://local.gateway.invalid", Platform: "sub2api", Enabled: true}
	return r.client.RunModel(ctx, site, RemoteKey{Key: target.Key}, request)
}

type loopbackRewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t loopbackRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.target == nil || t.base == nil {
		return nil, errors.New("local gateway transport is unavailable")
	}
	clone := req.Clone(req.Context())
	u := *req.URL
	u.Scheme, u.Host = t.target.Scheme, t.target.Host
	clone.URL = &u
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}
