//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type duplicateAccountRepoStub struct {
	*sparkShadowRepoStub
	atomicCreateErr error
	accountGroupsOf map[int64][]AccountGroup
}

func newDuplicateAccountRepoStub() *duplicateAccountRepoStub {
	return &duplicateAccountRepoStub{
		sparkShadowRepoStub: newSparkShadowRepoStub(),
		accountGroupsOf:     make(map[int64][]AccountGroup),
	}
}

func (s *duplicateAccountRepoStub) CreateWithAccountGroups(ctx context.Context, account *Account, groups []AccountGroup) error {
	if s.atomicCreateErr != nil {
		return s.atomicCreateErr
	}
	groupIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.GroupID)
	}
	account.GroupIDs = groupIDs
	if err := s.Create(ctx, account); err != nil {
		return err
	}
	clonedGroups := make([]AccountGroup, len(groups))
	copy(clonedGroups, groups)
	for i := range clonedGroups {
		clonedGroups[i].AccountID = account.ID
	}
	account.AccountGroups = clonedGroups
	s.accountGroupsOf[account.ID] = clonedGroups
	if len(groupIDs) > 0 {
		s.groupsOf[account.ID] = append([]int64(nil), groupIDs...)
	}
	stored := *account
	s.accounts[account.ID] = &stored
	s.mockAccountRepoForGemini.accountsByID[account.ID] = &stored
	return nil
}

func (s *duplicateAccountRepoStub) FindByExtraField(_ context.Context, key string, value any) ([]Account, error) {
	wanted, ok := value.(string)
	if !ok {
		return nil, nil
	}
	var matches []Account
	for _, account := range s.accounts {
		if actual, ok := account.Extra[key].(string); ok && actual == wanted {
			matches = append(matches, *account)
		}
	}
	return matches, nil
}

