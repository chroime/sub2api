package upstreamgovernance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/upstreambrowser"
	"github.com/stretchr/testify/require"
)

type browserFakeDriver struct {
	closed  atomic.Int32
	actions atomic.Int32
	capture *upstreambrowser.Capture
	failure *upstreambrowser.View
}

func (d *browserFakeDriver) Snapshot(context.Context) (upstreambrowser.View, error) {
	if d.failure != nil {
		return *d.failure, nil
	}
	status := "waiting"
	if d.capture != nil {
		status = "ready"
	}
	return upstreambrowser.View{Status: status, Width: 1024, Height: 720}, nil
}

func TestGovernanceBrowserFailurePreservesSanitizedHelperCode(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{"browser_navigation_failed", "browser_navigation_failed"},
		{"browser_unsupported_route", "browser_unsupported_route"},
		{"", "browser_login_failed"},
		{"https://secret-canary.example/token", "browser_protocol_error"},
	} {
		t.Run(test.want, func(t *testing.T) {
			svc, _, _, _ := setupEngine(t)
			factory := &browserFakeFactory{driver: &browserFakeDriver{failure: &upstreambrowser.View{
				Status: "failed", Width: upstreambrowser.Width, Height: upstreambrowser.Height, ErrorCode: test.code,
			}}}
			a := NewBrowserAuthorizer(svc, factory, nil)
			t.Cleanup(a.Stop)
			job, err := a.Start(t.Context(), 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"})
			require.NoError(t, err)
			var view *BrowserJob
			require.Eventually(t, func() bool {
				view, err = a.Get(t.Context(), 7, 1, job.ID)
				return err == nil && view.Status == "failed"
			}, time.Second, time.Millisecond)
			require.Equal(t, test.want, view.ErrorCode)
			raw, err := json.Marshal(view)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "canary")
		})
	}
}

func TestGovernanceBrowserRenderDiagnosticClearsOnNextSuccessfulFrame(t *testing.T) {
	svc, _, _, _ := setupEngine(t)
	driver := &browserFakeDriver{failure: &upstreambrowser.View{
		Status: "waiting", Width: upstreambrowser.Width, Height: upstreambrowser.Height, ErrorCode: "browser_render_failed",
	}}
	a := NewBrowserAuthorizer(svc, &browserFakeFactory{driver: driver}, nil)
	t.Cleanup(a.Stop)
	job, err := a.Start(t.Context(), 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"})
	require.NoError(t, err)
	var view *BrowserJob
	require.Eventually(t, func() bool {
		view, err = a.Get(t.Context(), 7, 1, job.ID)
		// Launch can expose waiting before Get has read its first driver snapshot.
		// Wait for the diagnostic itself, not just for the startup transition.
		return err == nil && view.Status == "waiting" && view.ErrorCode == "browser_render_failed"
	}, time.Second, time.Millisecond)
	require.Equal(t, "browser_render_failed", view.ErrorCode)
	require.Nil(t, view.Frame)
	require.Zero(t, driver.closed.Load(), "a transient screenshot failure must not end authorization")
	require.NoError(t, a.Action(t.Context(), 7, 1, job.ID, upstreambrowser.Action{Type: "key", Key: "Enter"}))
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, upstreambrowser.Width, upstreambrowser.Height)), nil))
	driver.failure = &upstreambrowser.View{
		Status: "waiting", Width: upstreambrowser.Width, Height: upstreambrowser.Height, Image: base64.StdEncoding.EncodeToString(encoded.Bytes()),
	}
	view, err = a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "waiting", view.Status)
	require.Empty(t, view.ErrorCode)
	require.NotNil(t, view.Frame)
	require.Zero(t, driver.closed.Load())
}
func (d *browserFakeDriver) Action(context.Context, upstreambrowser.Action) error {
	d.actions.Add(1)
	return nil
}
func (d *browserFakeDriver) Result(context.Context) (*upstreambrowser.Capture, error) {
	return d.capture, nil
}
func (d *browserFakeDriver) Close() error { d.closed.Add(1); return nil }

type browserFakeFactory struct {
	driver *browserFakeDriver
	starts atomic.Int32
}

func (f *browserFakeFactory) Available() (bool, string) { return true, "" }
func (f *browserFakeFactory) Start(context.Context, upstreambrowser.Input) (upstreambrowser.Driver, error) {
	f.starts.Add(1)
	return f.driver, nil
}

