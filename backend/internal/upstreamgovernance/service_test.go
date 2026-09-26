package upstreamgovernance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	Store
	site                    Site
	snap                    *Snapshot
	previews                map[string]*Preview
	bindings                []Binding
	events                  []Event
	checks                  []Check
	locked                  bool
	failBindingAfterAccount bool
	failResult              bool
}

func (m *memoryStore) GetSite(context.Context, int64) (*Site, error) { s := m.site; return &s, nil }
func (m *memoryStore) ListSites(context.Context) ([]Site, error)     { return []Site{m.site}, nil }
func (m *memoryStore) CreateSite(_ context.Context, s *Site) error {
	s.ID = 1
	s.Version = 1
	m.site = *s
	return nil
}
func (m *memoryStore) UpdateSite(_ context.Context, s *Site, v int64) error {
	if m.site.Version != v {
		return ErrConflict
	}
	s.Version = v + 1
	m.site = *s
	return nil
}
func (m *memoryStore) LockSite(context.Context, int64) (func(), bool, error) {
	if m.locked {
		return func() {}, false, nil
	}
	m.locked = true
	return func() { m.locked = false }, true, nil
}
func (m *memoryStore) ObserveSite(ctx context.Context, _ int64, state, code string, at, next time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.site.Status = state
	m.site.LastError = code
	if !at.IsZero() {
		m.site.LastSyncAt = &at
	}
	m.site.NextSyncAt = next
	return nil
}
func (m *memoryStore) LatestSnapshot(context.Context, int64) (*Snapshot, error) {
	if m.snap == nil {
		return nil, ErrNotFound
	}
	s := *m.snap
	return &s, nil
}
func (m *memoryStore) SaveSnapshot(_ context.Context, s *Snapshot, events []Event) error {
	s.ID = 1
	if m.snap != nil {
		s.ID = m.snap.ID + 1
	}
	m.snap = s
	m.events = append(m.events, events...)
	return nil
}
func (m *memoryStore) AddEvent(_ context.Context, e *Event) error {
	m.events = append(m.events, *e)
	return nil
}
func (m *memoryStore) SavePreview(_ context.Context, p *Preview) error {
	m.previews[p.ID] = p
	return nil
}
func (m *memoryStore) GetPreview(_ context.Context, _ int64, id string) (*Preview, error) {
	p, ok := m.previews[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}
func (m *memoryStore) SavePreviewResult(_ context.Context, _ int64, id string, r *ApplyResult) error {
	if m.failResult {
		return errors.New("fixture result write failure")
	}
	copyResult := *r
	copyResult.Items = append([]ItemResult(nil), r.Items...)
	m.previews[id].Result = &copyResult
	return nil
}
func (m *memoryStore) ListBindings(context.Context, int64) ([]Binding, error) {
	return append([]Binding(nil), m.bindings...), nil
}
func (m *memoryStore) SaveBinding(_ context.Context, b *Binding) error {
	if m.failBindingAfterAccount && b.AccountID > 0 {
		return errors.New("fixture binding write failure")
	}
	for i := range m.bindings {
		if m.bindings[i].Marker == b.Marker {
			b.ID = m.bindings[i].ID
			m.bindings[i] = *b
			return nil
		}
	}
	b.ID = int64(len(m.bindings) + 1)
	m.bindings = append(m.bindings, *b)
	return nil
}
func (m *memoryStore) AddCheck(_ context.Context, c *Check) error {
	m.checks = append(m.checks, *c)
	return nil
}

type fakeCipher struct{}

func (fakeCipher) Encrypt(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}
func (fakeCipher) Decrypt(s string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	return string(b), e
}

type fakeConnector struct {
	catalog        Catalog
	err            error
	keyCalls       int
	challenge      *Challenge
	discoveryCalls int
	probeCalls     int
	probeResult    *ProbeResult
}

func (f *fakeConnector) Login(context.Context, Site, LoginInput) (Session, *Challenge, error) {
	return Session{AccessToken: "fixture-session", UserID: 5}, f.challenge, f.err
}
func (f *fakeConnector) Discover(context.Context, Site, Session) (Catalog, error) {
	f.discoveryCalls++
	return f.catalog, f.err
}
func (f *fakeConnector) EnsureKey(context.Context, Site, Session, RemoteGroup, string) (RemoteKey, error) {
	f.keyCalls++
	return RemoteKey{ID: "1", Key: "fixture-inference-key"}, f.err
}
func (f *fakeConnector) Probe(context.Context, Site, RemoteKey, string, string) (ProbeResult, error) {
	f.probeCalls++
	if f.probeResult != nil {
		return *f.probeResult, f.err
	}
	return ProbeResult{Success: true, LatencyMS: 12}, f.err
}

type fakeLocal struct {
	accounts          map[string]*LocalAccount
	calls             int
	fail              bool
	targetFingerprint string
	changes           []AccountChange
}

func (f *fakeLocal) Target(context.Context, int64, string) (LocalTarget, error) {
	return LocalTarget{ID: 7, Name: "Local", Platform: "openai", SaleMultiplier: 3, Fingerprint: f.targetFingerprint}, nil
}
func (f *fakeLocal) FindAccount(_ context.Context, marker string) (*LocalAccount, error) {
	return f.accounts[marker], nil
}
func (f *fakeLocal) ApplyAccount(_ context.Context, c AccountChange) (*LocalAccount, error) {
	f.calls++
	f.changes = append(f.changes, c)
	if f.fail {
		return nil, errors.New("fixture failure secret")
	}
	a := &LocalAccount{ID: 10, Name: c.Name, GroupIDs: []int64{c.GroupID}, CostMultiplier: c.CostMultiplier, Fingerprint: "updated"}
	f.accounts[c.Marker] = a
	return a, nil
}

func TestApplyDelegatesFullAccountComparisonWhenOnlyProxyChanges(t *testing.T) {
	s, m, c, l := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	_, e = s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	proxyID := int64(44)
	m.site.ProxyID = &proxyID
	m.site.Version++
	_, e = s.Sync(t.Context(), 1)
	require.NoError(t, e)
	p, e = s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	_, e = s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Len(t, l.changes, 2, "unchanged name/cost/group must not bypass full credentials and proxy comparison")
	require.Equal(t, &proxyID, l.changes[1].ProxyID)
	require.Equal(t, 1, c.keyCalls)
}

func TestApplyRecoversUpdatedAccountAfterBindingPersistenceFailure(t *testing.T) {
	s, m, c, l := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	key := marker(1, "8", "openai")
	l.accounts[key] = &LocalAccount{ID: 10, Name: "Before", GroupIDs: []int64{7}, CostMultiplier: 2, Fingerprint: "old"}
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	m.failBindingAfterAccount = true
	r, e := s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Equal(t, "failed", r.Items[0].Status)
	require.Equal(t, "updated", l.accounts[key].Fingerprint)
	m.failBindingAfterAccount = false
	r, e = s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e, "adapter can recover a matching desired state after a successful local write")
	require.Equal(t, "applied", r.Items[0].Status)
	require.Equal(t, "old", l.changes[len(l.changes)-1].ExpectedFingerprint)
	require.Equal(t, 1, c.keyCalls)
}