func TestDuplicateAccountCopiesConfigurationAndResetsRuntimeState(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}

	notes := "keep this note"
	proxyID := int64(17)
	originalProxyID := int64(11)
	rateMultiplier := 1.25
	loadFactor := 9
	expiresAt := time.Date(2027, time.March, 4, 5, 6, 7, 0, time.UTC)
	rateLimitedAt := time.Now().Add(-time.Minute)
	rateLimitResetAt := time.Now().Add(time.Hour)
	overloadUntil := time.Now().Add(2 * time.Hour)
	tempUnschedulableUntil := time.Now().Add(3 * time.Hour)
	sessionWindowStart := time.Now().Add(-2 * time.Hour)
	sessionWindowEnd := time.Now().Add(2 * time.Hour)

	source := &Account{
		Name:                  "primary",
		Notes:                 &notes,
		Platform:              PlatformAnthropic,
		Type:                  AccountTypeAPIKey,
		ProxyID:               &proxyID,
		ProxyFallbackOriginID: &originalProxyID,
		Concurrency:           6,
		Priority:              40,
		RateMultiplier:        &rateMultiplier,
		LoadFactor:            &loadFactor,
		Status:                StatusError,
		Schedulable:           true,
		ErrorMessage:          "upstream unavailable",
		ExpiresAt:             &expiresAt,
		AutoPauseOnExpired:    false,
		Credentials: map[string]any{
			"api_key": "secret",
			"nested":  map[string]any{"token": "source-token"},
		},
		Extra: map[string]any{
			"config":                          map[string]any{"region": "us-east-1"},
			"items":                           []any{map[string]any{"enabled": true}},
			"quota_limit":                     1000,
			"quota_used":                      450,
			"quota_daily_used":                25,
			"quota_daily_start":               "2026-07-15T00:00:00Z",
			"model_rate_limits":               map[string]any{"gpt-5": "2099-01-01T00:00:00Z"},
			"codex_5h_used_percent":           80,
			"codex_cli_only":                  true,
			"grok_usage_snapshot":             map[string]any{"status_code": 429},
			"openai_responses_supported":      false,
			"openai_compact_checked_at":       "2026-07-15T00:00:00Z",
			"session_window_utilization":      0.8,
			"passive_usage_sampled_at":        "2026-07-15T00:00:00Z",
			"antigravity_force_token_refresh": true,
			"antigravity_credits_overages":    map[string]any{"enabled": true},
			"crs_account_id":                  "remote-42",
			"crs_kind":                        "openai-api-key",
			"crs_synced_at":                   "2026-07-15T00:00:00Z",
		},
		GroupIDs:                []int64{7, 3},
		AccountGroups:           []AccountGroup{{GroupID: 7, Priority: 50}, {GroupID: 3, Priority: 7}},
		RateLimitedAt:           &rateLimitedAt,
		RateLimitResetAt:        &rateLimitResetAt,
		OverloadUntil:           &overloadUntil,
		TempUnschedulableUntil:  &tempUnschedulableUntil,
		TempUnschedulableReason: "maintenance",
		SessionWindowStart:      &sessionWindowStart,
		SessionWindowEnd:        &sessionWindowEnd,
		SessionWindowStatus:     "active",
	}
	source.Extra[UpstreamBillingProbeEnabledExtraKey] = true
	source.Extra[UpstreamBillingRateSyncEnabledExtraKey] = true
	source.Extra[UpstreamBillingProbeExtraKey] = map[string]any{"status": "ok"}
	require.NoError(t, repo.Create(ctx, source))

	duplicate, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")

	require.NoError(t, err)
	require.NotEqual(t, source.ID, duplicate.ID)
	require.Equal(t, "primary (Copy)", duplicate.Name)
	require.Equal(t, source.Platform, duplicate.Platform)
	require.Equal(t, source.Type, duplicate.Type)
	require.Equal(t, source.Concurrency, duplicate.Concurrency)
	require.Equal(t, source.Priority, duplicate.Priority)
	require.Equal(t, source.AutoPauseOnExpired, duplicate.AutoPauseOnExpired)
	require.Equal(t, source.GroupIDs, duplicate.GroupIDs)
	require.Equal(t, source.Credentials, duplicate.Credentials)
	require.Equal(t, map[string]any{
		"config":         map[string]any{"region": "us-east-1"},
		"items":          []any{map[string]any{"enabled": true}},
		"quota_limit":    float64(1000),
		"codex_cli_only": true,
	}, duplicate.Extra)
	require.NotContains(t, duplicate.Extra, UpstreamBillingRateSyncEnabledExtraKey)
	require.NotNil(t, duplicate.ExpiresAt)
	require.True(t, source.ExpiresAt.Equal(*duplicate.ExpiresAt))
	require.Equal(t, source.Notes, duplicate.Notes)
	require.Equal(t, source.ProxyFallbackOriginID, duplicate.ProxyID)
	require.Equal(t, source.RateMultiplier, duplicate.RateMultiplier)
	require.Equal(t, source.LoadFactor, duplicate.LoadFactor)
	require.Equal(t, source.GroupIDs, repo.groupsOf[duplicate.ID])
	require.Equal(t, []AccountGroup{
		{AccountID: duplicate.ID, GroupID: 7, Priority: 50},
		{AccountID: duplicate.ID, GroupID: 3, Priority: 7},
	}, repo.accountGroupsOf[duplicate.ID])

	require.Equal(t, StatusActive, duplicate.Status)
	require.False(t, duplicate.Schedulable)
	require.Empty(t, duplicate.ErrorMessage)
	require.Nil(t, duplicate.LastUsedAt)
	require.Nil(t, duplicate.RateLimitedAt)
	require.Nil(t, duplicate.RateLimitResetAt)
	require.Nil(t, duplicate.OverloadUntil)
	require.Nil(t, duplicate.TempUnschedulableUntil)
	require.Empty(t, duplicate.TempUnschedulableReason)
	require.Nil(t, duplicate.SessionWindowStart)
	require.Nil(t, duplicate.SessionWindowEnd)
	require.Empty(t, duplicate.SessionWindowStatus)

	duplicate.Credentials["nested"].(map[string]any)["token"] = "changed"
	duplicate.Extra["config"].(map[string]any)["region"] = "changed"
	duplicate.Extra["items"].([]any)[0].(map[string]any)["enabled"] = false
	storedSource, getErr := repo.GetByID(ctx, source.ID)
	require.NoError(t, getErr)
	require.Equal(t, "source-token", storedSource.Credentials["nested"].(map[string]any)["token"])
	require.Equal(t, "us-east-1", storedSource.Extra["config"].(map[string]any)["region"])
	require.Equal(t, true, storedSource.Extra["items"].([]any)[0].(map[string]any)["enabled"])
	require.Equal(t, "remote-42", storedSource.Extra["crs_account_id"])
}

