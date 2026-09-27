package upstreambrowser

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/jpeg"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const maxRPCLine = 4 << 20

type Manager struct{ options Options }

func New(options Options) *Manager {
	if options.NodePath == "" {
		options.NodePath = "node"
	}
	return &Manager{options: options}
}

func (m *Manager) Available() (bool, string) {
	if m == nil {
		return false, "browser_unavailable"
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return false, "browser_unavailable"
	}
	if _, err := exec.LookPath(m.options.NodePath); err != nil {
		return false, "browser_node_missing"
	}
	for _, entry := range []struct{ path, code string }{
		{m.options.ScriptPath, "browser_script_missing"},
		{m.options.ExecutablePath, "browser_executable_missing"},
	} {
		info, err := os.Stat(entry.path)
		if entry.path == "" || err != nil || !info.Mode().IsRegular() {
			return false, entry.code
		}
	}
	return true, ""
}

type rpcResponse struct {
	ID     uint64          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type rpcFrame struct {
	response rpcResponse
	err      error
}

type processDriver struct {
	ctx            context.Context
	cancel         context.CancelFunc
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	proxy          *guardProxy
	profile        string
	killTree       func()
	responses      chan rpcFrame
	rpcGate        chan struct{}
	procDone       chan struct{}
	closing        chan struct{}
	closeOnce      sync.Once
	closeErr       error
	sequence       uint64
	browserCleanup atomic.Pointer[func()]
}

func (m *Manager) Start(ctx context.Context, input Input) (Driver, error) {
	if !validInput(input) {
		return nil, ErrInvalid
	}
	if ok, _ := m.Available(); !ok {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	proxy, err := startGuardProxy(jobCtx, input.ProxyURL)
	if err != nil {
		cancel()
		return nil, ErrUnavailable
	}
	tempDir := m.options.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	tempDir, err = filepath.Abs(tempDir)
	if err != nil {
		cancel()
		proxy.Close()
		return nil, ErrUnavailable
	}
	profile, err := os.MkdirTemp(tempDir, "upstream-browser-")
	if err != nil {
		cancel()
		proxy.Close()
		return nil, ErrUnavailable
	}
	executable, err := filepath.Abs(m.options.ExecutablePath)
	if err != nil {
		cancel()
		proxy.Close()
		os.RemoveAll(profile)
		return nil, ErrUnavailable
	}
	script, err := filepath.Abs(m.options.ScriptPath)
	if err != nil {
		cancel()
		proxy.Close()
		os.RemoveAll(profile)
		return nil, ErrUnavailable
	}
	cmd := exec.Command(m.options.NodePath, script)
	cmd.Dir = filepath.Dir(script)
	cmd.Env = childEnvironment(profile)
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		proxy.Close()
		os.RemoveAll(profile)
		return nil, ErrUnavailable
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		proxy.Close()
		os.RemoveAll(profile)
		return nil, ErrUnavailable
	}
	killTree, err := startProcess(cmd)
	if err != nil {
		stdin.Close()
		stdout.Close()
		cancel()
		proxy.Close()
		os.RemoveAll(profile)
		return nil, ErrUnavailable
	}
	d := &processDriver{ctx: jobCtx, cancel: cancel, cmd: cmd, stdin: stdin, proxy: proxy, profile: profile,
		killTree: killTree, responses: make(chan rpcFrame, 1), rpcGate: make(chan struct{}, 1),
		procDone: make(chan struct{}), closing: make(chan struct{})}
	d.rpcGate <- struct{}{}
	go d.readResponses(stdout)
	go func() {
		_ = cmd.Wait()
		close(d.procDone)
		cancel()
	}()
	go func() { <-jobCtx.Done(); _ = d.Close() }()
	username, password := proxy.Credentials()
	params := struct {
		Input
		ProxyServer    string `json:"proxy_server"`
		ProxyUsername  string `json:"proxy_username"`
		ProxyPassword  string `json:"proxy_password"`
		ExecutablePath string `json:"executable_path"`
		ProfileDir     string `json:"profile_dir"`
	}{input, proxy.URL(), username, password, executable, profile}
	startCtx, stopStart := context.WithTimeout(jobCtx, 45*time.Second)
	defer stopStart()
	raw, err := d.rpc(startCtx, "start", params)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	var started struct {
		BrowserPID int `json:"browser_pid"`
	}
	if decodeStrict(raw, &started) != nil || started.BrowserPID <= 0 {
		_ = d.Close()
		return nil, ErrProtocol
	}
	cleanup, err := browserProcessCleanup(cmd.Process, started.BrowserPID)
	if err != nil {
		_ = d.Close()
		return nil, ErrUnavailable
	}
	d.browserCleanup.Store(&cleanup)
	select {
	case <-d.closing:
		cleanup()
		return nil, ErrClosed
	default:
	}
	return d, nil
}

// Do not inherit NODE_OPTIONS, debugger logging, browser overrides or proxy
// environment variables. Only the guard proxy controls browser egress.
func childEnvironment(temp string) []string {
	var result []string
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "LANG", "LC_ALL"} {
		if value, ok := os.LookupEnv(key); ok {
			result = append(result, key+"="+value)
		}
	}
	return append(result, "TMP="+temp, "TEMP="+temp, "TMPDIR="+temp)
}

