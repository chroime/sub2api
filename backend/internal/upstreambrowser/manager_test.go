package upstreambrowser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInputAndActionBounds(t *testing.T) {
	valid := Input{BaseURL: "https://upstream.example", Platform: "sub2api"}
	if !validInput(valid) {
		t.Fatal("valid origin rejected")
	}
	for _, base := range []string{"http://upstream.example", "https://upstream.example:8443", "https://upstream.example/path", "https://user:pass@upstream.example", "https://upstream.example?q=1"} {
		input := valid
		input.BaseURL = base
		if validInput(input) {
			t.Fatalf("unsafe origin accepted: %s", base)
		}
	}
	for _, action := range []Action{{Type: "pointer_down", X: -1}, {Type: "pointer_move", X: Width}, {Type: "pointer_move", X: math.NaN()}, {Type: "key", Key: "Alt+F4"}, {Type: "text", Text: strings.Repeat("x", 2049)}, {Type: "navigate"}, {Type: "wheel", DeltaY: 2001}} {
		if validAction(action) {
			t.Fatalf("unsafe action accepted: %#v", action)
		}
	}
	for _, action := range []Action{{Type: "pointer_down", X: 10, Y: 20}, {Type: "pointer_up", X: 10, Y: 20}, {Type: "text", Text: "123456"}, {Type: "key", Key: "Enter"}, {Type: "wheel", DeltaY: 300}} {
		if !validAction(action) {
			t.Fatalf("valid action rejected: %#v", action)
		}
	}
}

func TestManagerUnavailableIsTokenFree(t *testing.T) {
	m := New(Options{NodePath: "missing-node-secret", ScriptPath: "missing-script-secret", ExecutablePath: "missing-browser-secret"})
	if ok, code := m.Available(); ok || strings.Contains(code, "secret") {
		t.Fatalf("unexpected availability %v %q", ok, code)
	}
	_, err := m.Start(context.Background(), Input{BaseURL: "https://upstream.example", Platform: "sub2api", Password: "password-secret"})
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error %v", err)
	}
}

func TestManagerStartPreservesBoundedAvailabilityCode(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing-secret-canary")
	for _, test := range []struct {
		name    string
		options Options
		want    string
	}{
		{"node", Options{NodePath: missing, ScriptPath: executable, ExecutablePath: executable}, "browser_node_missing"},
		{"script", Options{NodePath: executable, ScriptPath: missing, ExecutablePath: executable}, "browser_script_missing"},
		{"executable", Options{NodePath: executable, ScriptPath: executable, ExecutablePath: missing}, "browser_executable_missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.options).Start(t.Context(), Input{BaseURL: "https://upstream.example", Platform: "sub2api"})
			if !errors.Is(err, ErrUnavailable) || Code(err) != test.want {
				t.Fatalf("Start error = %v, code = %q, want unavailable with %q", err, Code(err), test.want)
			}
			if strings.Contains(err.Error(), "canary") {
				t.Fatal("availability error exposed a configured path")
			}
		})
	}
}

