package proxyutil

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func acceptForwardSOCKS(conn net.Conn, user, password, target string) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 5 {
		return fmt.Errorf("expected SOCKS5 greeting")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	method := byte(0)
	if user != "" {
		method = 2
	}
	if _, err := conn.Write([]byte{5, method}); err != nil {
		return err
	}
	if method == 2 {
		if _, err := io.ReadFull(conn, header); err != nil {
			return err
		}
		u := make([]byte, int(header[1]))
		if _, err := io.ReadFull(conn, u); err != nil {
			return err
		}
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		p := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, p); err != nil {
			return err
		}
		if string(u) != user || string(p) != password {
			return fmt.Errorf("wrong proxy credentials")
		}
		if _, err := conn.Write([]byte{1, 0}); err != nil {
			return err
		}
	}
	command := make([]byte, 4)
	if _, err := io.ReadFull(conn, command); err != nil {
		return err
	}
	var host string
	switch command[3] {
	case 1:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return err
		}
		host = net.IP(ip).String()
	case 3:
		size := make([]byte, 1)
		if _, err := io.ReadFull(conn, size); err != nil {
			return err
		}
		name := make([]byte, int(size[0]))
		if _, err := io.ReadFull(conn, name); err != nil {
			return err
		}
		host = string(name)
	default:
		return fmt.Errorf("unsupported address type %d", command[3])
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return err
	}
	actual := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(port)))
	if actual != target {
		return fmt.Errorf("expected target %s, got %s", target, actual)
	}
	_, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
	return err
}

func TestConfigureTransportProxyChainsThroughExplicitForwardProxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	result := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			result <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if err := acceptForwardSOCKS(conn, "", "", "127.0.0.1:1"); err != nil {
			result <- err
			return
		}
		if err := acceptForwardSOCKS(conn, "fixture-user", "fixture-password", "target.example:80"); err != nil {
			result <- err
			return
		}
		request := make([]byte, 512)
		n, err := conn.Read(request)
		if err != nil {
			result <- err
			return
		}
		if !strings.Contains(string(request[:n]), "GET /probe HTTP/1.1") {
			result <- fmt.Errorf("target request was not forwarded")
			return
		}
		_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 20\r\nConnection: close\r\n\r\nthrough-target-proxy")
		result <- err
	}()
	t.Setenv("SUB2API_PROXY_FORWARD_URL", "socks5h://"+listener.Addr().String())
	upstream, err := url.Parse("socks5://fixture-user:fixture-password@127.0.0.1:1")
	require.NoError(t, err)
	transport := &http.Transport{}
	require.NoError(t, ConfigureTransportProxy(transport, upstream))
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	resp, err := client.Get("http://target.example/probe")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "through-target-proxy", string(body))
	require.NoError(t, <-result)
}

func TestConfigureTransportProxyRejectsInvalidForwardProxy(t *testing.T) {
	t.Setenv("SUB2API_PROXY_FORWARD_URL", "not-a-valid-forward-proxy")
	upstream, err := url.Parse("socks5://127.0.0.1:1")
	require.NoError(t, err)
	require.Error(t, ConfigureTransportProxy(&http.Transport{}, upstream))
}

func TestConfigureTransportProxyForwardsHTTPProxyConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	result := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if err := acceptForwardSOCKS(conn, "", "", "127.0.0.1:1"); err != nil {
			result <- err
			return
		}
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			result <- err
			return
		}
		request := string(buf[:n])
		if !strings.Contains(request, "GET http://target.example/probe HTTP/1.1") || !strings.Contains(request, "Proxy-Authorization: Basic ") {
			result <- fmt.Errorf("HTTP proxy request or credentials bypassed")
			return
		}
		_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 20\r\nConnection: close\r\n\r\nthrough-target-proxy")
		result <- err
	}()
	t.Setenv("SUB2API_PROXY_FORWARD_URL", "socks5h://"+listener.Addr().String())
	upstream, err := url.Parse("http://fixture-user:fixture-password@127.0.0.1:1")
	require.NoError(t, err)
	transport := &http.Transport{}
	require.NoError(t, ConfigureTransportProxy(transport, upstream))
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	resp, err := client.Get("http://target.example/probe")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "through-target-proxy", string(body))
	require.NoError(t, <-result)
}

func TestNewProxyForwardDialerRejectsInvalidConfigurationWithoutCredentials(t *testing.T) {
	for _, value := range []string{
		"socks5://fixture-user:fixture-secret@",
		"http://fixture-user:fixture-secret@127.0.0.1:7897",
		"socks5://127.0.0.1",
	} {
		t.Setenv(ForwardProxyEnv, value)
		_, err := NewProxyForwardDialer()
		require.Error(t, err)
		require.NotContains(t, err.Error(), "fixture-user")
		require.NotContains(t, err.Error(), "fixture-secret")
	}
}

func TestConfigureTransportProxyDoesNotForwardDirectRequests(t *testing.T) {
	t.Setenv("SUB2API_PROXY_FORWARD_URL", "not-a-valid-forward-proxy")
	transport := &http.Transport{}
	require.NoError(t, ConfigureTransportProxy(transport, nil))
	require.Nil(t, transport.DialContext)
}

func TestConfigureTransportProxyForwardFailureDoesNotBypassTargetProxy(t *testing.T) {
	t.Setenv("SUB2API_PROXY_FORWARD_URL", "socks5h://127.0.0.1:1")
	upstream, err := url.Parse("socks5://target-proxy.example:1080")
	require.NoError(t, err)
	transport := &http.Transport{}
	require.NoError(t, ConfigureTransportProxy(transport, upstream))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := transport.DialContext(ctx, "tcp", "destination.example:443")
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err)
	require.ErrorContains(t, err, "127.0.0.1:1")
}