func (d *processDriver) readResponses(stdout io.Reader) {
	defer close(d.responses)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxRPCLine)
	for scanner.Scan() {
		var response rpcResponse
		err := decodeStrict(scanner.Bytes(), &response)
		if err != nil {
			err = ErrProtocol
		}
		select {
		case d.responses <- rpcFrame{response, err}:
		case <-d.closing:
			return
		}
		if err != nil {
			return
		}
	}
	if scanner.Err() != nil {
		select {
		case d.responses <- rpcFrame{err: ErrProtocol}:
		case <-d.closing:
		}
	}
}

func (d *processDriver) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	select {
	case <-d.closing:
		return nil, ErrClosed
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.ctx.Done():
		return nil, ErrClosed
	case <-d.rpcGate:
	}
	defer func() { d.rpcGate <- struct{}{} }()
	d.sequence++
	data, err := json.Marshal(struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{d.sequence, method, params})
	if err != nil || len(data) > 32<<10 {
		return nil, ErrInvalid
	}
	written := make(chan error, 1)
	go func() { _, err := d.stdin.Write(append(data, '\n')); written <- err }()
	select {
	case <-ctx.Done():
		go d.Close()
		return nil, ctx.Err()
	case <-d.ctx.Done():
		return nil, ErrClosed
	case err := <-written:
		if err != nil {
			go d.Close()
			return nil, ErrClosed
		}
	}
	select {
	case <-ctx.Done():
		go d.Close()
		return nil, ctx.Err()
	case frame, ok := <-d.responses:
		if !ok || frame.err != nil || frame.response.ID != d.sequence {
			go d.Close()
			return nil, ErrProtocol
		}
		response := frame.response
		if !response.OK || response.Error != "" {
			go d.Close()
			if response.Error == "browser_launch_failed" || response.Error == "browser_dependency_missing" {
				return nil, ErrUnavailable
			}
			return nil, ErrProtocol
		}
		return response.Result, nil
	case <-d.ctx.Done():
		return nil, ErrClosed
	}
}

func (d *processDriver) Snapshot(ctx context.Context) (View, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := d.rpc(ctx, "snapshot", nil)
	if err != nil {
		return View{}, err
	}
	view, err := decodeView(raw)
	if err != nil {
		go d.Close()
	}
	return view, err
}

