package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	store                Store
	connector            Connector
	local                LocalAccounts
	cipher               Encryptor
	durableKey           bool
	slots                chan struct{}
	now                  func() time.Time
	workerMu             sync.Mutex
	workerCancel         context.CancelFunc
	workerDone           chan struct{}
	sessionRefreshMu     sync.Mutex
	sessionRefreshCursor int64
	balanceNotifier      BalanceNotifier
	modelMu              sync.Mutex
	modelCancel          context.CancelFunc
	modelDone            chan struct{}
	modelWake            chan struct{}
	modelActive          map[string]modelActiveRun
	modelNotifier        ModelNotifier
	browserAuthorizer    *BrowserAuthorizer
}

func NewService(store Store, connector Connector, local LocalAccounts, cipher Encryptor, durableKey bool) *Service {
	return &Service{store: store, connector: connector, local: local, cipher: cipher, durableKey: durableKey, slots: make(chan struct{}, 2), now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) ListSites(ctx context.Context) ([]Site, error) { return s.store.ListSites(ctx) }
func (s *Service) Catalog(ctx context.Context, id int64) (*Snapshot, error) {
	return s.store.LatestSnapshot(ctx, id)
}
func (s *Service) Bindings(ctx context.Context, id int64) ([]Binding, error) {
	bindings, err := s.store.ListBindings(ctx, id)
	if err != nil || len(bindings) == 0 {
		return bindings, err
	}
	reader, ok := s.local.(LocalAccountNames)
	if !ok {
		return bindings, nil
	}
	ids := make([]int64, 0, len(bindings))
	seen := make(map[int64]bool, len(bindings))
	for _, binding := range bindings {
		if binding.AccountID > 0 && !seen[binding.AccountID] {
			ids = append(ids, binding.AccountID)
			seen[binding.AccountID] = true
		}
	}
	if len(ids) == 0 {
		return bindings, nil
	}
	names, err := reader.AccountNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	// Enrichment is response-only; never update stored bindings or accounts.
	bindings = append([]Binding(nil), bindings...)
	for i := range bindings {
		label := names[bindings[i].AccountID]
		bindings[i].AccountName = label.Name
		bindings[i].AccountDeleted = label.Deleted
	}
	return bindings, nil
}
func (s *Service) Events(ctx context.Context, id int64, page, size int) ([]Event, int64, error) {
	return s.store.ListEvents(ctx, id, page, size)
}
func (s *Service) Checks(ctx context.Context, id int64, page, size int) ([]Check, int64, error) {
	return s.store.ListChecks(ctx, id, page, size)
}
func (s *Service) Acknowledge(ctx context.Context, siteID, eventID int64) error {
	return s.store.AckEvent(ctx, siteID, eventID)
}

func validRate(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func validCost(v float64) bool { return validRate(v) && v <= 999999.9999 }
func validateSite(site *Site) error {
	site.Name = strings.TrimSpace(site.Name)
	if site.Name == "" || len(site.Name) > 100 || (site.Platform != "sub2api" && site.Platform != "newapi") {
		return ErrInvalid
	}
	u, e := url.Parse(strings.TrimSpace(site.BaseURL))
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return ErrInvalid
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return ErrInvalid
	}
	if ip := net.ParseIP(host); ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return ErrInvalid
	}
	if site.IntervalMinutes == 0 {
		site.IntervalMinutes = 15
	}
	if !validIntervalMinutes(site.IntervalMinutes) {
		return ErrInvalid
	}
	if site.ProxyID != nil && *site.ProxyID <= 0 {
		return ErrInvalid
	}
	site.BaseURL = strings.TrimSuffix(u.String(), "/")
	return nil
}

