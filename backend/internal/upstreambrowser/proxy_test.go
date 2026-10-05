package upstreambrowser

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testGuard(t *testing.T, ctx context.Context, upstream string, opts guardProxyOptions) *guardProxy {
	t.Helper()
	guard, err := startGuardProxyWithOptions(ctx, upstream, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	return guard
}

func guardAuth(guard *guardProxy) string {
	user, password := guard.Credentials()
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

func publicGuardLookup(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

func TestGuardPublicAddresses(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0", "0.1.2.3", "10.1.2.3", "100.64.0.1", "127.0.0.1", "168.63.129.16", "169.254.169.254",
		"172.16.0.1", "192.0.0.9", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.1",
		"192.168.0.1", "192.175.48.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:10.1.2.3", "64:ff9b::7f00:1", "64:ff9b:1::1",
		"100::1", "2001::1", "2001:2::1", "2001:20::1", "2001:db8::1", "2002:7f00:1::1",
		"2620:4f:8000::1", "3ffe::1", "3fff::1", "fc00::1", "fe80::1", "ff02::1", "2001:4860::1%eth0",
	} {
		t.Run(address, func(t *testing.T) {
			if guardPublicAddress(netip.MustParseAddr(address)) {
				t.Fatalf("special-use address %s was allowed", address)
			}
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "::ffff:8.8.8.8", "2001:4860:4860::8888", "2606:4700:4700::1111"} {
		if !guardPublicAddress(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was blocked", address)
		}
	}
}

func TestGuardRequiresPerJobProxyCredentials(t *testing.T) {
	g := testGuard(t, context.Background(), "", guardProxyOptions{})
	other := testGuard(t, context.Background(), "", guardProxyOptions{})
	if guardAuth(g) == guardAuth(other) {
		t.Fatal("credentials were reused across jobs")
	}
	if !strings.HasPrefix(g.URL(), "http://127.0.0.1:") {
		t.Fatalf("guard not bound to IPv4 loopback: %s", g.URL())
	}
	for _, tc := range []struct {
		name string
		auth []string
		want int
	}{
		{"absent", nil, http.StatusProxyAuthRequired},
		{"wrong", []string{guardAuth(other)}, http.StatusProxyAuthRequired},
		{"duplicate", []string{guardAuth(g), guardAuth(g)}, http.StatusProxyAuthRequired},
		{"valid", []string{guardAuth(g)}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
			r.Header["Proxy-Authorization"] = tc.auth
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestGuardRejectsMixedDNSAnswersBeforeDial(t *testing.T) {
	var dialed atomic.Bool
	g := testGuard(t, context.Background(), "", guardProxyOptions{
		lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed.Store(true)
			return nil, errors.New("unexpected dial")
		},
	})
	if _, err := g.dialDestination(context.Background(), "example.com:443"); err == nil {
		t.Fatal("mixed public/private DNS answer was accepted")
	}
	if dialed.Load() {
		t.Fatal("dial occurred before all DNS answers were validated")
	}
}

func TestGuardPinsDNSAndRechecksEachConnection(t *testing.T) {
	var lookups atomic.Int32
	var dials atomic.Int32
	g := testGuard(t, context.Background(), "", guardProxyOptions{
		lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
			if lookups.Add(1) == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		dialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			dials.Add(1)
			if network != "tcp" || address != "8.8.8.8:443" {
				t.Errorf("unvalidated destination dialed: %s %s", network, address)
			}
			client, server := net.Pipe()
			t.Cleanup(func() { _ = server.Close() })
			return client, nil
		},
	})
	conn, err := g.dialDestination(context.Background(), "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if _, err = g.dialDestination(context.Background(), "example.com:443"); err == nil {
		t.Fatal("rebound private destination was accepted")
	}
	if dials.Load() != 1 || lookups.Load() != 2 {
		t.Fatalf("dials = %d, lookups = %d", dials.Load(), lookups.Load())
	}
}

func TestGuardHTTPStripsProxyAndHopHeaders(t *testing.T) {
	forwarded := make(chan *http.Request, 1)
	g := testGuard(t, context.Background(), "", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			if address != "8.8.8.8:80" {
				t.Errorf("dial address = %s", address)
			}
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				r, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					return
				}
				forwarded <- r
				_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: X-Secret\r\nX-Secret: hidden\r\nProxy-Authenticate: hidden\r\n\r\nok")
			}()
			return client, nil
		},
	})
	r := httptest.NewRequest(http.MethodGet, "http://example.com/page", nil)
	r.Header.Set("Proxy-Authorization", guardAuth(g))
	r.Header.Set("Connection", "X-Secret")
	r.Header.Set("X-Secret", "hidden")
	r.Header.Set("Proxy-Connection", "keep-alive")
	r.Header.Set("Authorization", "Bearer origin-token")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	select {
	case got := <-forwarded:
		if got.Host != "example.com" || got.URL.Path != "/page" || got.Header.Get("Authorization") != "Bearer origin-token" {
			t.Fatalf("origin request changed: %#v", got)
		}
		for _, header := range []string{"Proxy-Authorization", "Proxy-Connection", "X-Secret"} {
			if got.Header.Get(header) != "" {
				t.Errorf("leaked request header %s", header)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("no request reached pinned origin")
	}
	if w.Header().Get("X-Secret") != "" || w.Header().Get("Proxy-Authenticate") != "" {
		t.Fatal("upstream hop/proxy headers leaked to browser")
	}
}

func TestGuardRejectsProtocolsPortsAndOversizedBodies(t *testing.T) {
	g := testGuard(t, context.Background(), "", guardProxyOptions{lookupIP: publicGuardLookup})
	for _, tc := range []struct {
		method, target, upgrade string
		body                    io.Reader
		want                    int
	}{
		{http.MethodGet, "ftp://example.com/", "", nil, http.StatusForbidden},
		{http.MethodGet, "http://example.com:22/", "", nil, http.StatusForbidden},
		{http.MethodGet, "http://user:pass@example.com/", "", nil, http.StatusForbidden},
		{http.MethodGet, "http://example.com/", "websocket", nil, http.StatusForbidden},
		{http.MethodConnect, "example.com:80", "", nil, http.StatusForbidden},
		{http.MethodPost, "http://example.com/", "", strings.NewReader(strings.Repeat("x", (1<<20)+1)), http.StatusRequestEntityTooLarge},
	} {
		r := httptest.NewRequest(tc.method, tc.target, tc.body)
		r.Header.Set("Proxy-Authorization", guardAuth(g))
		if tc.upgrade != "" {
			r.Header.Set("Upgrade", tc.upgrade)
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s status = %d, want %d", tc.method, tc.target, w.Code, tc.want)
		}
	}
}

func TestGuardExplicitHTTPProxyUsesPinnedCONNECTAndOwnCredentials(t *testing.T) {
	requests := make(chan *http.Request, 1)
	g := testGuard(t, context.Background(), "http://proxy-user:proxy-password@trusted.proxy:3128", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			if address != "trusted.proxy:3128" {
				t.Errorf("explicit proxy bypassed: %s", address)
			}
			client, server := net.Pipe()
			t.Cleanup(func() { _ = server.Close() })
			go func() {
				r, err := http.ReadRequest(bufio.NewReader(server))
				if err == nil {
					requests <- r
					_, _ = io.WriteString(server, "HTTP/1.1 200 Connection Established\r\n\r\n")
				}
			}()
			return client, nil
		},
	})
	conn, err := g.dialDestination(context.Background(), "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case r := <-requests:
		if r.Method != http.MethodConnect || r.Host != "8.8.8.8:443" || r.RequestURI != "8.8.8.8:443" {
			t.Fatalf("proxy did not receive a pinned CONNECT: %#v", r)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password"))
		if r.Header.Get("Proxy-Authorization") != want {
			t.Fatal("explicit proxy credentials missing or replaced by job credentials")
		}
	case <-time.After(time.Second):
		t.Fatal("proxy received no CONNECT")
	}
}

func TestGuardExplicitProxyFailureNeverFallsBack(t *testing.T) {
	var calls atomic.Int32
	g := testGuard(t, context.Background(), "http://trusted.proxy:3128", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			calls.Add(1)
			if address != "trusted.proxy:3128" {
				t.Errorf("explicit proxy bypassed: %s", address)
			}
			return nil, errors.New("proxy unavailable")
		},
	})
	if _, err := g.dialDestination(context.Background(), "example.com:443"); err == nil {
		t.Fatal("proxy failure was ignored")
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected retry/fallback count = %d", calls.Load())
	}
}

