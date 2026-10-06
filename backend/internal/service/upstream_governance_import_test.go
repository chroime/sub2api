package service

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceImportCreatesConfiguredAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT id FROM accounts").WithArgs("marker").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	admin := &governanceAdminStub{group: &Group{ID: 3, Platform: "openai", Status: StatusActive}}
	local := &governanceLocalAccounts{db: db, admin: admin}
	_, err = local.ApplyAccount(t.Context(), gov.AccountChange{Marker: "marker", Name: "https://fixture.example--2", Platform: "openai", BaseURL: "https://fixture.example", APIKey: "fixture-import-key", GroupID: 3, CostMultiplier: 2})
	require.NoError(t, err)
	require.Equal(t, 5000, admin.account.Concurrency)
	require.NotNil(t, admin.account.Notes)
	require.Equal(t, "fixture-import-key", *admin.account.Notes)
	require.True(t, upstreamBillingRateSyncEnabled(admin.account))
	require.Equal(t, 10000.0, admin.account.GetQuotaDailyLimit())
	require.Equal(t, 700000.0, admin.account.GetQuotaWeeklyLimit())
	require.Equal(t, 10000000.0, admin.account.GetQuotaLimit())
	require.True(t, admin.account.IsOpenAILongContextBillingEnabled())
	// A later probe owns the live rate. Recovery must keep it without a second write.
	rate := 1.25
	admin.account.RateMultiplier = &rate
	mock.ExpectQuery("SELECT id FROM accounts").WithArgs("marker").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(admin.account.ID))
	_, err = local.ApplyAccount(t.Context(), gov.AccountChange{Marker: "marker", Name: "https://fixture.example--2", Platform: "openai", BaseURL: "https://fixture.example", APIKey: "fixture-import-key", GroupID: 3, CostMultiplier: 2})
	require.NoError(t, err)
	require.Zero(t, admin.updates)
	require.Equal(t, 1.25, admin.account.BillingRateMultiplier())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGovernanceFingerprintTracksImportedMutableSettings(t *testing.T) {
	note := "fixture-one"
	account := &Account{Notes: &note, Concurrency: 1}
	before := GovernanceAccountFingerprint(account)
	note = "fixture-two"
	require.NotEqual(t, before, GovernanceAccountFingerprint(account))
	before = GovernanceAccountFingerprint(account)
	account.Concurrency = 5000
	require.NotEqual(t, before, GovernanceAccountFingerprint(account))
}

func TestGovernanceFingerprintAllowsRuntimeAccountingRefresh(t *testing.T) {
	rate := 1.0
	account := &Account{RateMultiplier: &rate, Extra: map[string]any{
		UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: true,
		"quota_daily_limit": 10000.0, "quota_daily_used": 2.0,
	}}
	before := GovernanceAccountFingerprint(account)
	rate = 2.0
	account.Extra["quota_daily_used"] = 3.0
	account.Extra[UpstreamBillingProbeExtraKey] = map[string]any{"status": "ok"}
	require.Equal(t, before, GovernanceAccountFingerprint(account))
	account.Extra["quota_daily_limit"] = 20000.0
	require.NotEqual(t, before, GovernanceAccountFingerprint(account))
}

func TestCreateAccountRateSyncImpliesProbeAndRejectsContradiction(t *testing.T) {
	enabled, disabled := true, false
	repo := &upstreamBillingProbeAccountRepo{}
	admin := &adminServiceImpl{accountRepo: repo}
	input := &CreateAccountInput{Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "invented-key"}, SkipDefaultGroupBind: true, RateSyncEnabled: &enabled}
	account, err := admin.CreateAccount(t.Context(), input)
	require.NoError(t, err)
	require.True(t, upstreamBillingProbeEnabled(account))
	require.True(t, upstreamBillingRateSyncEnabled(account))
	input.ProbeEnabled = &disabled
	_, err = admin.CreateAccount(t.Context(), input)
	require.ErrorContains(t, err, "requires upstream billing probe")
	input.ProbeEnabled = nil
	input.Type = AccountTypeOAuth
	_, err = admin.CreateAccount(t.Context(), input)
	require.ErrorIs(t, err, ErrUpstreamBillingProbeAccountInvalid)
}
