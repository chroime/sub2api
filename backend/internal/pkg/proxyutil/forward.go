package proxyutil

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"golang.org/x/net/proxy"
)

// ForwardProxyEnv configures the optional SOCKS5 hop used to reach an explicit
// upstream proxy. It never replaces the upstream proxy or applies to direct
// requests. An unset value preserves the normal direct connection to the proxy.
const ForwardProxyEnv = "SUB2API_PROXY_FORWARD_URL"

type ForwardDialer interface {
	proxy.Dialer
	proxy.ContextDialer
}

// NewProxyForwardDialer returns a bounded, cancellable dialer for connecting to
// proxy servers. A configured but invalid/unavailable hop must fail closed.
func NewProxyForwardDialer() (ForwardDialer, error) {
	raw := strings.TrimSpace(os.Getenv(ForwardProxyEnv))
	if raw == "" {
		return socks5ForwardDialer, nil
	}
	_, parsed, err := proxyurl.Parse(raw)
	if err != nil || parsed == nil {
		return nil, fmt.Errorf("invalid %s; expected a SOCKS5 proxy URL", ForwardProxyEnv)
	}
	if parsed.Scheme != "socks5" && parsed.Scheme != "socks5h" {
		return nil, fmt.Errorf("%s only supports socks5 or socks5h", ForwardProxyEnv)
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err != nil {
		return nil, fmt.Errorf("%s requires an explicit host and port", ForwardProxyEnv)
	}
	dialer, err := proxy.FromURL(parsed, socks5ForwardDialer)
	if err != nil {
		return nil, fmt.Errorf("cannot configure %s", ForwardProxyEnv)
	}
	contextDialer, ok := dialer.(ForwardDialer)
	if !ok {
		return nil, fmt.Errorf("%s dialer does not support cancellation", ForwardProxyEnv)
	}
	return contextDialer, nil
}