func TestApplyRejectsManualAccountChangeBeforeRemoteEffects(t *testing.T) {
	s, _, c, l := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	key := marker(1, "8", "openai")
	l.accounts[key] = &LocalAccount{ID: 10, Name: "Before", GroupIDs: []int64{7}, CostMultiplier: 2, Fingerprint: "old"}
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	l.accounts[key] = &LocalAccount{ID: 10, Name: "Manually changed", GroupIDs: []int64{7}, CostMultiplier: 4, Fingerprint: "manual"}
	_, e = s.Apply(t.Context(), 1, p.ID)
	require.ErrorIs(t, e, ErrConflict)
	require.Zero(t, c.keyCalls)
	require.Zero(t, l.calls)
}

func TestApplyResumesAfterOnlySomeSuccessfulRowsWerePersisted(t *testing.T) {
	s, m, c, l := setupEngine(t)
	g := c.catalog.Groups[0]
	g.ID = "9"
	c.catalog.Groups = append(c.catalog.Groups, g)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	selected := append(selections(), Selection{RemoteGroupID: "9", Platform: "openai", LocalGroupID: 7, AccountName: "Second", CostMultiplier: 0.8})
	p, e := s.Preview(t.Context(), 1, selected)
	require.NoError(t, e)
	m.previews[p.ID].Result = &ApplyResult{PreviewID: p.ID, Items: []ItemResult{{RemoteGroupID: "8", Platform: "openai", AccountID: 10, Status: "applied"}}}
	r, e := s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Len(t, r.Items, 2)
	require.Equal(t, "applied", r.Items[1].Status)
	require.Equal(t, 1, l.calls)
}
func setupEngine(t *testing.T) (*Service, *memoryStore, *fakeConnector, *fakeLocal) {
	t.Helper()
	cipher := fakeCipher{}
	session, _ := json.Marshal(Session{AccessToken: "fixture-session", UserID: 5})
	encrypted, _ := cipher.Encrypt(string(session))
	rate := 0.8
	m := &memoryStore{site: Site{ID: 1, Name: "Upstream", Platform: "sub2api", BaseURL: "https://upstream.example", Enabled: true, IntervalMinutes: 15, Version: 1, SessionCipher: encrypted, HasCredential: true}, previews: map[string]*Preview{}}
	c := &fakeConnector{catalog: Catalog{Groups: []RemoteGroup{{ID: "8", Name: "Remote", Platform: "openai", ResolvedRateMultiplier: &rate, Models: []string{"gpt-fixture"}, Source: "user"}}}}
	l := &fakeLocal{accounts: map[string]*LocalAccount{}, targetFingerprint: "group-v1"}
	return NewService(m, c, l, cipher, true), m, c, l
}
func selections() []Selection {
	return []Selection{{RemoteGroupID: "8", Platform: "openai", LocalGroupID: 7, AccountName: "Imported", CostMultiplier: 0.8}}
}