func TestGuardHTTPSProxyVerifiesOriginalHostname(t *testing.T) {
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer fixture.Close()
	sni := make(chan string, 1)
	g := testGuard(t, context.Background(), "https://trusted.proxy:8443", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			if address != "trusted.proxy:8443" {
				t.Errorf("wrong proxy address: %s", address)
			}
			client, peer := net.Pipe()
			go func() {
				server := tls.Server(peer, &tls.Config{
					Certificates: fixture.TLS.Certificates,
					GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
						sni <- hello.ServerName
						return nil, nil
					},
				})
				defer server.Close()
				_ = server.Handshake()
			}()
			return client, nil
		},
	})
	if conn, err := g.dialDestination(context.Background(), "example.com:443"); err == nil {
		conn.Close()
		t.Fatal("untrusted HTTPS proxy certificate was accepted")
	}
	select {
	case got := <-sni:
		if got != "trusted.proxy" {
			t.Fatalf("proxy TLS SNI = %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy TLS handshake did not start")
	}
}

func TestGuardCancellationClosesPendingProxyHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	peerClosed := make(chan struct{})
	g := testGuard(t, ctx, "http://trusted.proxy:3128", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				_, _ = http.ReadRequest(bufio.NewReader(peer))
				close(started)
				_, _ = io.Copy(io.Discard, peer)
				close(peerClosed)
			}()
			return client, nil
		},
	})
	finished := make(chan error, 1)
	go func() {
		conn, err := g.dialDestination(ctx, "example.com:443")
		if conn != nil {
			conn.Close()
		}
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("CONNECT did not reach proxy")
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation left proxy handshake blocked")
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("cancellation left proxy connection open")
	}
}

