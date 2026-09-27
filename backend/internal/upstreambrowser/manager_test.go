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