func TestDuplicateAccountRejectsCredentialShadow(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	parentID := int64(99)
	shadow := &Account{
		Name:            "shadow",
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
	}
	require.NoError(t, repo.Create(ctx, shadow))

	_, err := svc.DuplicateAccount(ctx, shadow.ID, "admin:1", "")

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "ACCOUNT_DUPLICATE_SHADOW_UNSUPPORTED", infraerrors.Reason(err))
	require.Len(t, repo.accounts, 1)
}

func TestDuplicateAccountRejectsRotatingOrUnknownCredentialTypes(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, "legacy-cookie"} {
		t.Run(accountType, func(t *testing.T) {
			ctx := context.Background()
			repo := newDuplicateAccountRepoStub()
			svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
			source := &Account{
				Name:        "rotating-credential-account",
				Platform:    PlatformOpenAI,
				Type:        accountType,
				Credentials: map[string]any{"refresh_token": "shared-token"},
			}
			require.NoError(t, repo.Create(ctx, source))

			_, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")

			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Equal(t, "ACCOUNT_DUPLICATE_CREDENTIAL_TYPE_UNSUPPORTED", infraerrors.Reason(err))
			require.Len(t, repo.accounts, 1)
		})
	}
}

func TestDuplicateAccountPreservesUngroupedState(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	source := &Account{
		Name:        "ungrouped",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "secret"},
		GroupIDs:    nil,
	}
	require.NoError(t, repo.Create(ctx, source))

	duplicate, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")

	require.NoError(t, err)
	require.Empty(t, duplicate.GroupIDs)
	require.NotContains(t, repo.groupsOf, duplicate.ID)
}

func TestDuplicateAccountSimpleModeRejectsCompositeGroupBinding(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	groupRepo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{
		9: {ID: 9, Platform: PlatformComposite},
	}}
	svc := &adminServiceImpl{
		cfg: &config.Config{RunMode: config.RunModeSimple}, groupRepo: groupRepo,
		accountRepo: repo, accountDuplicateRepo: repo,
	}
	source := &Account{
		Name: "composite-bound", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "secret"}, GroupIDs: []int64{9},
	}
	require.NoError(t, repo.Create(ctx, source))

	_, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")

	require.Equal(t, "SIMPLE_MODE_GROUP_NOT_BINDABLE", infraerrors.Reason(err))
	require.Len(t, repo.accounts, 1)
}

func TestDuplicateAccountAtomicCreateFailureLeavesNoOrphan(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	source := &Account{
		Name:          "source",
		Platform:      PlatformAnthropic,
		Type:          AccountTypeAPIKey,
		Credentials:   map[string]any{"api_key": "secret"},
		GroupIDs:      []int64{7},
		AccountGroups: []AccountGroup{{GroupID: 7, Priority: 25}},
	}
	require.NoError(t, repo.Create(ctx, source))
	repo.atomicCreateErr = errors.New("group binding failed")

	_, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")

	require.ErrorContains(t, err, "group binding failed")
	require.Len(t, repo.accounts, 1)
}

func TestDuplicateAccountNamePreservesSuffixWithinSchemaLimit(t *testing.T) {
	name := duplicateAccountName(strings.Repeat("界", 100))

	require.Equal(t, 100, utf8.RuneCountInString(name))
	require.True(t, strings.HasSuffix(name, " (Copy)"))
}