func TestGuardSOCKSProxyReceivesPinnedIPAddress(t *testing.T) {
	result := make(chan error, 1)
	g := testGuard(t, context.Background(), "socks5://trusted.proxy:1080", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			if address != "trusted.proxy:1080" {
				t.Errorf("SOCKS proxy bypassed: %s", address)
			}
			client, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			go func() {
				header := make([]byte, 2)
				if _, err := io.ReadFull(peer, header); err != nil {
					result <- err
					return
				}
				methods := make([]byte, int(header[1]))
				_, _ = io.ReadFull(peer, methods)
				_, _ = peer.Write([]byte{5, 0})
				request := make([]byte, 10)
				_, err := io.ReadFull(peer, request)
				if err == nil && string(request) != string([]byte{5, 1, 0, 1, 8, 8, 8, 8, 1, 187}) {
					err = fmt.Errorf("SOCKS request not pinned: %v", request)
				}
				result <- err
				_, _ = peer.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
			}()
			return client, nil
		},
	})
	conn, err := g.dialDestination(context.Background(), "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestGuardAcceptsPinnedSOCKS5HConfiguration(t *testing.T) {
	upstream, err := parseGuardUpstream("socks5h://fixture-user:fixture-password@proxy.example:7085")
	if err != nil {
		t.Fatal(err)
	}
	if upstream.Scheme != "socks5" || upstream.Hostname() != "proxy.example" || upstream.Port() != "7085" {
		t.Fatalf("wrong pinned-proxy normalization: %s", upstream.Host)
	}
	if username := upstream.User.Username(); username != "fixture-user" {
		t.Fatal("proxy username changed")
	}
}

func TestGuardRejectsMalformedExplicitProxies(t *testing.T) {
	for _, raw := range []string{"ftp://proxy:21", "http://", "http://proxy/path", "http://proxy?query", "http://proxy:0", "http://proxy:65536", "http://proxy/#fragment"} {
		if g, err := startGuardProxy(context.Background(), raw); err == nil {
			g.Close()
			t.Errorf("invalid proxy accepted: %s", raw)
		}
	}
}

func openGuardTunnel(t *testing.T, g *guardProxy) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(g.URL(), "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, err = fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: %s\r\n\r\n", guardAuth(g))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT response = %v, error = %v", response, err)
	}
	return conn, reader
}

func TestGuardCONNECTRejectsPlaintextBeforeForwarding(t *testing.T) {
	received := make(chan []byte, 1)
	g := testGuard(t, context.Background(), "", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				data, _ := io.ReadAll(peer)
				received <- data
			}()
			return client, nil
		},
	})
	conn, reader := openGuardTunnel(t, g)
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\n\r\n")
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("plaintext tunnel remained open")
	}
	select {
	case data := <-received:
		if len(data) != 0 {
			t.Fatalf("plaintext reached destination: %q", data)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected tunnel left destination connection open")
	}
}