func (s *Service) CreateSite(ctx context.Context, input Site) (*Site, error) {
	if e := validateSite(&input); e != nil {
		return nil, e
	}
	input.ID = 0
	input.Version = 0
	input.HasCredential = false
	input.SessionCipher = ""
	input.LoginCipher = ""
	input.Status = "disconnected"
	input.LastError = ""
	input.LastSyncAt = nil
	input.NextSyncAt = s.now()
	input.CreatedAt = s.now()
	input.UpdatedAt = s.now()
	input.BalanceMonitor = defaultBalanceMonitor(input.Platform)
	input.BalanceMonitorStatus = BalanceMonitorStatus{State: "disabled"}
	input.balanceState = BalanceMonitorState{Status: input.BalanceMonitorStatus}
	if e := s.store.CreateSite(ctx, &input); e != nil {
		return nil, e
	}
	return &input, nil
}
func (s *Service) siteLock(ctx context.Context, id int64) (*Site, func(), error) {
	release, ok, e := s.store.LockSite(ctx, id)
	if e != nil {
		return nil, nil, e
	}
	if !ok {
		return nil, nil, ErrBusy
	}
	site, e := s.store.GetSite(ctx, id)
	if e != nil {
		release()
		return nil, nil, e
	}
	return site, release, nil
}
func (s *Service) UpdateSite(ctx context.Context, id int64, input Site) (*Site, error) {
	return s.UpdateSiteWithLogin(ctx, id, input, nil)
}

func (s *Service) UpdateSiteWithLogin(ctx context.Context, id int64, input Site, login *LoginCredentials) (*Site, error) {
	if e := validateSite(&input); e != nil {
		return nil, e
	}
	if login != nil {
		normalized, e := normalizeLoginCredentials(*login)
		if e != nil {
			return nil, e
		}
		login = &normalized
	}
	site, release, e := s.siteLock(ctx, id)
	if e != nil {
		return nil, e
	}
	defer release()
	if input.Version != site.Version {
		return nil, ErrConflict
	}
	changedOrigin := site.BaseURL != input.BaseURL || site.Platform != input.Platform
	changedProxy := !reflect.DeepEqual(site.ProxyID, input.ProxyID)
	if changedOrigin {
		bindings, e := s.store.ListBindings(ctx, id)
		if e != nil {
			return nil, e
		}
		if len(bindings) > 0 {
			return nil, ErrConflict
		}
		keys, e := s.store.ListManagedKeys(ctx, id)
		if e != nil {
			return nil, e
		}
		if len(keys) > 0 {
			return nil, ErrConflict
		}
		site.SessionCipher = ""
		site.LoginCipher = ""
		site.HasCredential = false
		site.Status = "disconnected"
		site.LastError = ""
		site.LastSyncAt = nil
		// A different upstream is a different wallet, potentially with another
		// unit. Its thresholds and delivery history must be configured afresh.
		site.BalanceMonitor = defaultBalanceMonitor(input.Platform)
		site.balanceState = BalanceMonitorState{Status: BalanceMonitorStatus{State: "disabled"}}
		site.BalanceMonitorStatus = site.balanceState.Status
	}
	if changedProxy && !changedOrigin && site.SessionCipher != "" {
		previous, err := s.session(*site)
		if err != nil {
			return nil, err
		}
		previous.RefreshState = "reauth_required"
		raw, err := json.Marshal(previous)
		if err != nil {
			return nil, ErrEncryption
		}
		site.SessionCipher, err = s.cipher.Encrypt(string(raw))
		if err != nil {
			return nil, ErrEncryption
		}
		site.Status, site.LastError = "reauth_required", "reauth_required"
		stored, err := s.storedLogin(*site)
		if err != nil {
			return nil, err
		}
		stored.Pending = nil
		site.LoginCipher, err = s.encryptLogin(stored)
		if err != nil {
			return nil, err
		}
	}
	if login != nil {
		value := storedLoginCredentials{LoginCredentials: *login}
		if previous, err := s.session(*site); err == nil {
			value.OwnerUserID = previous.UserID
		}
		site.LoginCipher, e = s.encryptLogin(value)
		if e != nil {
			return nil, e
		}
	}
	site.Name = input.Name
	site.Platform = input.Platform
	site.BaseURL = input.BaseURL
	site.ProxyID = input.ProxyID
	site.Enabled = input.Enabled
	site.IntervalMinutes = input.IntervalMinutes
	site.NextSyncAt = s.now()
	if e = s.store.UpdateSite(ctx, site, input.Version); e != nil {
		return nil, e
	}
	return site, nil
}
func (s *Service) DeleteSite(ctx context.Context, id int64) error {
	_, release, e := s.siteLock(ctx, id)
	if e != nil {
		return e
	}
	defer release()
	return s.store.DeleteSite(ctx, id)
}
func (s *Service) remoteSlot(ctx context.Context) (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *Service) session(site Site) (Session, error) {
	if !s.durableKey || s.cipher == nil {
		return Session{}, ErrEncryption
	}
	if site.SessionCipher == "" {
		return Session{}, ErrReauth
	}
	plain, e := s.cipher.Decrypt(site.SessionCipher)
	if e != nil {
		return Session{}, ErrReauth
	}
	var session Session
	if json.Unmarshal([]byte(plain), &session) != nil {
		return Session{}, ErrReauth
	}
	if !hasSessionCredential(session) {
		return Session{}, ErrReauth
	}
	return session, nil
}