func TestDiscoveryNeverAppliesLocalConfigurationAndPreservesSnapshotOnFailure(t *testing.T) {
	s, m, c, l := setupEngine(t)
	snap, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	require.Len(t, snap.Catalog.Groups, 1)
	c.err = ErrReauth
	_, e = s.Sync(t.Context(), 1)
	require.ErrorIs(t, e, ErrReauth)
	require.Equal(t, snap.ID, m.snap.ID)
	require.Equal(t, "reauth_required", m.site.Status)
	require.Zero(t, c.keyCalls)
	require.Zero(t, l.calls)
}
func TestPreviewThenApplyIsExplicitAndIdempotent(t *testing.T) {
	s, m, c, l := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	require.Zero(t, c.keyCalls)
	require.Zero(t, l.calls)
	require.Equal(t, 3.0, p.Rows[0].Target.SaleMultiplier)
	r, e := s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Equal(t, "applied", r.Items[0].Status)
	require.Equal(t, 0.8, l.accounts[p.Rows[0].Marker].CostMultiplier)
	r2, e := s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Equal(t, r, r2)
	require.Equal(t, 1, c.keyCalls)
	require.Equal(t, 1, l.calls)
	b, _ := json.Marshal(m.bindings[0])
	require.NotContains(t, string(b), "fixture-inference-key")
	require.NotContains(t, string(b), m.bindings[0].KeyCipher)
}
func TestStaleCatalogAndChangedLocalGroupRejectBeforeSideEffects(t *testing.T) {
	for _, changed := range []string{"catalog", "local-group", "site"} {
		t.Run(changed, func(t *testing.T) {
			s, m, c, l := setupEngine(t)
			_, e := s.Sync(t.Context(), 1)
			require.NoError(t, e)
			p, e := s.Preview(t.Context(), 1, selections())
			require.NoError(t, e)
			switch changed {
			case "catalog":
				m.snap.ID++
			case "site":
				m.site.Version++
			case "local-group":
				l.targetFingerprint = "group-v2"
			}
			_, e = s.Apply(t.Context(), 1, p.ID)
			require.ErrorIs(t, e, ErrConflict)
			require.Zero(t, c.keyCalls)
			require.Zero(t, l.calls)
		})
	}
}
func TestPartialFailureRetainsKeyAndCanResume(t *testing.T) {
	s, _, c, l := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	l.fail = true
	r, e := s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Equal(t, "failed", r.Items[0].Status)
	require.NotContains(t, r.Items[0].Error, "secret")
	l.fail = false
	r, e = s.Apply(t.Context(), 1, p.ID)
	require.NoError(t, e)
	require.Equal(t, "applied", r.Items[0].Status)
	require.Equal(t, 1, c.keyCalls)
}
func TestConnectDoesNotPersistPasswordOrChallenge(t *testing.T) {
	s, m, c, _ := setupEngine(t)
	old := m.site.SessionCipher
	c.challenge = &Challenge{Kind: "totp", Token: "fixture-challenge"}
	r, e := s.Connect(t.Context(), 1, LoginInput{Username: "fixture", Password: "fixture-password"})
	require.NoError(t, e)
	require.NotNil(t, r.Challenge)
	require.Equal(t, old, m.site.SessionCipher)
	c.challenge = nil
	r, e = s.Connect(t.Context(), 1, LoginInput{Username: "fixture", Password: "fixture-password"})
	require.NoError(t, e)
	raw, _ := json.Marshal(r)
	require.NotContains(t, string(raw), "fixture-password")
	require.NotContains(t, string(raw), "fixture-session")
	plain, _ := fakeCipher{}.Decrypt(m.site.SessionCipher)
	require.NotContains(t, plain, "password")
}
func TestInvalidCatalogCannotErasePriorGoodSnapshot(t *testing.T) {
	s, m, c, _ := setupEngine(t)
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	id := m.snap.ID
	c.catalog.Groups = append(c.catalog.Groups, c.catalog.Groups[0])
	_, e = s.Sync(t.Context(), 1)
	require.Error(t, e)
	require.Equal(t, id, m.snap.ID)
}
func TestUnsafeSitesAndMissingDurableEncryptionAreRejected(t *testing.T) {
	s, _, _, _ := setupEngine(t)
	for _, u := range []string{"http://example.com", "https://127.0.0.1", "https://example.com?token=fixture", "https://user:pass@example.com", "https://localhost", "https://[::1]"} {
		_, e := s.CreateSite(t.Context(), Site{Name: "s", Platform: "sub2api", BaseURL: u, IntervalMinutes: 15})
		require.Error(t, e, u)
	}
	s.durableKey = false
	_, e := s.Connect(t.Context(), 1, LoginInput{})
	require.ErrorIs(t, e, ErrEncryption)
}