func TestBrowserChildEnvironmentOnlyForwardsLinuxDisplayConfiguration(t *testing.T) {
	display := map[string]string{
		"DISPLAY": ":77", "WAYLAND_DISPLAY": "wayland-7", "XDG_RUNTIME_DIR": "/run/user/1007", "XAUTHORITY": "/tmp/display-auth",
	}
	blocked := []string{"NODE_OPTIONS", "NODE_DEBUG", "DEBUG", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "PLAYWRIGHT_BROWSERS_PATH"}
	for key, value := range display {
		t.Setenv(key, value)
	}
	for _, key := range blocked {
		t.Setenv(key, "blocked-secret-canary")
	}
	env := map[string]string{}
	for _, entry := range childEnvironment(t.TempDir()) {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	for key, want := range display {
		got, forwarded := env[key]
		if runtime.GOOS == "linux" {
			if !forwarded || got != want {
				t.Errorf("Linux display %s = %q (forwarded %v), want %q", key, got, forwarded, want)
			}
		} else if forwarded {
			t.Errorf("display setting %s forwarded on %s", key, runtime.GOOS)
		}
	}
	for _, key := range blocked {
		if _, forwarded := env[key]; forwarded {
			t.Errorf("unsafe environment %s was forwarded", key)
		}
	}
}

func TestBrowserChildEnvironmentHeadlessOptIn(t *testing.T) {
	t.Setenv("GOVERNANCE_BROWSER_HEADLESS", "")
	requireEnv := func(want bool) {
		t.Helper()
		found := false
		for _, entry := range childEnvironment(t.TempDir()) {
			if entry == "GOVERNANCE_BROWSER_HEADLESS=true" {
				found = true
			}
		}
		if found != want {
			t.Fatalf("headless setting forwarded = %v, want %v", found, want)
		}
	}
	requireEnv(false)
	t.Setenv("GOVERNANCE_BROWSER_HEADLESS", "false")
	requireEnv(false)
	t.Setenv("GOVERNANCE_BROWSER_HEADLESS", "true")
	requireEnv(true)
}

func TestSnapshotRejectsNonImageAndUnexpectedFields(t *testing.T) {
	for _, raw := range []string{
		`{"status":"waiting","width":1024,"height":720,"image":"access-token"}`,
		`{"status":"waiting","width":1024,"height":720,"access_token":"secret"}`,
		`{"status":"https://secret.example","width":1024,"height":720}`,
		`{"status":"waiting","width":9999,"height":720}`,
		`{"status":"failed","width":1024,"height":720,"error_code":"secret-token"}`,
	} {
		if _, err := decodeView(json.RawMessage(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("unsafe view accepted: %s", raw)
		}
	}
	if _, err := decodeView(json.RawMessage(`{"status":"ready","width":1024,"height":720}`)); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotPreservesBoundedUnsupportedRouteDiagnostic(t *testing.T) {
	view, err := decodeView(json.RawMessage(`{"status":"failed","width":1024,"height":720,"error_code":"browser_unsupported_route"}`))
	if err != nil || view.Status != "failed" || view.ErrorCode != "browser_unsupported_route" {
		t.Fatalf("unsupported route diagnostic = %#v, %v", view, err)
	}
}

func TestSnapshotDropsTrailingPayloadFromJPEG(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, Width, Height)), nil); err != nil {
		t.Fatal(err)
	}
	encoded.WriteString("secret-access-token")
	raw, _ := json.Marshal(View{Status: "waiting", Width: Width, Height: Height, Image: base64.StdEncoding.EncodeToString(encoded.Bytes())})
	view, err := decodeView(raw)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(view.Image)
	if err != nil || bytes.Contains(decoded, []byte("secret-access-token")) {
		t.Fatal("snapshot retained a non-image payload")
	}
}

func TestCaptureRejectsMalformedAndUnboundedSecrets(t *testing.T) {
	now := time.Now().UTC()
	valid := Capture{AccessToken: "access", AuthVariant: "bearer", UserAgent: "Actual browser", IssuedAt: now}
	if !validCapture(&valid) {
		t.Fatal("bearer-only candidate rejected")
	}
	expires := now.Add(time.Hour)
	knownExpiry := valid
	knownExpiry.ExpiresIn, knownExpiry.ExpiresAt = 3600, &expires
	if !validCapture(&knownExpiry) {
		t.Fatal("known bearer expiry without refresh rejected")
	}
	for _, bad := range []Capture{
		{AccessToken: strings.Repeat("x", 16385), AuthVariant: "bearer", UserAgent: "UA", IssuedAt: now},
		{AccessToken: "access", RefreshToken: "refresh", AuthVariant: "bearer", UserAgent: "UA", IssuedAt: now},
		{Cookies: map[string]string{"session": "cookie", "forbidden": "secret"}, AuthVariant: "newapi_legacy_cookie", UserID: 4, UserAgent: "UA", IssuedAt: now},
		{AccessToken: "access", AuthVariant: "bearer", UserAgent: "UA\nHeader: injected", IssuedAt: now},
	} {
		if validCapture(&bad) {
			t.Fatal("invalid capture accepted")
		}
	}
}

func fixtureManager(t *testing.T) *Manager {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node runtime unavailable")
	}
	script, err := filepath.Abs("testdata/rpc.mjs")
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{NodePath: node, ScriptPath: script, ExecutablePath: node, TempDir: t.TempDir()})
}