func (s *Service) Connect(ctx context.Context, id int64, input LoginInput) (*ConnectResult, error) {
	if e := validateLoginInput(input); e != nil {
		return nil, e
	}
	if !s.durableKey || s.cipher == nil {
		return nil, ErrEncryption
	}
	free, e := s.remoteSlot(ctx)
	if e != nil {
		return nil, e
	}
	defer free()
	site, release, e := s.siteLock(ctx, id)
	if e != nil {
		return nil, e
	}
	defer release()
	// Saved login details belong to the site version displayed by the editor.
	// Check while holding the site lock before sending credentials upstream.
	if input.ExpectedSiteVersion != nil {
		if *input.ExpectedSiteVersion <= 0 {
			return nil, ErrInvalid
		}
		if *input.ExpectedSiteVersion != site.Version {
			return nil, ErrConflict
		}
	}
	session, challenge, e := s.connector.Login(ctx, *site, input)
	if challenge != nil && (e == nil || errors.Is(e, ErrUnsupported)) {
		if e = s.stageLoginChallenge(ctx, *site, input, challenge); e != nil {
			return nil, e
		}
		return &ConnectResult{Challenge: challenge}, nil
	}
	if e != nil {
		return nil, e
	}
	if challenge != nil {
		return &ConnectResult{Challenge: challenge}, nil
	}
	return s.connectSessionLocked(ctx, site, input, session, nil)
}

func (s *Service) connectSessionLocked(ctx context.Context, site *Site, input LoginInput, session Session, verifiedLogin *LoginCredentials) (*ConnectResult, error) {
	if !hasSessionCredential(session) {
		return nil, ErrReauth
	}
	bindings, e := s.store.ListBindings(ctx, site.ID)
	if e != nil {
		return nil, e
	}
	if len(bindings) > 0 {
		old, e := s.session(*site)
		if e != nil {
			return nil, e
		}
		if old.UserID != session.UserID {
			return nil, ErrConflict
		}
	}
	keys, e := s.store.ListManagedKeys(ctx, site.ID)
	if e != nil {
		return nil, e
	}
	for _, key := range keys {
		if key.OwnerUserID != session.UserID {
			return nil, ErrConflict
		}
	}
	raw, e := json.Marshal(session)
	if e != nil {
		return nil, ErrReauth
	}
	cipher, e := s.cipher.Encrypt(string(raw))
	if e != nil {
		return nil, ErrEncryption
	}
	loginCipher, e := s.loginAfterConnect(*site, input, session)
	if verifiedLogin != nil {
		loginCipher, e = s.encryptLogin(storedLoginCredentials{LoginCredentials: *verifiedLogin, OwnerUserID: session.UserID})
	}
	if e != nil {
		return nil, e
	}
	site.SessionCipher = cipher
	site.LoginCipher = loginCipher
	site.HasCredential = true
	site.Status = "connected"
	site.LastError = ""
	site.NextSyncAt = s.now()
	version := site.Version
	if e = s.store.UpdateSite(ctx, site, version); e != nil {
		return nil, e
	}
	return &ConnectResult{Site: site}, nil
}

