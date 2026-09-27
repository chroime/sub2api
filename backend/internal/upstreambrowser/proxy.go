package upstreambrowser

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// Each job lasts at most ten minutes with 32 active requests/tunnels. HTTP
// bodies are capped separately; opaque TLS tunnels allow 8 MiB per direction.
const (
	guardSessionLifetime = 10 * time.Minute
	guardDialTimeout     = 15 * time.Second
	guardMaxHeaders      = 16 << 10
	guardMaxRequestBody  = 1 << 20
	guardMaxResponseBody = 8 << 20
	guardMaxTunnelBytes  = 8 << 20
	guardMaxActive       = 32
	guardMaxConnections  = 96
)

var errGuardDestinationBlocked = errors.New("browser destination blocked")

// Non-public and special-purpose ranges are denied even where an IANA range
// contains a globally reachable exception. IPv6 must also be in 2000::/3.
var guardSpecialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("168.63.129.16/32"), // Azure platform virtual IP.
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3ffe::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func guardPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range guardSpecialNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

type guardProxyOptions struct {
	lookupIP    func(context.Context, string, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

type guardProxy struct {
	ctx       context.Context
	cancel    context.CancelFunc
	listener  net.Listener
	server    *http.Server
	transport *http.Transport
	upstream  *url.URL
	opts      guardProxyOptions
	username  string
	password  string
	auth      string
	active    chan struct{}
	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

func startGuardProxy(ctx context.Context, explicitProxy string) (*guardProxy, error) {
	return startGuardProxyWithOptions(ctx, explicitProxy, guardProxyOptions{})
}

func startGuardProxyWithOptions(ctx context.Context, explicitProxy string, opts guardProxyOptions) (*guardProxy, error) {
	upstream, err := parseGuardUpstream(explicitProxy)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.lookupIP == nil {
		opts.lookupIP = net.DefaultResolver.LookupNetIP
	}
	if opts.dialContext == nil {
		opts.dialContext = (&net.Dialer{Timeout: guardDialTimeout, KeepAlive: 15 * time.Second}).DialContext
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("create browser proxy credentials: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, guardSessionLifetime)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen for browser proxy: %w", err)
	}
	g := &guardProxy{
		ctx: ctx, cancel: cancel, listener: listener, upstream: upstream, opts: opts,
		username: "browser", password: base64.RawURLEncoding.EncodeToString(token),
		active: make(chan struct{}, guardMaxActive), conns: make(map[net.Conn]struct{}),
	}
	g.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(g.username+":"+g.password))
	g.transport = &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				return nil, errGuardDestinationBlocked
			}
			return g.dialDestination(ctx, address)
		},
		DisableKeepAlives: true, ForceAttemptHTTP2: false,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		MaxResponseHeaderBytes: guardMaxHeaders, MaxConnsPerHost: guardMaxActive,
	}
	g.server = &http.Server{
		Handler: g, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		ErrorLog:     log.New(io.Discard, "", 0),
		WriteTimeout: 30 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: guardMaxHeaders,
		BaseContext: func(net.Listener) context.Context { return g.ctx },
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				if !g.addConnection(conn) {
					_ = conn.Close()
				}
			case http.StateClosed:
				g.removeConnection(conn)
			}
		},
	}
	go func() { _ = g.server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		_ = g.Close()
	}()
	return g, nil
}

func parseGuardUpstream(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid explicit browser proxy")
	}
	var defaultPort string
	switch u.Scheme {
	case "http":
		defaultPort = "80"
	case "https":
		defaultPort = "443"
	case "socks5", "socks5h":
		defaultPort = "1080"
		u.Scheme = "socks5" // Browser egress always sends a validated, pinned public IP.
	default:
		return nil, errors.New("unsupported explicit browser proxy scheme")
	}
	port := u.Port()
	if port == "" {
		port = defaultPort
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, errors.New("invalid explicit browser proxy port")
	}
	u.Host = net.JoinHostPort(u.Hostname(), port)
	return u, nil
}

func (g *guardProxy) URL() string { return "http://" + g.listener.Addr().String() }

func (g *guardProxy) Credentials() (string, string) { return g.username, g.password }

