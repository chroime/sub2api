package upstreamgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type reconciliationMemoryStore struct {
	*memoryStore
	config    AutomationConfig
	states    map[int64]ReconciliationState
	plans     map[string]*ReconciliationPreview
	failState bool
}

func (m *reconciliationMemoryStore) GetAutomation(context.Context, int64) (AutomationConfig, error) {
	return m.config, nil
}
func (m *reconciliationMemoryStore) SaveAutomation(_ context.Context, _ int64, c AutomationConfig) (AutomationConfig, error) {
	if c.Version != m.config.Version {
		return c, ErrConflict
	}
	c.Version++
	m.config = c
	return c, nil
}
func (m *reconciliationMemoryStore) ReconciliationStates(context.Context, int64) (map[int64]ReconciliationState, error) {
	out := map[int64]ReconciliationState{}
	for id, state := range m.states {
		out[id] = state
	}
	return out, nil
}
func (m *reconciliationMemoryStore) SaveReconciliationState(_ context.Context, _ int64, s ReconciliationState) error {
	if m.failState {
		m.failState = false
		return errors.New("state storage interrupted")
	}
	m.states[s.BindingID] = s
	return nil
}
func (m *reconciliationMemoryStore) SaveReconciliationPreview(_ context.Context, _ int64, p *ReconciliationPreview) error {
	m.plans[p.ID] = p
	return nil
}
func (m *reconciliationMemoryStore) GetReconciliationPreview(_ context.Context, _ int64, id string) (*ReconciliationPreview, error) {
	p, ok := m.plans[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}
func (m *reconciliationMemoryStore) SaveReconciliationResult(_ context.Context, _ int64, id string, r *ReconciliationResult) error {
	copyResult := *r
	copyResult.Items = append([]ReconciliationItemResult(nil), r.Items...)
	m.plans[id].Result = &copyResult
	return nil
}

type reconciliationLocalFixture struct {
	LocalAccounts
	account    ManagedLocalAccount
	calls      int
	failPatch  bool
	afterApply func()
}

func (l *reconciliationLocalFixture) InspectManagedAccount(_ context.Context, b Binding) (*ManagedLocalAccount, error) {
	if b.AccountID != l.account.ID {
		return nil, ErrNotFound
	}
	v := l.account
	return &v, nil
}
func (l *reconciliationLocalFixture) ApplyManagedPatch(ctx context.Context, p ManagedAccountPatch) (*ManagedLocalAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.failPatch {
		return nil, errors.New("fixture patch failure")
	}
	if ManagedPatchAlreadyApplied(l.account, p) {
		a := l.account
		return &a, nil
	}
	if l.account.Identity != p.Expected.Identity {
		return nil, ErrConflict
	}
	if p.Name != nil {
		l.account.Name = *p.Name
	}
	if p.Rate != nil {
		l.account.Rate = *p.Rate
	}
	if p.RateOwner != nil {
		l.account.RateOwner = *p.RateOwner
	}
	if p.Availability == "pause" {
		l.account.Schedulable = false
		l.account.PauseToken = p.OperationID
		l.account.PauseMarker = p.Marker
		l.account.PauseIdentity = l.account.Identity
		l.account.PauseReason = p.PauseReason
	}
	if p.Availability == "restore" {
		l.account.Schedulable = true
		l.account.PauseToken = ""
		l.account.PauseMarker = ""
		l.account.PauseIdentity = ""
		l.account.PauseReason = ""
	}
	l.account.Receipt = p.OperationID
	l.calls++
	if l.afterApply != nil {
		l.afterApply()
	}
	a := l.account
	return &a, nil
}
func reconciliationFixture(t *testing.T) (*Service, *reconciliationMemoryStore, *fakeConnector, *reconciliationLocalFixture, *time.Time) {
	s, m, c, _ := setupEngine(t)
	now := time.Now().UTC()
	m.site.IntervalMinutes = 5
	m.bindings = []Binding{{ID: 2, SiteID: 1, AccountID: 3, Marker: "owned", RemoteGroupID: "8", Platform: "openai", LocalGroupIDs: []int64{7, 9}, LocalGroupID: 7}}
	m.bindings[0].KeyCipher, _ = fakeCipher{}.Encrypt(`{"id":"fixture","key":"fixture-key"}`)
	store := &reconciliationMemoryStore{memoryStore: m, config: DefaultAutomationConfig(), states: map[int64]ReconciliationState{}, plans: map[string]*ReconciliationPreview{}}
	l := &reconciliationLocalFixture{account: ManagedLocalAccount{ID: 3, Identity: "id", Name: m.site.BaseURL + "--0.8", Rate: 0.8, Status: "active", Schedulable: true, CanRestore: true, NativeRateSync: true}}
	l.account.Identity = ManagedAccountIdentity(3, "owned", "openai", m.site.BaseURL, "fixture-key")
	c.catalog.GroupsComplete = true
	s.store = store
	s.local = l
	s.now = func() time.Time { return now }
	return s, store, c, l, &now
}
func TestReconciliationAutomationRequiresPolicyAndSkipsLargeIncrease(t *testing.T) {
	s, m, c, l, now := reconciliationFixture(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, l.calls)
	cfg := m.config
	cfg.Policy.Enabled = true
	_, err = s.ConfigureAutomation(t.Context(), 1, cfg)
	require.NoError(t, err)
	require.Zero(t, l.calls, "saving policy must not modify accounts")
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "owned", l.account.RateOwner)
	rate := 2.0
	c.catalog.Groups[0].ResolvedRateMultiplier = &rate
	*now = now.Add(5 * time.Minute)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.8, l.account.Rate)
	r, err := s.Reconciliation(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "review", r.Rows[0].State)
	p, err := s.PreviewReconciliation(t.Context(), 1)
	require.NoError(t, err)
	out, err := s.ApplyReconciliation(t.Context(), 1, p.ID, []int64{2})
	require.NoError(t, err)
	require.Equal(t, "applied", out.Items[0].Status)
	require.Equal(t, 2.0, l.account.Rate)
	calls := l.calls
	_, err = s.ApplyReconciliation(t.Context(), 1, p.ID, []int64{2})
	require.NoError(t, err)
	require.Equal(t, calls, l.calls)
	cfg = m.config
	cfg.Policy.Enabled = false
	_, err = s.ConfigureAutomation(t.Context(), 1, cfg)
	require.NoError(t, err)
	require.Equal(t, "owned", l.account.RateOwner)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, l.account.RateOwner)
	require.Equal(t, 2.0, l.account.Rate)
	require.Zero(t, c.keyCalls)
}
func TestReconciliationOnlySuccessfulSpacedCollectionsPauseAndRestore(t *testing.T) {
	s, m, c, l, now := reconciliationFixture(t)
	m.config.Policy.Enabled = true
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	group := c.catalog.Groups[0]
	c.catalog.Groups = []RemoteGroup{}
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, m.states[2].MissingCount)
	for i := 0; i < 3; i++ {
		_, err = s.Reconciliation(t.Context(), 1)
		require.NoError(t, err)
		_, err = s.PreviewReconciliation(t.Context(), 1)
		require.NoError(t, err)
	}
	require.Equal(t, 1, m.states[2].MissingCount)
	require.True(t, l.account.Schedulable)
	*now = now.Add(5 * time.Minute)
	c.err = ErrReauth
	_, err = s.Sync(t.Context(), 1)
	require.ErrorIs(t, err, ErrReauth)
	require.Equal(t, 1, m.states[2].MissingCount)
	c.err = nil
	c.catalog.GroupsComplete = false
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, l.account.Schedulable)
	c.catalog.GroupsComplete = true
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, l.account.Schedulable)
	require.NotEmpty(t, l.account.PauseToken)
	c.catalog.Groups = []RemoteGroup{group}
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, l.account.Schedulable)
	require.Empty(t, l.account.PauseToken)
}
func TestReconciliationRecoversCommittedAccountAfterPreviewExpiryWithoutNewWrite(t *testing.T) {
	s, m, c, l, now := reconciliationFixture(t)
	m.config.Policy.Enabled = true
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	rate := 0.9
	c.catalog.Groups[0].ResolvedRateMultiplier = &rate
	l.afterApply = func() { m.failState = true; l.afterApply = nil }
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.9, l.account.Rate)
	require.Equal(t, 0.8, m.states[2].Rate)
	calls := l.calls
	*now = now.Add(20 * time.Minute)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.9, m.states[2].Rate)
	require.Equal(t, calls, l.calls)
	for _, plan := range m.plans {
		if plan.ID+":2" == l.account.Receipt {
			require.Equal(t, "applied", plan.Result.Items[0].Status)
		}
	}
}