func validateCatalog(c Catalog) error {
	if len(c.Groups) > 1000 || len(c.Channels) > 1000 {
		return ErrUnsupported
	}
	seen := map[string]bool{}
	for _, g := range c.Groups {
		if g.ID == "" || len(g.ID) > 200 || len(g.Name) > 300 || seen[g.ID] || len(g.Models) > 3000 || len(g.Prices) > 3000 {
			return ErrUnsupported
		}
		seen[g.ID] = true
		for _, r := range []*float64{g.RateMultiplier, g.UserRateMultiplier, g.ResolvedRateMultiplier, g.PeakRateMultiplier} {
			if r != nil && !validRate(*r) {
				return ErrUnsupported
			}
		}
		for _, m := range g.Models {
			if m == "" || len(m) > 300 {
				return ErrUnsupported
			}
		}
	}
	raw, e := json.Marshal(c)
	if e != nil || len(raw) > 2*1024*1024 {
		return ErrUnsupported
	}
	return nil
}

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrReauth):
		return "reauth_required"
	case errors.Is(err, ErrConflict):
		return "stale_preview"
	case errors.Is(err, ErrSiteInUse):
		return "site_in_use"
	case errors.Is(err, ErrBusy):
		return "site_busy"
	case errors.Is(err, ErrUnsupported):
		return "unsupported_contract"
	case errors.Is(err, ErrEncryption):
		return "persistent_encryption_required"
	case errors.Is(err, ErrInvalid):
		return "invalid_input"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "operation_failed"
	}
}
func (s *Service) Sync(ctx context.Context, id int64) (*Snapshot, error) {
	free, e := s.remoteSlot(ctx)
	if e != nil {
		return nil, e
	}
	defer free()
	site, release, e := s.siteLock(ctx, id)
	if e != nil {
		return nil, e
	}
	defer release()
	return s.syncLocked(ctx, *site)
}
func (s *Service) syncLocked(ctx context.Context, site Site) (*Snapshot, error) {
	return s.syncLockedWithAutoReauthorization(ctx, site, false)
}

