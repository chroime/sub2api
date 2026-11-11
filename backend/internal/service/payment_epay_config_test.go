//go:build unit

package service

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinEasyPayConfigRequiresSeparateSecretAndExactOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, key, origins string
		enabled            bool
		ok                 bool
	}{
		{"disabled draft", "", "", false, true},
		{"https", "0123456789abcdef0123456789abcdef", `["https://merchant.example"]`, true, true},
		{"loopback dev", "0123456789abcdef0123456789abcdef", `["http://127.0.0.1:8080"]`, true, true},
		{"short key", "short", `["https://merchant.example"]`, true, false},
		{"empty origins", "0123456789abcdef0123456789abcdef", `[]`, true, false},
		{"public http", "0123456789abcdef0123456789abcdef", `["http://merchant.example"]`, true, false},
		{"wildcard", "0123456789abcdef0123456789abcdef", `["https://*.merchant.example"]`, true, false},
		{"path", "0123456789abcdef0123456789abcdef", `["https://merchant.example/callback"]`, true, false},
		{"userinfo", "0123456789abcdef0123456789abcdef", `["https://key@merchant.example"]`, true, false},
		{"private address", "0123456789abcdef0123456789abcdef", `["https://192.168.1.1"]`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]string{"epayKey": tc.key, "epayNotifyOrigins": tc.origins}
			if tc.enabled {
				cfg["epayEnabled"] = "true"
			}
			err := ValidateBuiltinEasyPayConfig(cfg)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBuiltinEasyPayCallbackURLsMustStayInsideConfiguredOrigin(t *testing.T) {
	origins := []string{"https://merchant.example", "http://127.0.0.1:8080"}
	for _, raw := range []string{"https://merchant.example/notify", "https://merchant.example:443/notify", "http://127.0.0.1:8080/callback"} {
		_, err := validateBuiltinEasyPayURL(raw, origins, true)
		require.NoError(t, err, raw)
	}
	for _, raw := range []string{"https://merchant.example.evil.test/notify", "https://merchant.example:8443/notify", "https://secret@merchant.example/notify", "https://merchant.example/notify#fragment", "https://127.0.0.1/notify", "http://127.0.0.1:9090/notify"} {
		_, err := validateBuiltinEasyPayURL(raw, origins, true)
		require.Error(t, err, raw)
	}
}

func TestBuiltinEasyPayDialRejectsPrivateResolvedAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "fc00::1", "0.0.0.0", "100.64.0.1", "224.0.0.1"} {
		require.False(t, builtinEasyPayPublicIP(net.ParseIP(ip)), ip)
	}
	require.True(t, builtinEasyPayPublicIP(net.ParseIP("8.8.8.8")))
}