func TestReconciliationRefusesFirstAdoptionOfChangedUpstreamIdentity(t *testing.T) {
	s, m, _, l, _ := reconciliationFixture(t)
	m.config.Policy.Enabled = true
	l.account.Identity = ManagedAccountIdentity(l.account.ID, "owned", "openai", "https://different.example", "different-key")
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, l.calls)
	require.Empty(t, m.states)
	r, err := s.Reconciliation(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "conflict", r.Rows[0].State)
	require.Equal(t, "account_identity_changed", r.Rows[0].Reason)
}

func TestReconciliationRecoversRateOnlyCommitDespiteUntouchedNameEdit(t *testing.T) {
	s, m, c, l, now := reconciliationFixture(t)
	m.config.Policy.Enabled = true
	m.config.Policy.SyncName = false
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	rate := 0.9
	c.catalog.Groups[0].ResolvedRateMultiplier = &rate
	l.afterApply = func() { l.account.Name = "manual name"; m.failState = true; l.afterApply = nil }
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.9, l.account.Rate)
	require.Equal(t, 0.8, m.states[2].Rate)
	calls := l.calls
	*now = now.Add(20 * time.Minute)
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.9, m.states[2].Rate)
	require.Equal(t, calls, l.calls)
	require.Equal(t, "manual name", l.account.Name)
	require.Equal(t, m.site.BaseURL+"--0.8", m.states[2].Name, "unowned name edit must not be silently adopted")
}