func TestGovernanceBrowserJobOwnerVersionAndExplicitStart(t *testing.T) {
	svc, store, _, _ := setupEngine(t)
	factory := &browserFakeFactory{driver: &browserFakeDriver{}}
	a := NewBrowserAuthorizer(svc, factory, nil)
	t.Cleanup(a.Stop)
	before := store.site
	input := BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "browser-user-canary", Password: "browser-password-canary"}
	job, err := a.Start(t.Context(), 7, 1, input)
	require.NoError(t, err)
	require.Equal(t, "starting", job.Status)
	require.Eventually(t, func() bool { view, e := a.Get(t.Context(), 7, 1, job.ID); return e == nil && view.Status == "waiting" }, time.Second, time.Millisecond)
	view, err := a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "waiting", view.Status)
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "canary")
	require.Equal(t, before, store.site, "opening a browser never persists a login")
	_, err = a.Get(t.Context(), 8, 1, job.ID)
	require.ErrorIs(t, err, ErrNotFound)
	err = a.Cancel(t.Context(), 7, 2, job.ID)
	require.ErrorIs(t, err, ErrNotFound)
	store.site.Version++
	view, err = a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", view.Status)
	require.Equal(t, "stale_preview", view.ErrorCode)
	require.Eventually(t, func() bool { return factory.driver.closed.Load() == 1 }, time.Second, time.Millisecond)
}

func TestGovernanceBrowserStartDeduplicatesAndCancellationCloses(t *testing.T) {
	svc, _, _, _ := setupEngine(t)
	factory := &browserFakeFactory{driver: &browserFakeDriver{}}
	a := NewBrowserAuthorizer(svc, factory, nil)
	t.Cleanup(a.Stop)
	input := BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"}
	job, err := a.Start(t.Context(), 7, 1, input)
	require.NoError(t, err)
	again, err := a.Start(t.Context(), 7, 1, input)
	require.NoError(t, err)
	require.Equal(t, job.ID, again.ID)
	_, err = a.Start(t.Context(), 8, 1, input)
	require.ErrorIs(t, err, ErrBusy)
	require.NoError(t, a.Cancel(t.Context(), 7, 1, job.ID))
	view, err := a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", view.Status)
	require.NoError(t, a.Cancel(t.Context(), 7, 1, job.ID))
	require.Eventually(t, func() bool { return factory.starts.Load() == 1 && factory.driver.closed.Load() == 1 }, time.Second, time.Millisecond)
}

func TestGovernanceBrowserVersionProxyAndActionBounds(t *testing.T) {
	svc, store, _, _ := setupEngine(t)
	factory := &browserFakeFactory{driver: &browserFakeDriver{}}
	proxy := "http://proxy.example:8080"
	a := NewBrowserAuthorizer(svc, factory, func(context.Context, Site) (string, error) { return proxy, nil })
	t.Cleanup(a.Stop)
	input := BrowserAuthorizationInput{ExpectedSiteVersion: 2, Username: "user", Password: "password"}
	_, err := a.Start(t.Context(), 7, 1, input)
	require.ErrorIs(t, err, ErrConflict)
	require.Zero(t, factory.starts.Load())
	input.ExpectedSiteVersion = store.site.Version
	job, err := a.Start(t.Context(), 7, 1, input)
	require.NoError(t, err)
	require.Eventually(t, func() bool { v, e := a.Get(t.Context(), 7, 1, job.ID); return e == nil && v.Status == "waiting" }, time.Second, time.Millisecond)
	for _, action := range []upstreambrowser.Action{{Type: "javascript", Text: "alert(1)"}, {Type: "pointer_down", X: -1}, {Type: "pointer_move", X: 1024}, {Type: "wheel", DeltaY: 2001}, {Type: "key", Key: "Control+L"}, {Type: "text", Text: ""}} {
		require.ErrorIs(t, a.Action(t.Context(), 7, 1, job.ID, action), ErrInvalid)
	}
	require.Zero(t, factory.driver.actions.Load())
	require.NoError(t, a.Action(t.Context(), 7, 1, job.ID, upstreambrowser.Action{Type: "pointer_down", X: 30, Y: 40}))
	require.EqualValues(t, 1, factory.driver.actions.Load())
	proxy = "http://changed.example:8080"
	require.ErrorIs(t, a.Action(t.Context(), 7, 1, job.ID, upstreambrowser.Action{Type: "key", Key: "Enter"}), ErrConflict)
	view, err := a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", view.Status)
}

type browserVerifyingConnector struct {
	*fakeConnector
	verified            atomic.Int32
	waitForCancellation bool
}