func (s *Service) syncLockedWithAutoReauthorization(ctx context.Context, site Site, autoReauthorize bool) (*Snapshot, error) {
	previousCipher := site.SessionCipher
	session, e := s.managementSessionLocked(ctx, &site, false)
	var catalog Catalog
	if e == nil {
		catalog, e = s.connector.Discover(ctx, site, session)
		if errors.Is(e, ErrReauth) && site.SessionCipher == previousCipher {
			session, e = s.managementSessionLocked(ctx, &site, true)
			if e == nil {
				catalog, e = s.connector.Discover(ctx, site, session)
			}
		}
		if errors.Is(e, ErrReauth) && session.AccessToken != "" {
			e = s.requireAuthorizationLocked(ctx, &site, session)
		}
	}
	if autoReauthorize && site.Enabled && errors.Is(e, ErrReauth) {
		session, e = s.autoReauthorizeLocked(ctx, &site)
		if e == nil {
			catalog, e = s.connector.Discover(ctx, site, session)
			if errors.Is(e, ErrReauth) {
				// A freshly issued session that is rejected on its first read is
				// not evidence that the saved password is still usable. Freeze
				// automatic retries until the credentials are edited or a manual
				// authorization replaces them.
				persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				_, e = s.recordAutoReauthorizationFailure(persistCtx, &site, session, loginCipherHash(site.LoginCipher), s.now(), autoReauthorizationCredentialsRejected)
				cancel()
			}
		}
	}
	if e == nil {
		e = validateCatalog(catalog)
	}
	if errors.Is(e, ErrConflict) {
		return nil, e
	}
	now := s.now()
	next := addMinutes(now, int64(site.IntervalMinutes))
	if e != nil {
		state := "error"
		if errors.Is(e, ErrReauth) {
			state = "reauth_required"
		}
		if oe := s.store.ObserveSite(ctx, site.ID, state, ErrorCode(e), now, next); oe != nil {
			return nil, oe
		}
		if site.Status != state || site.LastError != ErrorCode(e) {
			if ae := s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "sync_failed", After: ErrorCode(e), CreatedAt: now}); ae != nil {
				return nil, ae
			}
		}
		return nil, e
	}
	var before Catalog
	previous, e := s.store.LatestSnapshot(ctx, site.ID)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	if previous != nil {
		before = previous.Catalog
	}
	snapshot := &Snapshot{SiteID: site.ID, SiteVersion: site.Version, Catalog: catalog, CreatedAt: now}
	events := DiffCatalog(site.ID, before, catalog)
	for i := range events {
		events[i].CreatedAt = now
	}
	if e = s.store.SaveSnapshot(ctx, snapshot, events); e != nil {
		return nil, e
	}
	if e = s.store.ObserveSite(ctx, site.ID, "healthy", "", now, next); e != nil {
		return nil, e
	}
	// Notification failures have their own persisted status and must not turn a
	// successfully collected catalog into a failed synchronization.
	s.reconcileSuccessfulSnapshot(ctx, site, snapshot)
	s.evaluateRechargeAfterSnapshot(ctx, site, snapshot)
	s.checkBalanceMonitor(ctx, site, snapshot)
	return snapshot, nil
}

func marker(siteID int64, group, platform string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", siteID, group, platform)))
	return "sub2api-governance-" + hex.EncodeToString(sum[:12])
}
func (s *Service) Preview(ctx context.Context, id int64, selections []Selection) (*Preview, error) {
	if len(selections) == 0 || len(selections) > 100 {
		return nil, ErrInvalid
	}
	site, release, e := s.siteLock(ctx, id)
	if e != nil {
		return nil, e
	}
	defer release()
	if _, e = s.session(*site); e != nil {
		return nil, e
	}
	snapshot, e := s.store.LatestSnapshot(ctx, id)
	if e != nil {
		return nil, e
	}
	if snapshot.SiteVersion != site.Version {
		return nil, ErrConflict
	}
	groups := map[string]RemoteGroup{}
	for _, g := range snapshot.Catalog.Groups {
		groups[g.ID] = g
	}
	bindings, e := s.store.ListBindings(ctx, id)
	if e != nil {
		return nil, e
	}
	byMarker := map[string]Binding{}
	for _, b := range bindings {
		byMarker[b.Marker] = b
	}
	managed, e := s.managedKeysByMarker(ctx, id)
	if e != nil {
		return nil, e
	}
	p := &Preview{ID: uuid.NewString(), SiteID: id, SiteVersion: site.Version, SnapshotID: snapshot.ID, CreatedAt: s.now(), ExpiresAt: s.now().Add(15 * time.Minute), Rows: []PreviewRow{}}
	seen := map[string]bool{}
	for _, selection := range selections {
		g, ok := groups[selection.RemoteGroupID]
		if !ok || !validSiteTransport(site.Platform, selection.Platform) || !validCost(selection.CostMultiplier) {
			return nil, ErrInvalid
		}
		selection.LocalGroupIDs, e = NormalizeLocalGroupIDs(selection.LocalGroupIDs, selection.LocalGroupID)
		if e != nil {
			return nil, e
		}
		selection.LocalGroupID = selection.LocalGroupIDs[0]
		// Existing account cost storage is NUMERIC(10,4). Freeze its persisted
		// precision in the preview so recovery compares exactly the approved cost.
		selection.CostMultiplier = math.Round(selection.CostMultiplier*10000) / 10000
		selection.AccountName = strings.TrimSpace(selection.AccountName)
		if selection.AccountName == "" {
			selection.AccountName = site.BaseURL + "--" + strconv.FormatFloat(selection.CostMultiplier, 'f', -1, 64)
		}
		if len(selection.AccountName) > 100 {
			return nil, ErrInvalid
		}
		selection.AccountConfig, e = NormalizeAccountConfig(selection.AccountConfig, selection.Platform, g.Models)
		if e != nil {
			return nil, e
		}
		if !compatibleTransport(g.Platform, selection.Platform) {
			return nil, ErrInvalid
		}
		key := marker(id, g.ID, selection.Platform)
		if seen[key] {
			return nil, ErrInvalid
		}
		seen[key] = true
		targets := make([]LocalTarget, 0, len(selection.LocalGroupIDs))
		for _, targetID := range selection.LocalGroupIDs {
			target, e := s.local.Target(ctx, targetID, selection.Platform)
			if e != nil {
				return nil, e
			}
			targets = append(targets, target)
		}
		existing, e := s.local.FindAccount(ctx, key)
		if e != nil {
			return nil, e
		}
		willCreateKey := byMarker[key].KeyCipher == "" && (managed[key] == nil || managed[key].KeyCipher == "")
		p.Rows = append(p.Rows, PreviewRow{Selection: selection, RemoteGroup: g, Target: targets[0], Targets: targets, Existing: existing, Marker: key, WillCreateKey: willCreateKey})
	}
	if e = s.store.SavePreview(ctx, p); e != nil {
		return nil, e
	}
	return p, nil
}