func TestGuardCONNECTFragmentedClientHelloAndSessionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peerClosed := make(chan struct{})
	hello := []byte{22, 3, 1, 0, 4, 1, 0, 0, 0}
	g := testGuard(t, ctx, "", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				defer close(peerClosed)
				data := make([]byte, len(hello))
				if _, err := io.ReadFull(peer, data); err != nil {
					return
				}
				_, _ = peer.Write(data)
				_, _ = io.Copy(io.Discard, peer)
			}()
			return client, nil
		},
	})
	conn, reader := openGuardTunnel(t, g)
	for _, fragment := range [][]byte{hello[:2], hello[2:5], hello[5:]} {
		if _, err := conn.Write(fragment); err != nil {
			t.Fatal(err)
		}
	}
	got := make([]byte, len(hello))
	if _, err := io.ReadFull(reader, got); err != nil || string(got) != string(hello) {
		t.Fatalf("fragmented TLS header not forwarded intact: %v, %v", got, err)
	}
	cancel()
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("cancellation left browser tunnel open")
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("cancellation left origin tunnel open")
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if other, err := net.DialTimeout("tcp", strings.TrimPrefix(g.URL(), "http://"), time.Second); err == nil {
		other.Close()
		t.Fatal("closed guard still accepts connections")
	}
}

func TestGuardBoundsConcurrentRequestsAndCancelsThem(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 32)
	g := testGuard(t, ctx, "", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				_, _ = http.ReadRequest(bufio.NewReader(peer))
				started <- struct{}{}
				_, _ = io.Copy(io.Discard, peer)
			}()
			return client, nil
		},
	})
	var handlers sync.WaitGroup
	for i := 0; i < 32; i++ {
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			r.Header.Set("Proxy-Authorization", guardAuth(g))
			g.ServeHTTP(httptest.NewRecorder(), r)
		}()
	}
	for i := 0; i < 32; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("request did not reach origin")
		}
	}
	r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	r.Header.Set("Proxy-Authorization", guardAuth(g))
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("request above concurrency bound returned %d", w.Code)
	}
	cancel()
	finished := make(chan struct{})
	go func() {
		handlers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancellation left HTTP handlers blocked")
	}
}

func TestGuardRejectsOversizedOriginResponseHeaders(t *testing.T) {
	g := testGuard(t, context.Background(), "http://trusted.proxy:3128", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				_, _ = http.ReadRequest(bufio.NewReader(peer))
				_, _ = io.WriteString(peer, "HTTP/1.1 200 OK\r\nX-Large: "+strings.Repeat("x", 17<<10)+"\r\n\r\n")
			}()
			return client, nil
		},
	})
	if conn, err := g.dialDestination(context.Background(), "example.com:443"); err == nil {
		conn.Close()
		t.Fatal("oversized proxy response headers accepted")
	}
}

type guardTestZeroReader struct{}

func (guardTestZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestGuardBoundsTunnelResponseBytes(t *testing.T) {
	g := testGuard(t, context.Background(), "", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				hello := make([]byte, 9)
				if _, err := io.ReadFull(peer, hello); err != nil {
					return
				}
				_, _ = io.CopyN(peer, guardTestZeroReader{}, (8<<20)+1)
			}()
			return client, nil
		},
	})
	conn, reader := openGuardTunnel(t, g)
	_, err := conn.Write([]byte{22, 3, 1, 0, 4, 1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.CopyN(io.Discard, reader, (8<<20)+1)
	if n > 8<<20 || err == nil {
		t.Fatalf("tunnel exceeded response byte budget: n=%d, error=%v", n, err)
	}
}

func TestGuardBoundsResponseBody(t *testing.T) {
	g := testGuard(t, context.Background(), "", guardProxyOptions{
		lookupIP: publicGuardLookup,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go func() {
				defer peer.Close()
				_, _ = http.ReadRequest(bufio.NewReader(peer))
				_, _ = io.WriteString(peer, "HTTP/1.1 200 OK\r\nContent-Length: 8388609\r\n\r\n")
			}()
			return client, nil
		},
	})
	r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	r.Header.Set("Proxy-Authorization", guardAuth(g))
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("oversized response status = %d", w.Code)
	}
}