func TestDriverStartPreservesOnlyAllowlistedHelperErrors(t *testing.T) {
	for _, test := range []struct {
		origin      string
		want        string
		unavailable bool
	}{
		{"https://launch-failed.example", "browser_launch_failed", true},
		{"https://dependency-missing.example", "browser_dependency_missing", true},
		{"https://raw-error.example", "browser_protocol_error", false},
	} {
		t.Run(test.want, func(t *testing.T) {
			m := fixtureManager(t)
			driver, err := m.Start(t.Context(), Input{BaseURL: test.origin, Platform: "sub2api"})
			if driver != nil {
				_ = driver.Close()
				t.Fatal("failed helper returned a driver")
			}
			if Code(err) != test.want || errors.Is(err, ErrUnavailable) != test.unavailable {
				t.Fatalf("helper error = %v, code = %q, want %q", err, Code(err), test.want)
			}
			if strings.Contains(err.Error(), "canary") {
				t.Fatal("helper error exposed raw subprocess content")
			}
			entries, readErr := os.ReadDir(m.options.TempDir)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("failed launch profile remains: %v %v", entries, readErr)
			}
		})
	}
}

func TestDriverActionPreservesOnlyBoundedUnsupportedRouteError(t *testing.T) {
	for _, test := range []struct{ origin, want string }{
		{"https://unsupported-route.example", "browser_unsupported_route"},
		{"https://raw-action-error.example", "browser_protocol_error"},
	} {
		t.Run(test.want, func(t *testing.T) {
			driver, err := fixtureManager(t).Start(t.Context(), Input{BaseURL: test.origin, Platform: "sub2api"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = driver.Close() })
			err = driver.Action(t.Context(), Action{Type: "key", Key: "Enter"})
			if Code(err) != test.want || errors.Is(err, ErrUnavailable) {
				t.Fatalf("action error = %v, code = %q, want %q without unavailable classification", err, Code(err), test.want)
			}
			if strings.Contains(err.Error(), "canary") {
				t.Fatal("action error exposed raw helper content")
			}
		})
	}
}

func TestDriverCancellationClosesRPCAndRemovesProfile(t *testing.T) {
	m := fixtureManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	driver, err := m.Start(ctx, Input{BaseURL: "https://upstream.example", Platform: "sub2api"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := driver.Snapshot(context.Background())
	if err != nil || view.Status != "waiting" {
		t.Fatalf("snapshot: %#v %v", view, err)
	}
	if err := driver.Action(context.Background(), Action{Type: "key", Key: "Alt+F4"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad action accepted: %v", err)
	}
	cancel()
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Snapshot(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed driver accepted command: %v", err)
	}
	entries, err := os.ReadDir(m.options.TempDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("ephemeral profile remains: %v %v", entries, err)
	}
}

func TestDriverMalformedResponseClosesChild(t *testing.T) {
	m := fixtureManager(t)
	driver, err := m.Start(context.Background(), Input{BaseURL: "https://malformed.example", Platform: "sub2api"})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if _, err := driver.Snapshot(context.Background()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("malformed output accepted: %v", err)
	}
}

func TestDriverRPCCancellationDoesNotHang(t *testing.T) {
	m := fixtureManager(t)
	driver, err := m.Start(context.Background(), Input{BaseURL: "https://blocked.example", Platform: "sub2api"})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = driver.Snapshot(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("RPC cancellation hung")
	}
}

func TestOptionalChromiumLifetimeWithBlockedOrigin(t *testing.T) {
	executable := os.Getenv("GOVERNANCE_BROWSER_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("explicit test browser executable not configured")
	}
	temp, err := os.MkdirTemp(os.Getenv("GOVERNANCE_BROWSER_TEST_TEMP_DIR"), "go-browser-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	script, err := filepath.Abs("../../../tools/upstream-browser/main.mjs")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{ScriptPath: script, ExecutablePath: executable, TempDir: temp})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := m.Start(ctx, Input{BaseURL: "https://127.0.0.1", Platform: "sub2api"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	view, err := driver.Snapshot(ctx)
	if err != nil || (view.Status != "failed" && view.Status != "waiting") {
		t.Fatalf("unexpected blocked origin state: %#v %v", view, err)
	}
	if capture, err := driver.Result(ctx); err != nil || capture != nil {
		t.Fatalf("blocked origin produced a candidate: %#v %v", capture, err)
	}
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("real browser profile was retained: %v %v", entries, err)
	}
}