func allApplied(r *ApplyResult) bool {
	if r == nil || len(r.Items) == 0 {
		return false
	}
	for _, i := range r.Items {
		if i.Status != "applied" {
			return false
		}
	}
	return true
}
func accountMatches(a *LocalAccount, row PreviewRow) bool {
	config := row.Selection.AccountConfig
	return a != nil && config != nil && a.Name == row.Selection.AccountName &&
		(config.UpstreamBillingRateSyncEnabled || a.CostMultiplier == row.Selection.CostMultiplier) &&
		sameGroupIDs(a.GroupIDs, row.Selection.LocalGroupIDs) &&
		a.NotesMatchAPIKey && a.BillingProbeEnabled == config.UpstreamBillingRateSyncEnabled && reflect.DeepEqual(a.AccountConfig, config)
}
func (s *Service) Apply(ctx context.Context, id int64, previewID string) (*ApplyResult, error) {
	free, e := s.remoteSlot(ctx)
	if e != nil {
		return nil, e
	}
	defer free()
	site, release, e := s.siteLock(ctx, id)
	if e != nil {
		return nil, e
	}
	defer release()
	p, e := s.store.GetPreview(ctx, id, previewID)
	if e != nil {
		return nil, e
	}
	if p.Result != nil && len(p.Result.Items) == len(p.Rows) && allApplied(p.Result) {
		return p.Result, nil
	}
	snapshot, e := s.store.LatestSnapshot(ctx, id)
	if e != nil {
		return nil, e
	}
	if site.Version != p.SiteVersion || snapshot.ID != p.SnapshotID || !s.now().Before(p.ExpiresAt) {
		return nil, ErrConflict
	}
	session, e := s.managementSessionLocked(ctx, site, false)
	if e != nil {
		return nil, e
	}
	completed := map[string]ItemResult{}
	if p.Result != nil {
		for _, r := range p.Result.Items {
			if r.Status == "applied" {
				completed[r.RemoteGroupID+"\x00"+r.Platform] = r
			}
		}
	}
	// Validate every remaining destination before any remote key creation or local write.
	for _, row := range p.Rows {
		// Previews issued before account settings were included must be refreshed;
		// applying new defaults to an old preview would change its reviewed intent.
		if !reviewedTargets(row) {
			return nil, ErrConflict
		}
		if _, ok := completed[row.Selection.RemoteGroupID+"\x00"+row.Selection.Platform]; ok {
			continue
		}
		for _, frozen := range row.Targets {
			target, e := s.local.Target(ctx, frozen.ID, row.Selection.Platform)
			if e != nil {
				return nil, e
			}
			if target.Fingerprint != frozen.Fingerprint {
				return nil, ErrConflict
			}
		}
		account, e := s.local.FindAccount(ctx, row.Marker)
		if e != nil {
			return nil, e
		}
		if row.Existing != nil {
			if account == nil || (account.Fingerprint != row.Existing.Fingerprint && !accountMatches(account, row)) {
				return nil, ErrConflict
			}
		} else if account != nil && !accountMatches(account, row) {
			return nil, ErrConflict
		}
	}
	bindings, e := s.store.ListBindings(ctx, id)
	if e != nil {
		return nil, e
	}
	byMarker := map[string]Binding{}
	for _, b := range bindings {
		byMarker[b.Marker] = b
	}
	managed, e := s.managedKeysByMarker(ctx, id)
	if e != nil {
		return nil, e
	}
	result := &ApplyResult{PreviewID: p.ID, Items: []ItemResult{}}
	unauthorized := false
	for _, row := range p.Rows {
		key := row.Selection.RemoteGroupID + "\x00" + row.Selection.Platform
		if done, ok := completed[key]; ok {
			result.Items = append(result.Items, done)
			continue
		}
		item := ItemResult{RemoteGroupID: row.Selection.RemoteGroupID, Platform: row.Selection.Platform, Status: "failed"}
		binding := byMarker[row.Marker]
		if binding.Marker == "" {
			binding = Binding{SiteID: id, RemoteGroupID: row.Selection.RemoteGroupID, Platform: row.Selection.Platform, Marker: row.Marker, LocalGroupID: row.Selection.LocalGroupID, LocalGroupIDs: append([]int64(nil), row.Selection.LocalGroupIDs...), ProbeIntervalMinutes: 30, NextProbeAt: s.now()}
		}
		var account *LocalAccount
		applyErr := ErrReauth
		if !unauthorized {
			account, applyErr = s.applyRow(ctx, *site, session, row, &binding, managed[row.Marker])
			if errors.Is(applyErr, ErrReauth) {
				unauthorized = true
				if authErr := s.requireAuthorizationLocked(ctx, site, session); !errors.Is(authErr, ErrReauth) {
					return nil, authErr
				}
			}
		}
		if applyErr != nil {
			item.Error = ErrorCode(applyErr)
		} else {
			item.Status = "applied"
			item.AccountID = account.ID
		}
		result.Items = append(result.Items, item)
		if e = s.store.SavePreviewResult(ctx, id, p.ID, result); e != nil {
			return nil, e
		}
	}
	if e = s.store.AddEvent(ctx, &Event{SiteID: id, Kind: "import_applied", Resource: p.ID, After: fmt.Sprintf("%d items", len(result.Items)), CreatedAt: s.now()}); e != nil {
		return nil, e
	}
	return result, nil
}
func (s *Service) applyRow(ctx context.Context, site Site, session Session, row PreviewRow, binding *Binding, managed *ManagedKey) (*LocalAccount, error) {
	managed, key, _, e := s.ensureManagedKey(ctx, site, session, row.RemoteGroup, row.Selection.Platform, managed, binding)
	if e != nil {
		return nil, e
	}
	if binding.KeyCipher == "" {
		binding.KeyCipher = managed.KeyCipher
		if e = s.store.SaveBinding(ctx, binding); e != nil {
			return nil, e
		}
	}
	expected := ""
	if row.Existing != nil {
		expected = row.Existing.Fingerprint
	}
	// The local adapter checks the full desired state, including key, origin and
	// proxy. It can recover a committed local write without overwriting later edits.
	targetFingerprints := make(map[int64]string, len(row.Targets))
	for _, target := range row.Targets {
		targetFingerprints[target.ID] = target.Fingerprint
	}
	account, e := s.local.ApplyAccount(ctx, AccountChange{Marker: row.Marker, Name: row.Selection.AccountName, Platform: row.Selection.Platform, BaseURL: site.BaseURL, APIKey: key.Key, ExpectedFingerprint: expected, ExpectedTargetFingerprint: row.Target.Fingerprint, ExpectedTargetFingerprints: targetFingerprints, GroupID: row.Selection.LocalGroupID, GroupIDs: append([]int64(nil), row.Selection.LocalGroupIDs...), CostMultiplier: row.Selection.CostMultiplier, ProxyID: site.ProxyID, AccountConfig: row.Selection.AccountConfig})
	if e != nil {
		return nil, e
	}
	binding.AccountID = account.ID
	binding.LocalGroupID = row.Selection.LocalGroupID
	binding.LocalGroupIDs = append([]int64(nil), row.Selection.LocalGroupIDs...)
	if e = s.store.SaveBinding(ctx, binding); e != nil {
		return nil, e
	}
	if e = s.adoptImportedReconciliationState(ctx, site.ID, *binding, row.RemoteGroup.Name); e != nil {
		return nil, e
	}
	return account, nil
}