func (c *browserVerifyingConnector) VerifySession(ctx context.Context, _ Site, s Session) (Session, error) {
	c.verified.Add(1)
	if c.waitForCancellation {
		<-ctx.Done()
		return Session{}, ctx.Err()
	}
	s.UserID = 5
	return s, nil
}

func TestGovernanceBrowserCompletionCannotOutliveJob(t *testing.T) {
	svc, store, connector, _ := setupEngine(t)
	svc.connector = &browserVerifyingConnector{fakeConnector: connector, waitForCancellation: true}
	issued := time.Now().UTC()
	expires := issued.Add(time.Hour)
	factory := &browserFakeFactory{driver: &browserFakeDriver{capture: &upstreambrowser.Capture{AccessToken: "access-canary", IssuedAt: issued, ExpiresAt: &expires, ExpiresIn: 3600, UserAgent: "BrowserFixture/1", AuthVariant: "bearer"}}}
	a := NewBrowserAuthorizer(svc, factory, nil)
	a.ttl = 150 * time.Millisecond
	t.Cleanup(a.Stop)
	before := store.site
	job, err := a.Start(t.Context(), 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"})
	require.NoError(t, err)
	require.Eventually(t, func() bool { view, e := a.Get(t.Context(), 7, 1, job.ID); return e == nil && view.Status == "ready" }, time.Second, time.Millisecond)
	started := time.Now()
	_, err = a.Complete(t.Context(), 7, 1, job.ID)
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, before, store.site)
	view, err := a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "expired", view.Status)
}

func TestGovernanceBrowserCompletionIsExplicitVerifiedAndIdempotent(t *testing.T) {
	svc, store, connector, _ := setupEngine(t)
	verifier := &browserVerifyingConnector{fakeConnector: connector}
	svc.connector = verifier
	issued := time.Now().UTC()
	expires := issued.Add(time.Hour)
	driver := &browserFakeDriver{capture: &upstreambrowser.Capture{AccessToken: "browser-access-canary", RefreshToken: "browser-refresh-canary", IssuedAt: issued, ExpiresAt: &expires, ExpiresIn: 3600, UserAgent: "BrowserFixture/1", AuthVariant: "bearer", VerifiedLogin: &upstreambrowser.VerifiedLogin{Username: "browser-user", Password: "browser-password-canary"}}}
	factory := &browserFakeFactory{driver: driver}
	a := NewBrowserAuthorizer(svc, factory, nil)
	t.Cleanup(a.Stop)
	before := store.site
	job, err := a.Start(t.Context(), 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "browser-user", Password: "browser-password-canary"})
	require.NoError(t, err)
	require.Eventually(t, func() bool { v, e := a.Get(t.Context(), 7, 1, job.ID); return e == nil && v.Status == "ready" }, time.Second, time.Millisecond)
	require.Equal(t, before, store.site)
	store.locked = true
	_, err = a.Complete(t.Context(), 7, 1, job.ID)
	require.ErrorIs(t, err, ErrBusy)
	view, err := a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "ready", view.Status)
	store.locked = false
	result, err := a.Complete(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.NotNil(t, result.Site)
	again, err := a.Complete(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.EqualValues(t, 1, verifier.verified.Load())
	value, err := svc.session(store.site)
	require.NoError(t, err)
	require.Equal(t, "browser-refresh-canary", value.RefreshToken)
	view, err = a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", view.Status)
	require.Nil(t, view.Frame)
	raw, _ := json.Marshal(view)
	require.NotContains(t, string(raw), "canary")
	require.EqualValues(t, 1, driver.closed.Load())
}

type browserBlockingSiteStore struct {
	*memoryStore
	block   atomic.Bool
	entered chan struct{}
}

func (m *browserBlockingSiteStore) GetSite(ctx context.Context, id int64) (*Site, error) {
	if m.block.Load() {
		select {
		case m.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.memoryStore.GetSite(ctx, id)
}

func TestGovernanceBrowserCancellationAndExpiryInterruptTargetLookup(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "expire"}[expire], func(t *testing.T) {
			svc, store, _, _ := setupEngine(t)
			blocking := &browserBlockingSiteStore{memoryStore: store, entered: make(chan struct{}, 1)}
			svc.store = blocking
			factory := &browserFakeFactory{driver: &browserFakeDriver{}}
			a := NewBrowserAuthorizer(svc, factory, nil)
			if expire {
				a.ttl = 100 * time.Millisecond
			}
			t.Cleanup(a.Stop)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			job, err := a.Start(ctx, 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"})
			require.NoError(t, err)
			blocking.block.Store(true)
			readDone := make(chan struct{})
			go func() { defer close(readDone); _, _ = a.Get(ctx, 7, 1, job.ID) }()
			select {
			case <-blocking.entered:
			case <-time.After(time.Second):
				t.Fatal("target lookup not entered")
			}
			if !expire {
				cancelDone := make(chan struct{})
				go func() { defer close(cancelDone); _ = a.Cancel(ctx, 7, 1, job.ID) }()
				select {
				case <-cancelDone:
				case <-time.After(time.Second):
					t.Error("cancel did not interrupt target lookup")
					cancel()
				}
			}
			select {
			case <-readDone:
			case <-time.After(time.Second):
				t.Error("expiry did not interrupt target lookup")
				cancel()
				<-readDone
			}
		})
	}
}

func TestGovernanceBrowserTextBoundsMatchDriver(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 2049), "line\nbreak", "tab\tvalue", "control\x7f"} {
		require.False(t, validBrowserAction(upstreambrowser.Action{Type: "text", Text: text}))
	}
	require.True(t, validBrowserAction(upstreambrowser.Action{Type: "text", Text: strings.Repeat("x", 2048)}))
}

