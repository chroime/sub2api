package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/upstreambrowser"
	"github.com/google/uuid"
)

var ErrBrowserUnavailable = errors.New("browser authorization is unavailable")

type BrowserFactory interface {
	Available() (bool, string)
	Start(context.Context, upstreambrowser.Input) (upstreambrowser.Driver, error)
}

type BrowserProxyResolver func(context.Context, Site) (string, error)

type BrowserAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type BrowserAuthorizationInput struct {
	ExpectedSiteVersion int64  `json:"expected_site_version"`
	Username            string `json:"username"`
	Password            string `json:"password"`
}

type BrowserFrame struct {
	Image  string `json:"image"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type BrowserJob struct {
	ID        string        `json:"id"`
	SiteID    int64         `json:"site_id"`
	Status    string        `json:"status"`
	ExpiresAt time.Time     `json:"expires_at"`
	ErrorCode string        `json:"error_code,omitempty"`
	Frame     *BrowserFrame `json:"frame,omitempty"`
}

type browserJobState struct {
	mu         sync.Mutex
	ops        sync.Mutex
	view       BrowserJob
	actorID    int64
	site       Site
	proxy      string
	request    string
	ctx        context.Context
	cancel     context.CancelFunc
	timer      *time.Timer
	driver     upstreambrowser.Driver
	result     *ConnectResult
	completing bool
	launching  bool
	closing    bool
}

type BrowserAuthorizer struct {
	service *Service
	factory BrowserFactory
	proxy   BrowserProxyResolver
	mu      sync.Mutex
	jobs    map[string]*browserJobState
	closing bool
	ttl     time.Duration
	now     func() time.Time
}

func NewBrowserAuthorizer(service *Service, factory BrowserFactory, proxy BrowserProxyResolver) *BrowserAuthorizer {
	return &BrowserAuthorizer{service: service, factory: factory, proxy: proxy, jobs: map[string]*browserJobState{}, ttl: 10 * time.Minute, now: time.Now}
}

func (a *BrowserAuthorizer) Availability() BrowserAvailability {
	if a == nil || a.factory == nil {
		return BrowserAvailability{Reason: "browser_not_configured"}
	}
	available, reason := a.factory.Available()
	return BrowserAvailability{Available: available, Reason: reason}
}

func (s *Service) SetBrowserAuthorizer(a *BrowserAuthorizer) { s.browserAuthorizer = a }
func (s *Service) BrowserAuthorizer() *BrowserAuthorizer     { return s.browserAuthorizer }
func (s *Service) stopBrowserAuthorizations() {
	if s.browserAuthorizer != nil {
		s.browserAuthorizer.Stop()
	}
}

func browserActive(status string) bool {
	return status == "starting" || status == "waiting" || status == "ready"
}

func (j *browserJobState) snapshot() *BrowserJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	copy := j.view
	if copy.Frame != nil {
		frame := *copy.Frame
		copy.Frame = &frame
	}
	return &copy
}

func (j *browserJobState) occupiesSlot() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return browserActive(j.view.Status) || j.launching || j.closing
}

type browserProxyContextKey struct{}

// WithBrowserAuthorizationProxy pins native verification to the same configured
// proxy URL as the browser. It is an internal context value, never a JSON input.
func WithBrowserAuthorizationProxy(ctx context.Context, proxy string) context.Context {
	return context.WithValue(ctx, browserProxyContextKey{}, proxy)
}

func BrowserAuthorizationProxy(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(browserProxyContextKey{}).(string)
	return value, ok
}

func (a *BrowserAuthorizer) resolveProxy(ctx context.Context, site Site) (string, error) {
	if a.proxy != nil {
		return a.proxy(ctx, site)
	}
	if site.ProxyID != nil {
		return "", ErrInvalid
	}
	return "", nil
}

func (a *BrowserAuthorizer) Start(ctx context.Context, actorID, siteID int64, input BrowserAuthorizationInput) (*BrowserJob, error) {
	if actorID <= 0 || siteID <= 0 || input.ExpectedSiteVersion <= 0 {
		return nil, ErrInvalid
	}
	if !a.Availability().Available {
		return nil, ErrBrowserUnavailable
	}
	if a.service == nil || !a.service.durableKey || a.service.cipher == nil {
		return nil, ErrEncryption
	}
	login, err := normalizeLoginCredentials(LoginCredentials{Username: input.Username, Password: input.Password})
	if err != nil || login.Username == "" {
		return nil, ErrInvalid
	}
	site, release, err := a.service.remoteSiteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	if site.ID != siteID || site.Version != input.ExpectedSiteVersion {
		return nil, ErrConflict
	}
	if err = validateSite(site); err != nil {
		return nil, err
	}
	proxy, err := a.resolveProxy(ctx, *site)
	if err != nil {
		return nil, err
	}
	input.Username = login.Username
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		return nil, ErrBrowserUnavailable
	}
	active := 0
	for id, existing := range a.jobs {
		view := existing.snapshot()
		if existing.occupiesSlot() {
			active++
			if existing.site.ID == siteID {
				if browserActive(view.Status) && existing.actorID == actorID && existing.request == fingerprint && existing.proxy == proxy && existing.site.Version == site.Version {
					a.mu.Unlock()
					return view, nil
				}
				a.mu.Unlock()
				return nil, ErrBusy
			}
		} else if a.now().After(view.ExpiresAt) || len(a.jobs) >= 64 {
			delete(a.jobs, id)
		}
	}
	if active >= 2 || len(a.jobs) >= 64 {
		a.mu.Unlock()
		return nil, ErrBusy
	}
	expires := a.now().UTC().Add(a.ttl)
	jobCtx, cancel := context.WithDeadline(context.Background(), expires)
	job := &browserJobState{view: BrowserJob{ID: uuid.NewString(), SiteID: siteID, Status: "starting", ExpiresAt: expires}, actorID: actorID, site: *site, proxy: proxy, request: fingerprint, ctx: jobCtx, cancel: cancel, launching: true}
	a.jobs[job.view.ID] = job
	job.timer = time.AfterFunc(a.ttl, func() { job.ops.Lock(); defer job.ops.Unlock(); a.finish(job, "expired", "browser_expired") })
	initial := job.snapshot()
	a.mu.Unlock()
	go a.launch(job, upstreambrowser.Input{BaseURL: site.BaseURL, Platform: site.Platform, ProxyURL: proxy, Username: login.Username, Password: login.Password})
	return initial, nil
}

func (a *BrowserAuthorizer) launch(job *browserJobState, input upstreambrowser.Input) {
	defer func() { job.mu.Lock(); job.launching = false; job.mu.Unlock() }()
	driver, err := a.factory.Start(job.ctx, input)
	job.mu.Lock()
	if !browserActive(job.view.Status) || job.ctx.Err() != nil {
		job.mu.Unlock()
		if driver != nil {
			_ = driver.Close()
		}
		return
	}
	if err != nil || driver == nil {
		job.mu.Unlock()
		if driver != nil {
			_ = driver.Close()
		}
		job.ops.Lock()
		defer job.ops.Unlock()
		code := upstreambrowser.Code(err)
		if code == "" {
			code = "browser_protocol_error"
		}
		a.finish(job, "failed", code)
		return
	}
	job.driver = driver
	job.view.Status = "waiting"
	job.mu.Unlock()
}

func (a *BrowserAuthorizer) lookup(actorID, siteID int64, id string) (*browserJobState, error) {
	if a == nil || actorID <= 0 || siteID <= 0 {
		return nil, ErrNotFound
	}
	a.mu.Lock()
	job := a.jobs[id]
	a.mu.Unlock()
	if job == nil || job.actorID != actorID || job.site.ID != siteID {
		return nil, ErrNotFound
	}
	return job, nil
}

// Call with the job operation lock. No pool lock is held during process I/O.
func (a *BrowserAuthorizer) finish(job *browserJobState, status, code string) {
	job.mu.Lock()
	if !browserActive(job.view.Status) {
		job.mu.Unlock()
		return
	}
	job.view.Status = status
	job.view.ErrorCode = code
	job.view.Frame = nil
	job.closing = true
	driver := job.driver
	job.driver = nil
	job.mu.Unlock()
	if driver != nil {
		if err := driver.Close(); err != nil {
			log.Printf("[UpstreamGovernance] browser cleanup: %s", upstreambrowser.Code(err))
			job.mu.Lock()
			if job.view.ErrorCode == "" {
				job.view.ErrorCode = upstreambrowser.Code(err)
			}
			job.mu.Unlock()
		}
	}
	job.cancel()
	job.mu.Lock()
	job.closing = false
	job.mu.Unlock()
}

func (a *BrowserAuthorizer) validTarget(ctx context.Context, job *browserJobState) error {
	if !a.now().Before(job.view.ExpiresAt) {
		return context.DeadlineExceeded
	}
	site, err := a.service.store.GetSite(ctx, job.site.ID)
	if err != nil {
		return err
	}
	if site.ID != job.site.ID || site.Version != job.site.Version || site.BaseURL != job.site.BaseURL || site.Platform != job.site.Platform || !reflect.DeepEqual(site.ProxyID, job.site.ProxyID) {
		return ErrConflict
	}
	proxy, err := a.resolveProxy(ctx, *site)
	if err != nil {
		return err
	}
	if proxy != job.proxy {
		return ErrConflict
	}
	return nil
}

func browserOperationContext(ctx context.Context, job *browserJobState) (context.Context, func()) {
	deadline := time.Now().Add(15 * time.Second)
	if job.view.ExpiresAt.Before(deadline) {
		deadline = job.view.ExpiresAt
	}
	op, cancel := context.WithDeadline(ctx, deadline)
	stop := context.AfterFunc(job.ctx, cancel)
	return op, func() { stop(); cancel() }
}

func (a *BrowserAuthorizer) targetFailure(job *browserJobState, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		a.finish(job, "expired", "browser_expired")
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	a.finish(job, "failed", ErrorCode(err))
}

func (a *BrowserAuthorizer) Get(ctx context.Context, actorID, siteID int64, id string) (*BrowserJob, error) {
	job, err := a.lookup(actorID, siteID, id)
	if err != nil {
		return nil, err
	}
	if !job.ops.TryLock() {
		return job.snapshot(), nil
	}
	defer job.ops.Unlock()
	if !browserActive(job.snapshot().Status) {
		return job.snapshot(), nil
	}
	op, cancel := browserOperationContext(ctx, job)
	defer cancel()
	if err = a.validTarget(op, job); err != nil {
		a.targetFailure(job, err)
		return job.snapshot(), nil
	}
	job.mu.Lock()
	driver := job.driver
	job.mu.Unlock()
	if driver == nil {
		return job.snapshot(), nil
	}
	view, err := driver.Snapshot(op)
	if err != nil {
		if !a.now().Before(job.view.ExpiresAt) {
			a.finish(job, "expired", "browser_expired")
		} else if ctx.Err() == nil && job.ctx.Err() == nil {
			a.finish(job, "failed", upstreambrowser.Code(err))
		}
		return job.snapshot(), nil
	}
	// Keep the client DTO bounded even if a driver bypasses the subprocess decoder.
	switch view.ErrorCode {
	case "", "browser_navigation_failed", "browser_render_failed", "browser_page_closed", "browser_timeout", "browser_unsupported_route":
	default:
		a.finish(job, "failed", "browser_protocol_error")
		return job.snapshot(), nil
	}
	if view.Status == "failed" {
		code := view.ErrorCode
		if code == "" {
			code = "browser_login_failed"
		}
		a.finish(job, "failed", code)
		return job.snapshot(), nil
	}
	if view.Status != "waiting" && view.Status != "starting" && view.Status != "ready" {
		a.finish(job, "failed", "browser_protocol_error")
		return job.snapshot(), nil
	}
	var frame *BrowserFrame
	if view.Image != "" && view.Status != "ready" {
		if len(view.Image) > 2<<20 {
			a.finish(job, "failed", "browser_protocol_error")
			return job.snapshot(), nil
		}
		raw, decodeErr := base64.StdEncoding.DecodeString(view.Image)
		if decodeErr != nil || len(raw) < 3 || raw[0] != 0xff || raw[1] != 0xd8 || view.Width != upstreambrowser.Width || view.Height != upstreambrowser.Height {
			a.finish(job, "failed", "browser_protocol_error")
			return job.snapshot(), nil
		}
		frame = &BrowserFrame{Image: view.Image, Width: view.Width, Height: view.Height}
	}
	job.mu.Lock()
	job.view.Status = view.Status
	job.view.ErrorCode = view.ErrorCode
	job.view.Frame = frame
	job.mu.Unlock()
	return job.snapshot(), nil
}

func validBrowserAction(action upstreambrowser.Action) bool {
	for _, value := range []float64{action.X, action.Y, action.DeltaX, action.DeltaY} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	switch action.Type {
	case "pointer_down", "pointer_move", "pointer_up", "wheel":
		return action.X >= 0 && action.X < upstreambrowser.Width && action.Y >= 0 && action.Y < upstreambrowser.Height && math.Abs(action.DeltaX) <= 2000 && math.Abs(action.DeltaY) <= 2000 && action.Text == "" && action.Key == ""
	case "text":
		return action.Text != "" && len(action.Text) <= 2048 && utf8.ValidString(action.Text) && !strings.ContainsFunc(action.Text, func(r rune) bool { return r < 32 || r == 127 }) && action.Key == ""
	case "key":
		switch action.Key {
		case "Enter", "Tab", "Escape", "Backspace", "Delete", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "Space":
			return action.Text == ""
		}
	}
	return false
}

func (a *BrowserAuthorizer) Action(ctx context.Context, actorID, siteID int64, id string, action upstreambrowser.Action) error {
	if !validBrowserAction(action) {
		return ErrInvalid
	}
	job, err := a.lookup(actorID, siteID, id)
	if err != nil {
		return err
	}
	if !job.ops.TryLock() {
		return ErrBusy
	}
	defer job.ops.Unlock()
	op, cancel := browserOperationContext(ctx, job)
	defer cancel()
	if err = a.validTarget(op, job); err != nil {
		a.targetFailure(job, err)
		return err
	}
	job.mu.Lock()
	driver, status := job.driver, job.view.Status
	job.mu.Unlock()
	if status != "waiting" || driver == nil {
		return ErrConflict
	}
	if err = driver.Action(op, action); err != nil {
		if errors.Is(err, upstreambrowser.ErrInvalid) {
			return ErrInvalid
		}
		if !a.now().Before(job.view.ExpiresAt) {
			a.finish(job, "expired", "browser_expired")
		} else if ctx.Err() == nil && job.ctx.Err() == nil {
			a.finish(job, "failed", upstreambrowser.Code(err))
		}
		return ErrUnsupported
	}
	return nil
}

func (a *BrowserAuthorizer) Complete(ctx context.Context, actorID, siteID int64, id string) (*ConnectResult, error) {
	job, err := a.lookup(actorID, siteID, id)
	if err != nil {
		return nil, err
	}
	if !job.ops.TryLock() {
		return nil, ErrBusy
	}
	defer job.ops.Unlock()
	job.mu.Lock()
	result := job.result
	status := job.view.Status
	driver := job.driver
	job.mu.Unlock()
	if status == "completed" && result != nil {
		return result, nil
	}
	if !browserActive(status) || driver == nil {
		return nil, ErrConflict
	}
	if job.ctx.Err() != nil {
		return nil, ErrConflict
	}
	op, cancel := browserOperationContext(ctx, job)
	defer cancel()
	if err = a.validTarget(op, job); err != nil {
		a.targetFailure(job, err)
		return nil, err
	}
	job.mu.Lock()
	if job.ctx.Err() != nil {
		job.mu.Unlock()
		return nil, ErrConflict
	}
	job.completing = true
	job.mu.Unlock()
	defer func() { job.mu.Lock(); job.completing = false; job.mu.Unlock() }()
	capture, err := driver.Result(op)
	if err != nil {
		a.finish(job, "failed", upstreambrowser.Code(err))
		return nil, ErrUnsupported
	}
	if capture == nil {
		return nil, ErrConflict
	}
	issued := capture.IssuedAt
	session := Session{AccessToken: capture.AccessToken, RefreshToken: capture.RefreshToken, ExpiresAt: capture.ExpiresAt, IssuedAt: &issued, Cookies: capture.Cookies, UserID: capture.UserID, AuthVariant: capture.AuthVariant, UserAgent: capture.UserAgent}
	var login *LoginCredentials
	if capture.VerifiedLogin != nil {
		login = &LoginCredentials{Username: capture.VerifiedLogin.Username, Password: capture.VerifiedLogin.Password}
	}
	result, err = a.service.ImportBrowserSession(WithBrowserAuthorizationProxy(op, job.proxy), siteID, job.site.Version, session, login)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			return nil, err
		}
		if !a.now().Before(job.view.ExpiresAt) {
			a.finish(job, "expired", "browser_expired")
			return nil, err
		}
		a.finish(job, "failed", ErrorCode(err))
		return nil, err
	}
	job.mu.Lock()
	job.result = result
	job.mu.Unlock()
	a.finish(job, "completed", "")
	return result, nil
}

func (a *BrowserAuthorizer) Cancel(ctx context.Context, actorID, siteID int64, id string) error {
	job, err := a.lookup(actorID, siteID, id)
	if err != nil {
		return err
	}
	job.mu.Lock()
	completing := job.completing
	if !completing {
		job.cancel()
	}
	job.mu.Unlock()
	if completing {
		return ErrBusy
	}
	job.ops.Lock()
	defer job.ops.Unlock()
	a.finish(job, "cancelled", "")
	return nil
}

func (a *BrowserAuthorizer) Stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closing = true
	jobs := make([]*browserJobState, 0, len(a.jobs))
	for _, job := range a.jobs {
		jobs = append(jobs, job)
	}
	a.mu.Unlock()
	for _, job := range jobs {
		job.cancel()
		job.ops.Lock()
		a.finish(job, "cancelled", "")
		if job.timer != nil {
			job.timer.Stop()
		}
		job.ops.Unlock()
	}
}