func canonical(v any) string            { b, _ := json.Marshal(v); return string(b) }
func sortedStrings(s []string) []string { r := append([]string{}, s...); sort.Strings(r); return r }
func groupRates(g RemoteGroup) any {
	return struct {
		Base, User, Resolved, Peak *float64
		Enabled                    bool
		Start, End                 string
	}{g.RateMultiplier, g.UserRateMultiplier, g.ResolvedRateMultiplier, g.PeakRateMultiplier, g.PeakRateEnabled, g.PeakStart, g.PeakEnd}
}
func prices(g RemoteGroup) []RemotePrice {
	p := append([]RemotePrice{}, g.Prices...)
	sort.Slice(p, func(i, j int) bool { return canonical(p[i]) < canonical(p[j]) })
	return p
}
func channels(c Catalog) []RemoteChannel {
	r := append([]RemoteChannel{}, c.Channels...)
	for i := range r {
		r[i].GroupIDs = sortedStrings(r[i].GroupIDs)
		r[i].Models = sortedStrings(r[i].Models)
	}
	sort.Slice(r, func(i, j int) bool { return canonical(r[i]) < canonical(r[j]) })
	return r
}
func DiffCatalog(siteID int64, before, after Catalog) []Event {
	events := []Event{}
	add := func(kind, resource string, b, a any) {
		old, new := canonical(b), canonical(a)
		if old != new {
			events = append(events, Event{SiteID: siteID, Kind: kind, Resource: resource, Before: old, After: new})
		}
	}
	old := map[string]RemoteGroup{}
	for _, g := range before.Groups {
		old[g.ID] = g
	}
	for _, g := range after.Groups {
		prior, ok := old[g.ID]
		if !ok {
			add("group_added", g.ID, nil, g)
		} else {
			add("rate_changed", g.ID, groupRates(prior), groupRates(g))
			add("models_changed", g.ID, sortedStrings(prior.Models), sortedStrings(g.Models))
			add("price_changed", g.ID, prices(prior), prices(g))
			if prior.Name != g.Name || prior.Platform != g.Platform {
				add("group_changed", g.ID, []string{prior.Name, prior.Platform}, []string{g.Name, g.Platform})
			}
		}
		delete(old, g.ID)
	}
	for id, g := range old {
		add("group_removed", id, g, nil)
	}
	add("channels_changed", "channels", channels(before), channels(after))
	sort.Slice(events, func(i, j int) bool { return events[i].Kind+events[i].Resource < events[j].Kind+events[j].Resource })
	return events
}