func TestGovernanceBrowserProxyConflictIsNotHiddenByConnector(t *testing.T) {
	connector := NewConnector(func(context.Context, Site) (HTTPDoer, error) { return nil, ErrConflict })
	_, _, err := connector.Login(t.Context(), Site{ID: 1, Platform: "sub2api", BaseURL: "https://upstream.example"}, LoginInput{Username: "user", Password: "password"})
	require.ErrorIs(t, err, ErrConflict)
}

func TestGovernanceBrowserExpiryAndUnavailable(t *testing.T) {
	svc, _, _, _ := setupEngine(t)
	a := NewBrowserAuthorizer(svc, nil, nil)
	require.False(t, a.Availability().Available)
	_, err := a.Start(t.Context(), 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"})
	require.ErrorIs(t, err, ErrBrowserUnavailable)
	factory := &browserFakeFactory{driver: &browserFakeDriver{}}
	a = NewBrowserAuthorizer(svc, factory, nil)
	t.Cleanup(a.Stop)
	job, err := a.Start(t.Context(), 7, 1, BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"})
	require.NoError(t, err)
	a.now = func() time.Time { return job.ExpiresAt.Add(time.Second) }
	view, err := a.Get(t.Context(), 7, 1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "expired", view.Status)
}

type browserMultiSiteStore struct {
	*memoryStore
	sites map[int64]Site
}

func (m *browserMultiSiteStore) GetSite(_ context.Context, id int64) (*Site, error) {
	site, ok := m.sites[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &site, nil
}

func TestGovernanceBrowserGlobalCapacityAndShutdown(t *testing.T) {
	svc, store, _, _ := setupEngine(t)
	sites := map[int64]Site{}
	for id := int64(1); id <= 3; id++ {
		site := store.site
		site.ID = id
		sites[id] = site
	}
	svc.store = &browserMultiSiteStore{memoryStore: store, sites: sites}
	factory := &browserFakeFactory{driver: &browserFakeDriver{}}
	a := NewBrowserAuthorizer(svc, factory, nil)
	t.Cleanup(a.Stop)
	input := BrowserAuthorizationInput{ExpectedSiteVersion: 1, Username: "user", Password: "password"}
	first, err := a.Start(t.Context(), 7, 1, input)
	require.NoError(t, err)
	second, err := a.Start(t.Context(), 7, 2, input)
	require.NoError(t, err)
	_, err = a.Start(t.Context(), 7, 3, input)
	require.ErrorIs(t, err, ErrBusy)
	require.NoError(t, a.Cancel(t.Context(), 7, 1, first.ID))
	firstState, lookupErr := a.lookup(7, 1, first.ID)
	require.NoError(t, lookupErr)
	require.Eventually(t, func() bool { return !firstState.occupiesSlot() }, time.Second, time.Millisecond)
	third, err := a.Start(t.Context(), 7, 3, input)
	require.NoError(t, err)
	a.Stop()
	for _, job := range []*BrowserJob{second, third} {
		view, e := a.Get(t.Context(), 7, job.SiteID, job.ID)
		require.NoError(t, e)
		require.Equal(t, "cancelled", view.Status)
	}
	_, err = a.Start(t.Context(), 7, 1, input)
	require.ErrorIs(t, err, ErrBrowserUnavailable)
}