func TestUnknownUpstreamTransportCanBeExplicitlyMapped(t *testing.T) {
	s, _, c, _ := setupEngine(t)
	c.catalog.Groups[0].Platform = "unknown"
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	p, e := s.Preview(t.Context(), 1, selections())
	require.NoError(t, e)
	require.Equal(t, "openai", p.Rows[0].Selection.Platform)
}

func TestPreviewShowsDatabaseCostPrecisionAndDiscoveryPreservesLargeRates(t *testing.T) {
	s, _, c, _ := setupEngine(t)
	rate := 200.123456
	c.catalog.Groups[0].ResolvedRateMultiplier = &rate
	_, e := s.Sync(t.Context(), 1)
	require.NoError(t, e)
	selected := selections()
	selected[0].CostMultiplier = rate
	p, e := s.Preview(t.Context(), 1, selected)
	require.NoError(t, e)
	require.Equal(t, 200.1235, p.Rows[0].Selection.CostMultiplier, "preview the exact numeric(10,4) account cost that will persist")
	require.Equal(t, rate, *p.Rows[0].RemoteGroup.ResolvedRateMultiplier, "remote observations remain unchanged")
}

func TestUnsupportedInteractiveChallengeIsReturnedWithoutPersistingSession(t *testing.T) {
	s, m, c, _ := setupEngine(t)
	old := m.site.SessionCipher
	c.challenge = &Challenge{Kind: "interactive_verification"}
	c.err = ErrUnsupported
	r, e := s.Connect(t.Context(), 1, LoginInput{Password: "fixture"})
	require.NoError(t, e)
	require.Equal(t, "interactive_verification", r.Challenge.Kind)
	require.Equal(t, old, m.site.SessionCipher)
}
func TestDiffIgnoresOrderingAndReportsRatesModelsAndChannels(t *testing.T) {
	a := 0.8
	b := 1.0
	before := Catalog{Groups: []RemoteGroup{{ID: "g", Models: []string{"b", "a"}, ResolvedRateMultiplier: &a}}, Channels: []RemoteChannel{{Name: "c", GroupIDs: []string{"g"}}}}
	same := Catalog{Groups: []RemoteGroup{{ID: "g", Models: []string{"a", "b"}, ResolvedRateMultiplier: &a}}, Channels: before.Channels}
	require.Empty(t, DiffCatalog(1, before, same))
	same.Groups[0].ResolvedRateMultiplier = &b
	same.Groups[0].Models = []string{"c"}
	same.Channels = nil
	events := DiffCatalog(1, before, same)
	all, _ := json.Marshal(events)
	require.True(t, strings.Contains(string(all), "rate_changed"))
	require.True(t, strings.Contains(string(all), "models_changed"))
	require.True(t, strings.Contains(string(all), "channels_changed"))
}