func (d *processDriver) Action(ctx context.Context, action Action) error {
	if !validAction(action) {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := d.rpc(ctx, "action", action)
	return err
}

func (d *processDriver) Result(ctx context.Context) (*Capture, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := d.rpc(ctx, "result", nil)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var capture Capture
	if decodeStrict(raw, &capture) != nil || !validCapture(&capture) {
		go d.Close()
		return nil, ErrProtocol
	}
	return &capture, nil
}

func (d *processDriver) Close() error {
	d.closeOnce.Do(func() {
		close(d.closing)
		d.cancel()
		_ = d.proxy.Close()
		_ = d.stdin.Close()
		// Playwright owns a detached Chromium group on Linux. Let its normal EOF
		// cleanup run first, then kill the independently verified browser group.
		select {
		case <-d.procDone:
		case <-time.After(2 * time.Second):
		}
		if cleanup := d.browserCleanup.Load(); cleanup != nil {
			(*cleanup)()
		}
		d.killTree()
		<-d.procDone
		// Antivirus and Windows browser child teardown can briefly retain handles.
		d.closeErr = ErrCleanup
		for attempt := 0; attempt < 20; attempt++ {
			if os.RemoveAll(d.profile) == nil {
				d.closeErr = nil
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return d.closeErr
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrProtocol
	}
	return nil
}

func cleanString(value string, max int, allowSpace bool) bool {
	if len(value) == 0 || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 || (!allowSpace && c == 32) {
			return false
		}
	}
	return true
}

func validInput(input Input) bool {
	u, err := url.Parse(input.BaseURL)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Path == "" || u.Path == "/") && (u.Port() == "" || u.Port() == "443") &&
		(input.Platform == "sub2api" || input.Platform == "newapi") &&
		(input.Username == "" || cleanString(input.Username, 512, true)) && (input.Password == "" || cleanString(input.Password, 4096, true))
}

func validAction(action Action) bool {
	switch action.Type {
	case "pointer_down", "pointer_move", "pointer_up":
		return !math.IsNaN(action.X) && !math.IsInf(action.X, 0) && action.X >= 0 && action.X < Width &&
			!math.IsNaN(action.Y) && !math.IsInf(action.Y, 0) && action.Y >= 0 && action.Y < Height
	case "wheel":
		return !math.IsNaN(action.DeltaX) && !math.IsInf(action.DeltaX, 0) && math.Abs(action.DeltaX) <= 2000 &&
			!math.IsNaN(action.DeltaY) && !math.IsInf(action.DeltaY, 0) && math.Abs(action.DeltaY) <= 2000
	case "text":
		return cleanString(action.Text, 2048, true)
	case "key":
		switch action.Key {
		case "Enter", "Tab", "Backspace", "Delete", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Escape", "Home", "End", "PageUp", "PageDown", "Space", "Control+A", "Meta+A":
			return true
		}
	}
	return false
}

func decodeView(raw json.RawMessage) (View, error) {
	var view View
	if decodeStrict(raw, &view) != nil || view.Width != Width || view.Height != Height {
		return View{}, ErrProtocol
	}
	switch view.Status {
	case "starting", "waiting", "ready", "failed":
	default:
		return View{}, ErrProtocol
	}
	switch view.ErrorCode {
	case "", "browser_navigation_failed", "browser_render_failed", "browser_page_closed", "browser_timeout":
	default:
		return View{}, ErrProtocol
	}
	if view.Image != "" {
		if view.Status == "ready" || len(view.Image) > 3<<20 {
			return View{}, ErrProtocol
		}
		data, err := base64.StdEncoding.Strict().DecodeString(view.Image)
		if err != nil {
			return View{}, ErrProtocol
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != Width || config.Height != Height {
			return View{}, ErrProtocol
		}
		decoded, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return View{}, ErrProtocol
		}
		// Only pixels cross the client boundary, never JPEG metadata or trailing
		// bytes that a malformed subprocess response might contain.
		var clean bytes.Buffer
		if jpeg.Encode(&clean, decoded, &jpeg.Options{Quality: 65}) != nil {
			return View{}, ErrProtocol
		}
		view.Image = base64.StdEncoding.EncodeToString(clean.Bytes())
	}
	return view, nil
}

func validCapture(capture *Capture) bool {
	if capture == nil || !cleanString(capture.UserAgent, 1024, true) || capture.IssuedAt.IsZero() ||
		capture.IssuedAt.After(time.Now().Add(5*time.Second)) || capture.IssuedAt.Before(time.Now().Add(-11*time.Minute)) || capture.UserID < 0 {
		return false
	}
	for key, value := range capture.Cookies {
		if (key != "session" && key != "cf_clearance") || !cleanString(value, 8192, false) || strings.ContainsAny(value, ";,\"\\") {
			return false
		}
	}
	switch capture.AuthVariant {
	case "bearer":
		if !cleanString(capture.AccessToken, 16384, false) {
			return false
		}
	case "newapi_legacy_cookie":
		if capture.UserID <= 0 || capture.Cookies["session"] == "" || capture.AccessToken != "" || capture.RefreshToken != "" {
			return false
		}
	default:
		return false
	}
	if capture.RefreshToken != "" && !cleanString(capture.RefreshToken, 16384, false) {
		return false
	}
	if capture.ExpiresIn != 0 || capture.ExpiresAt != nil {
		if capture.ExpiresIn <= 0 || capture.ExpiresIn > 31536000 || capture.ExpiresAt == nil ||
			!capture.ExpiresAt.Equal(capture.IssuedAt.Add(time.Duration(capture.ExpiresIn)*time.Second)) {
			return false
		}
	} else if capture.RefreshToken != "" {
		return false
	}
	return capture.VerifiedLogin == nil || (cleanString(capture.VerifiedLogin.Username, 512, true) && cleanString(capture.VerifiedLogin.Password, 4096, true))
}