func TestDuplicateAccountReturnsExistingCopyForSameOperationKey(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	source := &Account{
		Name:        "source",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "secret"},
	}
	require.NoError(t, repo.Create(ctx, source))

	first, err := svc.DuplicateAccount(ctx, source.ID, "admin:7", "stable-operation-key")
	require.NoError(t, err)
	second, err := svc.DuplicateAccount(ctx, source.ID, "admin:7", "stable-operation-key")
	require.NoError(t, err)
	recovered, err := svc.RecoverDuplicateAccount(ctx, source.ID, "admin:7", "stable-operation-key")
	require.NoError(t, err)
	otherAdminRecovery, err := svc.RecoverDuplicateAccount(ctx, source.ID, "admin:8", "stable-operation-key")
	require.NoError(t, err)
	otherAdminCopy, err := svc.DuplicateAccount(ctx, source.ID, "admin:8", "stable-operation-key")
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.ID, recovered.ID)
	require.Nil(t, otherAdminRecovery, "durable recovery identity must remain scoped to the initiating admin")
	require.NotEqual(t, first.ID, otherAdminCopy.ID)
	require.Len(t, repo.accounts, 3)
	require.NotEmpty(t, first.Extra[duplicateAccountOperationIDExtraKey])
}

func TestDuplicateAccountDoesNotInheritGovernanceIdentity(t *testing.T) {
	ctx := context.Background()
	repo := newDuplicateAccountRepoStub()
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	rate := 0.04
	source := &Account{
		Name: "governed-upstream", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, RateMultiplier: &rate,
		Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://fixture.example"},
		GroupIDs:    []int64{2}, AccountGroups: []AccountGroup{{GroupID: 2, Priority: 7}},
		Extra: map[string]any{
			duplicateAccountOperationIDExtraKey: "previous-source-copy-operation",
			governanceMarkerKey:                 "source-unique-import-marker",
			gov.GovernanceRateOwnerExtraKey:     "source-rate-owner",
			gov.GovernancePauseExtraKey:         map[string]any{"token": "source-pause", "identity": "source-identity"},
			gov.GovernanceReceiptExtraKey:       "source-receipt",
			"quota_limit":                       1000,
			"custom_config":                     map[string]any{"enabled": true},
		},
	}
	require.NoError(t, repo.Create(ctx, source))

	duplicate, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "governance-copy-operation")
	require.NoError(t, err)
	require.NotEqual(t, source.ID, duplicate.ID)
	require.False(t, duplicate.Schedulable)
	require.Equal(t, source.Credentials, duplicate.Credentials)
	require.Equal(t, source.GroupIDs, duplicate.GroupIDs)
	require.Equal(t, source.RateMultiplier, duplicate.RateMultiplier)
	require.Equal(t, float64(1000), duplicate.Extra["quota_limit"])
	require.Equal(t, map[string]any{"enabled": true}, duplicate.Extra["custom_config"])
	for _, field := range []string{governanceMarkerKey, gov.GovernanceRateOwnerExtraKey, gov.GovernancePauseExtraKey, gov.GovernanceReceiptExtraKey} {
		require.NotContains(t, duplicate.Extra, field, "governance ownership belongs to the original account")
	}
	require.False(t, GovernanceCatalogOwnsRate(duplicate), "the independent copy must not be owned by the source's rate automation")
	require.True(t, GovernanceCatalogOwnsRate(source))
	require.Equal(t, "source-unique-import-marker", source.Extra[governanceMarkerKey])
	require.Equal(t, "source-receipt", source.Extra[gov.GovernanceReceiptExtraKey])
	require.Equal(t, "previous-source-copy-operation", source.Extra[duplicateAccountOperationIDExtraKey])
	require.Equal(t, map[string]any{"token": "source-pause", "identity": "source-identity"}, source.Extra[gov.GovernancePauseExtraKey])
	require.True(t, source.Schedulable)

	replayed, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "governance-copy-operation")
	require.NoError(t, err)
	require.Equal(t, duplicate.ID, replayed.ID)
	require.Len(t, repo.accounts, 2, "retries still recover one independent copy")
	require.NotEmpty(t, duplicate.Extra[duplicateAccountOperationIDExtraKey])
	require.NotEqual(t, source.Extra[duplicateAccountOperationIDExtraKey], duplicate.Extra[duplicateAccountOperationIDExtraKey], "a copy must carry this operation's identity, not the source's previous operation")
}
