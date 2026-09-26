package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type governanceAdminStub struct {
	account          *Account
	group            *Group
	creates, updates int
	input            UpdateAccountInput
}

func (s *governanceAdminStub) GetGroup(context.Context, int64) (*Group, error) { return s.group, nil }
func (s *governanceAdminStub) GetAccount(context.Context, int64) (*Account, error) {
	return s.account, nil
}
func (s *governanceAdminStub) CreateAccount(_ context.Context, in *CreateAccountInput) (*Account, error) {
	s.creates++
	a, e := buildAccountForCreate(in, in.Extra)
	if e != nil {
		return nil, e
	}
	a.ID = 9
	a.GroupIDs = in.GroupIDs
	s.account = a
	return a, nil
}
func (s *governanceAdminStub) UpdateAccount(_ context.Context, id int64, in *UpdateAccountInput) (*Account, error) {
	s.updates++
	s.input = *in
	a := s.account
	a.Name = in.Name
	a.Notes = in.Notes
	a.Credentials = in.Credentials
	if in.Extra != nil {
		a.Extra = in.Extra
	}
	if in.Concurrency != nil {
		a.Concurrency = *in.Concurrency
	}
	a.GroupIDs = *in.GroupIDs
	if in.RateMultiplier != nil {
		if in.RateSyncEnabled != nil && *in.RateSyncEnabled {
			return nil, ErrUpstreamBillingRateSyncConflict
		}
		a.RateMultiplier = in.RateMultiplier
	}
	if *in.ProxyID == 0 {
		a.ProxyID = nil
	} else {
		a.ProxyID = in.ProxyID
	}
	a.Extra[UpstreamBillingProbeEnabledExtraKey] = *in.ProbeEnabled
	a.Extra[UpstreamBillingRateSyncEnabledExtraKey] = *in.RateSyncEnabled
	return a, nil
}
func TestGovernanceAccountRecoveryChecksCredentialsAndFingerprint(t *testing.T) {
	db, mock, e := sqlmock.New()
	require.NoError(t, e)
	defer db.Close()
	rate := 2.0
	key := "canary-key"
	a := &Account{ID: 9, Name: "import", Notes: &key, Concurrency: 5000, Platform: "openai", Type: "apikey", Status: StatusActive, Credentials: map[string]any{"api_key": "canary-key", "base_url": "https://fixture.example", "custom": "keep"}, Extra: map[string]any{governanceMarkerKey: "marker", "unrelated": "keep", "quota_daily_limit": 10000.0, "quota_weekly_limit": 700000.0, "quota_limit": 10000000.0, openAILongContextBillingEnabledKey: true, UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: true}, GroupIDs: []int64{3}, RateMultiplier: &rate}
	admin := &governanceAdminStub{account: a, group: &Group{ID: 3, Platform: "openai", Status: StatusActive}}
	local := &governanceLocalAccounts{db: db, admin: admin}
	change := gov.AccountChange{Marker: "marker", Name: "import", Platform: "openai", BaseURL: "https://fixture.example", APIKey: "canary-key", GroupID: 3, CostMultiplier: 2}
	expect := func() {
		mock.ExpectQuery("SELECT id FROM accounts").WithArgs("marker").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	}
	expect()
	v, e := local.ApplyAccount(context.Background(), change)
	require.NoError(t, e)
	require.EqualValues(t, 9, v.ID)
	require.Zero(t, admin.updates)
	raw, _ := json.Marshal(v)
	require.NotContains(t, string(raw), "canary-key")
	change.APIKey = "changed-key"
	expect()
	_, e = local.ApplyAccount(context.Background(), change)
	require.ErrorIs(t, e, gov.ErrConflict)
	require.Zero(t, admin.updates)
	change.ExpectedFingerprint = governanceLocal(a).Fingerprint
	expect()
	_, e = local.ApplyAccount(context.Background(), change)
	require.NoError(t, e)
	require.Equal(t, 1, admin.updates)
	require.Equal(t, "keep", a.Credentials["custom"])
	require.Equal(t, "keep", a.Extra["unrelated"])
	require.True(t, *admin.input.ProbeEnabled)
	require.True(t, *admin.input.RateSyncEnabled)
	require.Nil(t, admin.input.RateMultiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestGovernanceFingerprintTracksSecretsProxyAndTarget(t *testing.T) {
	a := &Account{Credentials: map[string]any{"api_key": "one"}, GroupIDs: []int64{3}}
	before := governanceLocal(a).Fingerprint
	a.Credentials["api_key"] = "two"
	require.NotEqual(t, before, governanceLocal(a).Fingerprint)
	before = governanceLocal(a).Fingerprint
	proxy := int64(7)
	a.ProxyID = &proxy
	require.NotEqual(t, before, governanceLocal(a).Fingerprint)
	local := &governanceLocalAccounts{admin: &governanceAdminStub{group: &Group{ID: 3, Status: StatusActive, Platform: "anthropic"}}}
	_, e := local.Target(context.Background(), 3, "openai")
	require.ErrorIs(t, e, gov.ErrInvalid)
}
func TestGovernanceCreateEnablesDeclaredRateSync(t *testing.T) {
	db, m, e := sqlmock.New()
	require.NoError(t, e)
	defer db.Close()
	m.ExpectQuery("SELECT id FROM accounts").WithArgs("marker").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	admin := &governanceAdminStub{group: &Group{ID: 3, Platform: "composite", Status: StatusActive, RateMultiplier: 5}}
	local := &governanceLocalAccounts{db: db, admin: admin}
	_, e = local.ApplyAccount(context.Background(), gov.AccountChange{Marker: "marker", Name: "import", Platform: "openai", BaseURL: "https://fixture.example", APIKey: "key", GroupID: 3, CostMultiplier: 2})
	require.NoError(t, e)
	require.True(t, upstreamBillingRateSyncEnabled(admin.account))
	require.Equal(t, 5.0, admin.group.RateMultiplier)
	require.Equal(t, 2.0, admin.account.BillingRateMultiplier())
	require.NoError(t, m.ExpectationsWereMet())
}

type governanceProxyStub struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (p governanceProxyStub) GetByID(context.Context, int64) (*Proxy, error) { return p.proxy, p.err }

type governanceTransportStub struct {
	HTTPUpstream
	t     *testing.T
	calls int
	proxy string
}

func (s *governanceTransportStub) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	s.calls++
	s.proxy = proxy
	require.True(s.t, HTTPUpstreamRedirectsDisabled(req.Context()))
	require.True(s.t, HTTPUpstreamPublicHostsOnly(req.Context()))
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}
func TestGovernanceTransportRequiresSelectedProxy(t *testing.T) {
	id := int64(7)
	transport := &governanceTransportStub{t: t}
	site := gov.Site{ProxyID: &id}
	for _, proxy := range []*Proxy{nil, {Status: StatusActive, Host: "proxy.example", Port: 8080, Protocol: "invalid"}, {Status: "inactive", Host: "proxy.example", Port: 8080, Protocol: "http"}} {
		_, e := governanceClientFactory(transport, governanceProxyStub{proxy: proxy})(context.Background(), site)
		require.ErrorIs(t, e, gov.ErrInvalid)
	}
	_, deletedErr := governanceClientFactory(transport, governanceProxyStub{err: ErrProxyNotFound})(context.Background(), site)
	require.ErrorIs(t, deletedErr, gov.ErrInvalid)
	require.Zero(t, transport.calls)
	client, e := governanceClientFactory(transport, governanceProxyStub{proxy: &Proxy{Status: StatusActive, Host: "proxy.example", Port: 8080, Protocol: "http"}})(context.Background(), site)
	require.NoError(t, e)
	req, e := http.NewRequest("GET", "https://fixture.example/", nil)
	require.NoError(t, e)
	resp, e := client.Do(req)
	require.NoError(t, e)
	resp.Body.Close()
	require.Equal(t, "http://proxy.example:8080", transport.proxy)
}

type governanceAtomicCreateStub struct {
	calls  int
	groups []AccountGroup
	err    error
}

func (s *governanceAtomicCreateStub) CreateWithAccountGroups(_ context.Context, a *Account, groups []AccountGroup) error {
	s.calls++
	s.groups = groups
	if s.err != nil {
		return s.err
	}
	a.ID = 19
	a.GroupIDs = []int64{groups[0].GroupID}
	return nil
}
func TestGovernanceAdminCreateUsesAtomicAccountAndBindingPath(t *testing.T) {
	for _, failure := range []bool{false, true} {
		repo := &governanceAtomicCreateStub{}
		if failure {
			repo.err = errors.New("binding failed")
		}
		admin := &adminServiceImpl{accountDuplicateRepo: repo}
		// accountRepo is deliberately nil: neither standalone Create nor BindGroups may run.
		ctx := withGovernanceMutation(context.Background(), "", []int64{3})
		account, e := admin.CreateAccount(ctx, &CreateAccountInput{Name: "fixture", Platform: "openai", Type: "apikey", Credentials: map[string]any{"api_key": "fixture"}, Extra: map[string]any{governanceMarkerKey: "marker"}, GroupIDs: []int64{3}, SkipMixedChannelCheck: true})
		require.Equal(t, 1, repo.calls)
		require.EqualValues(t, 3, repo.groups[0].GroupID)
		if failure {
			require.ErrorIs(t, e, repo.err)
			require.Nil(t, account)
		} else {
			require.NoError(t, e)
			require.EqualValues(t, 19, account.ID)
			require.Equal(t, []int64{3}, account.GroupIDs)
		}
	}
}

func TestGovernanceTargetFingerprintRecheckedImmediatelyBeforeWrite(t *testing.T) {
	admin := &governanceAdminStub{group: &Group{ID: 3, Platform: "openai", Status: StatusActive, RateMultiplier: 1}}
	local := &governanceLocalAccounts{admin: admin}
	target, e := local.Target(context.Background(), 3, "openai")
	require.NoError(t, e)
	admin.group.RateMultiplier = 2
	_, e = local.ApplyAccount(context.Background(), gov.AccountChange{Marker: "marker", APIKey: "key", GroupID: 3, Platform: "openai", ExpectedTargetFingerprint: target.Fingerprint})
	require.ErrorIs(t, e, gov.ErrConflict)
	require.Zero(t, admin.creates)
	require.Zero(t, admin.updates)
}