func (g *guardProxy) Close() error {
	g.closeOnce.Do(func() {
		g.cancel()
		g.mu.Lock()
		g.closed = true
		conns := make([]net.Conn, 0, len(g.conns))
		for conn := range g.conns {
			conns = append(conns, conn)
		}
		g.mu.Unlock()
		g.closeErr = g.server.Close()
		g.transport.CloseIdleConnections()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	return g.closeErr
}

func (g *guardProxy) addConnection(conn net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || len(g.conns) >= guardMaxConnections {
		return false
	}
	g.conns[conn] = struct{}{}
	return true
}

func (g *guardProxy) removeConnection(conn net.Conn) {
	g.mu.Lock()
	delete(g.conns, conn)
	g.mu.Unlock()
}

type guardTrackedConn struct {
	net.Conn
	guard *guardProxy
	once  sync.Once
	err   error
}

func (conn *guardTrackedConn) Close() error {
	conn.once.Do(func() {
		conn.err = conn.Conn.Close()
		conn.guard.removeConnection(conn)
	})
	return conn.err
}

func (g *guardProxy) openConnection(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := g.opts.dialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	tracked := &guardTrackedConn{Conn: conn, guard: g}
	if !g.addConnection(tracked) {
		_ = conn.Close()
		return nil, errors.New("browser proxy connection limit or closed session")
	}
	return tracked, nil
}

func (g *guardProxy) pinnedAddress(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") || host == "" {
		return "", errGuardDestinationBlocked
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.ContainsAny(host, "%/\\ \t\r\n") {
		return "", errGuardDestinationBlocked
	}
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		addresses, err = g.opts.lookupIP(ctx, "ip", host)
		if err != nil {
			return "", fmt.Errorf("resolve browser destination: %w", err)
		}
	}
	if len(addresses) == 0 {
		return "", errGuardDestinationBlocked
	}
	for _, address := range addresses {
		if !guardPublicAddress(address) {
			return "", errGuardDestinationBlocked
		}
	}
	return net.JoinHostPort(addresses[0].Unmap().String(), port), nil
}

func (g *guardProxy) dialDestination(ctx context.Context, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, guardDialTimeout)
	stop := context.AfterFunc(g.ctx, cancel)
	defer stop()
	defer cancel()
	if err := g.ctx.Err(); err != nil {
		return nil, err
	}
	pinned, err := g.pinnedAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	if g.upstream == nil {
		return g.openConnection(ctx, "tcp", pinned)
	}
	if g.upstream.Scheme == "socks5" {
		var auth *proxy.Auth
		if g.upstream.User != nil {
			password, _ := g.upstream.User.Password()
			auth = &proxy.Auth{User: g.upstream.User.Username(), Password: password}
		}
		dialer, err := proxy.SOCKS5("tcp", g.upstream.Host, auth, guardForwardDialer{guard: g})
		if err != nil {
			return nil, err
		}
		return dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", pinned)
	}
	return g.dialHTTPProxy(ctx, pinned)
}

type guardForwardDialer struct{ guard *guardProxy }

func (d guardForwardDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(d.guard.ctx, network, address)
}

func (d guardForwardDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.guard.openConnection(ctx, network, address)
}

func (g *guardProxy) dialHTTPProxy(ctx context.Context, pinned string) (conn net.Conn, err error) {
	conn, err = g.openConnection(ctx, "tcp", g.upstream.Host)
	if err != nil {
		return nil, err
	}
	raw := conn
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer func() {
		stop()
		if err != nil {
			_ = raw.Close()
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if g.upstream.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: g.upstream.Hostname(), MinVersion: tls.VersionTLS12})
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tlsConn
	}
	r := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: pinned}, Host: pinned, Header: make(http.Header)}
	if g.upstream.User != nil {
		password, _ := g.upstream.User.Password()
		r.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(g.upstream.User.Username()+":"+password)))
	}
	if err = r.Write(conn); err != nil {
		return nil, err
	}
	// Bound CONNECT response headers without leaving a size limit on the tunnel.
	headerReader := &guardHeaderReader{reader: conn, remaining: guardMaxHeaders}
	reader := bufio.NewReader(headerReader)
	response, err := http.ReadResponse(reader, r)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("explicit browser proxy rejected CONNECT")
	}
	headerReader.unlimited = true
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return &guardBufferedConn{Conn: conn, reader: reader}, nil
}

type guardHeaderReader struct {
	reader    io.Reader
	remaining int
	unlimited bool
}

func (r *guardHeaderReader) Read(p []byte) (int, error) {
	if !r.unlimited {
		if r.remaining <= 0 {
			return 0, errors.New("explicit browser proxy response headers too large")
		}
		if len(p) > r.remaining {
			p = p[:r.remaining]
		}
	}
	n, err := r.reader.Read(p)
	r.remaining -= n
	return n, err
}

type guardBufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (conn *guardBufferedConn) Read(p []byte) (int, error) { return conn.reader.Read(p) }

func (g *guardProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	values := r.Header.Values("Proxy-Authorization")
	if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(g.auth)) != 1 {
		w.Header().Set("Proxy-Authenticate", `Basic realm="browser-session"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if g.ctx.Err() != nil {
		http.Error(w, "browser session closed", http.StatusServiceUnavailable)
		return
	}
	select {
	case g.active <- struct{}{}:
		defer func() { <-g.active }()
	default:
		http.Error(w, "browser proxy busy", http.StatusServiceUnavailable)
		return
	}
	if r.Header.Get("Upgrade") != "" || guardHeaderToken(r.Header, "Connection", "upgrade") {
		http.Error(w, "protocol upgrade blocked", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		g.serveConnect(w, r)
		return
	}
	g.serveHTTP(w, r)
}

func guardHeaderToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), token) {
				return true
			}
		}
	}
	return false
}

func guardStripHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, item := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(item))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

func (g *guardProxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if r.URL == nil || (r.URL.Scheme != "http" && r.URL.Scheme != "https") || r.URL.Hostname() == "" || r.URL.User != nil || r.URL.Opaque != "" || r.URL.Fragment != "" {
		http.Error(w, "browser destination blocked", http.StatusForbidden)
		return
	}
	port := r.URL.Port()
	if port != "" && port != "80" && port != "443" {
		http.Error(w, "browser destination blocked", http.StatusForbidden)
		return
	}
	if r.ContentLength > guardMaxRequestBody {
		http.Error(w, "browser request too large", http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, guardMaxRequestBody+1))
	if err != nil || len(body) > guardMaxRequestBody {
		http.Error(w, "browser request too large", http.StatusRequestEntityTooLarge)
		return
	}
	out := r.Clone(ctx)
	out.RequestURI = ""
	out.Host = out.URL.Host
	out.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		out.Body = http.NoBody
	}
	out.ContentLength = int64(len(body))
	out.TransferEncoding = nil
	out.Trailer = nil
	guardStripHopHeaders(out.Header)
	response, err := g.transport.RoundTrip(out)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errGuardDestinationBlocked) {
			status = http.StatusForbidden
		}
		http.Error(w, "browser destination unavailable", status)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusSwitchingProtocols || response.ContentLength > guardMaxResponseBody {
		http.Error(w, "browser response blocked", http.StatusBadGateway)
		return
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, guardMaxResponseBody+1))
	if err != nil || len(data) > guardMaxResponseBody {
		http.Error(w, "browser response blocked", http.StatusBadGateway)
		return
	}
	guardStripHopHeaders(response.Header)
	for name, values := range response.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(data)
}

func (g *guardProxy) serveConnect(w http.ResponseWriter, r *http.Request) {
	_, port, err := net.SplitHostPort(r.Host)
	if err != nil || port != "443" || r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		http.Error(w, "browser tunnel blocked", http.StatusForbidden)
		return
	}
	upstream, err := g.dialDestination(r.Context(), r.Host)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errGuardDestinationBlocked) {
			status = http.StatusForbidden
		}
		http.Error(w, "browser destination unavailable", status)
		return
	}
	defer upstream.Close()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "browser tunnel unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func() {
		_ = client.Close()
		g.removeConnection(client)
	}()
	deadline, _ := g.ctx.Deadline()
	_ = client.SetDeadline(deadline)
	_ = upstream.SetDeadline(deadline)
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	// Peek across TCP fragments within five seconds. Established TLS tunnels
	// retain the job deadline and their per-direction byte budgets.
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	header, err := buffered.Peek(6)
	if err != nil || header[0] != 22 || header[1] != 3 || header[2] > 3 || header[5] != 1 {
		return
	}
	length := int(header[3])<<8 | int(header[4])
	if length < 4 || length > 18432 {
		return
	}
	_ = client.SetReadDeadline(deadline)
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.CopyN(upstream, buffered, guardMaxTunnelBytes)
		_ = upstream.Close()
		done <- struct{}{}
	}()
	_, _ = io.CopyN(client, upstream, guardMaxTunnelBytes)
	_ = client.Close()
	_ = upstream.Close()
	<-done
}
