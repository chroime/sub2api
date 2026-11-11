package service

import (
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func builtinEasyPayConfigError(message string) error {
	return infraerrors.BadRequest("BUILTIN_EPAY_INVALID_CONFIG", message)
}

// ValidateBuiltinEasyPayConfig validates the optional compatibility API settings
// separately from the Alipay app and fixed-QR credentials.
func ValidateBuiltinEasyPayConfig(cfg map[string]string) error {
	enabled := strings.TrimSpace(cfg["epayEnabled"])
	if enabled != "" && enabled != "true" && enabled != "false" {
		return builtinEasyPayConfigError("epayEnabled must be true or false")
	}
	key := cfg["epayKey"]
	if key != "" || enabled == "true" {
		if !validBuiltinEasyPayKey(key) {
			return builtinEasyPayConfigError("EasyPay key must contain 32 to 256 characters without whitespace or control characters")
		}
	}
	origins, err := parseBuiltinEasyPayOrigins(cfg["epayNotifyOrigins"])
	if err != nil {
		return err
	}
	if enabled == "true" && len(origins) == 0 {
		return builtinEasyPayConfigError("configure at least one exact callback origin before enabling the compatibility API")
	}
	return nil
}

func validBuiltinEasyPayKey(key string) bool {
	if !utf8.ValidString(key) || utf8.RuneCountInString(key) < 32 || utf8.RuneCountInString(key) > 256 {
		return false
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseBuiltinEasyPayOrigins(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 8192 {
		return nil, builtinEasyPayConfigError("callback origin configuration is too large")
	}
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil || values == nil || len(values) > 50 {
		return nil, builtinEasyPayConfigError("callback origins must be a JSON array of at most 50 exact origins")
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, rawOrigin := range values {
		u, origin, err := parseBuiltinEasyPayURL(rawOrigin)
		if err != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery {
			return nil, builtinEasyPayConfigError("callback entries must be exact HTTPS origins; HTTP is allowed only for explicit loopback development origins")
		}
		if !seen[origin] {
			result = append(result, origin)
			seen[origin] = true
		}
	}
	return result, nil
}

func parseBuiltinEasyPayURL(raw string) (*url.URL, string, error) {
	invalid := func() (*url.URL, string, error) {
		return nil, "", infraerrors.BadRequest("BUILTIN_EPAY_INVALID_URL", "invalid or unsupported callback URL")
	}
	if len(raw) == 0 || len(raw) > 2048 || strings.TrimSpace(raw) != raw || strings.Contains(raw, "#") {
		return invalid()
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Host == "" {
		return invalid()
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(host, ".") {
		return invalid()
	}
	loopback := builtinEasyPayLoopbackHost(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return invalid()
	}
	if ip := net.ParseIP(host); ip != nil {
		if !loopback && !builtinEasyPayPublicIP(ip) {
			return invalid()
		}
	} else {
		for _, ch := range host {
			if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-') {
				return invalid()
			}
		}
		if strings.HasSuffix(host, ".localhost") && host != "localhost" {
			return invalid()
		}
	}
	port := u.Port()
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return invalid()
		}
		port = strconv.Itoa(p)
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}
	u.Host = authority
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return invalid()
	}
	return u, u.Scheme + "://" + authority, nil
}

func validateBuiltinEasyPayURL(raw string, origins []string, required bool) (string, error) {
	if raw == "" && !required {
		return "", nil
	}
	u, origin, err := parseBuiltinEasyPayURL(raw)
	if err != nil {
		return "", err
	}
	for _, allowed := range origins {
		if origin == allowed {
			return u.String(), nil
		}
	}
	return "", infraerrors.BadRequest("BUILTIN_EPAY_INVALID_URL", "callback URL origin is not allowed for this merchant")
}

func builtinEasyPayLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func builtinEasyPayPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// Also exclude carrier-grade NAT, benchmark, and reserved ranges that
		// net.IP.IsPrivate intentionally does not classify as RFC1918.
		return v4[0] != 0 && !(v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127) &&
			!(v4[0] == 198 && (v4[1] == 18 || v4[1] == 19)) && v4[0] < 224
	}
	return true
}