func TestReconciliationPauseOnlyDoesNotAdoptConcurrentManualRate(t *testing.T) {
	s, m, c, l, now := reconciliationFixture(t)
	m.config.Policy.Enabled = true
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	group := c.catalog.Groups[0]
	c.catalog.Groups = []RemoteGroup{}
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	*now = now.Add(5 * time.Minute)
	l.afterApply = func() { l.account.Rate = 0.7; l.afterApply = nil }
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, l.account.Schedulable)
	require.Equal(t, 0.8, m.states[2].Rate)
	c.catalog.Groups = []RemoteGroup{group}
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	r, err := s.Reconciliation(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "managed_rate_changed", r.Rows[0].Reason)
	require.Equal(t, 0.7, l.account.Rate)
	require.False(t, l.account.Schedulable)
}
func TestReconciliationReadAndApplyRejectStaleCatalogAndPolicy(t *testing.T) {
	s, m, c, _, now := reconciliationFixture(t)
	_, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	rate := 0.9
	c.catalog.Groups[0].ResolvedRateMultiplier = &rate
	_, err = s.Sync(t.Context(), 1)
	require.NoError(t, err)
	p, err := s.PreviewReconciliation(t.Context(), 1)
	require.NoError(t, err)
	*now = now.Add(11 * time.Minute)
	r, err := s.Reconciliation(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "catalog_stale", r.Rows[0].Reason)
	_, err = s.ApplyReconciliation(t.Context(), 1, p.ID, []int64{2})
	require.ErrorIs(t, err, ErrConflict)
	*now = now.Add(-11 * time.Minute)
	_, err = s.ConfigureAutomation(t.Context(), 1, m.config)
	require.NoError(t, err)
	_, err = s.ApplyReconciliation(t.Context(), 1, p.ID, []int64{2})
	require.ErrorIs(t, err, ErrConflict)
}
