//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAccountIsOpenAISyntheticFirstResponseEnabledForGroup(t *testing.T) {
	groupID := int64(42)
	otherGroupID := int64(99)
	zeroGroupID := int64(0)

	tests := []struct {
		name    string
		account *Account
		groupID *int64
		want    bool
	}{
		{name: "nil account is disabled", account: nil, groupID: &groupID},
		{name: "non OpenAI account is disabled", account: &Account{Platform: PlatformGrok, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}}, groupID: &groupID},
		{name: "missing key defaults disabled", account: &Account{Platform: PlatformOpenAI}, groupID: &groupID},
		{name: "explicit false is disabled", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: false}}, groupID: &groupID},
		{name: "malformed value is disabled", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: "true"}}, groupID: &groupID},
		{name: "spark shadow is disabled even with an explicit key", account: &Account{Platform: PlatformOpenAI, ParentAccountID: &groupID, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}, GroupIDs: []int64{groupID}}, groupID: &groupID},
		{name: "enabled account matches GroupIDs", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}, GroupIDs: []int64{groupID}}, groupID: &groupID, want: true},
		{name: "enabled account matches AccountGroups", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}, AccountGroups: []AccountGroup{{GroupID: groupID}}}, groupID: &groupID, want: true},
		{name: "enabled account rejects another group", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}, GroupIDs: []int64{groupID}}, groupID: &otherGroupID},
		{name: "enabled ungrouped account matches nil group", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}}, groupID: nil, want: true},
		{name: "enabled ungrouped account matches zero group", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}}, groupID: &zeroGroupID, want: true},
		{name: "enabled grouped account rejects nil group", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}, GroupIDs: []int64{groupID}}, groupID: nil},
		{name: "enabled grouped account rejects zero group", account: &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}, GroupIDs: []int64{groupID}}, groupID: &zeroGroupID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.IsOpenAISyntheticFirstResponseEnabledForGroup(tt.groupID))
		})
	}
}

func TestValidateOpenAISyntheticFirstResponseExtra(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		extra    map[string]any
		wantErr  bool
	}{
		{name: "missing key is valid", platform: PlatformOpenAI},
		{name: "boolean true is valid", platform: PlatformOpenAI, extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: true}},
		{name: "boolean false is valid", platform: PlatformOpenAI, extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: false}},
		{name: "malformed OpenAI value is rejected", platform: PlatformOpenAI, extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: "true"}, wantErr: true},
		{name: "non OpenAI value is provider-owned", platform: PlatformGrok, extra: map[string]any{OpenAISyntheticFirstResponseEnabledExtraKey: "true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOpenAISyntheticFirstResponseExtra(tt.platform, tt.extra)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAdminServiceUpdateAccountExtraValidatesSyntheticFirstResponse(t *testing.T) {
	for _, tt := range []struct {
		name    string
		value   any
		wantErr bool
	}{
		{name: "boolean is accepted", value: true},
		{name: "malformed value is rejected", value: "true", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &longContextBillingRepoStub{account: &Account{ID: 1, Platform: PlatformOpenAI}}
			svc := &adminServiceImpl{accountRepo: repo}

			err := svc.UpdateAccountExtra(context.Background(), 1, map[string]any{
				OpenAISyntheticFirstResponseEnabledExtraKey: tt.value,
			})
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Zero(t, repo.updateExtraCalls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, repo.updateExtraCalls)
		})
	}
}

func TestNormalizeBulkOpenAISyntheticFirstResponseSetting(t *testing.T) {
	valid := &BulkUpdateAccountsInput{Extra: map[string]any{
		OpenAISyntheticFirstResponseEnabledExtraKey: true,
	}}
	settings, err := normalizeBulkOpenAISettings(valid)
	require.NoError(t, err)
	require.True(t, settings.syntheticFirstResponse)

	_, err = normalizeBulkOpenAISettings(&BulkUpdateAccountsInput{Extra: map[string]any{
		OpenAISyntheticFirstResponseEnabledExtraKey: "true",
	}})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
}

func TestAdminServiceBulkUpdateSyntheticFirstResponseValidatesTargets(t *testing.T) {
	parentID := int64(10)
	tests := []struct {
		name        string
		account     *Account
		wantErr     bool
		wantWritten bool
	}{
		{
			name:    "rejects non OpenAI account",
			account: &Account{ID: 1, Platform: PlatformGrok},
			wantErr: true,
		},
		{
			name:    "rejects spark shadow account",
			account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID},
			wantErr: true,
		},
		{
			name:        "writes ordinary OpenAI account",
			account:     &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			wantWritten: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &longContextBillingRepoStub{account: tt.account}
			svc := &adminServiceImpl{accountRepo: repo}

			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
				AccountIDs: []int64{1},
				Extra: map[string]any{
					OpenAISyntheticFirstResponseEnabledExtraKey: true,
				},
			})

			if tt.wantErr {
				require.Nil(t, result)
				require.Error(t, err)
				require.Equal(t, "OPENAI_BULK_TARGET_INVALID", infraerrors.Reason(err))
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
			if tt.wantWritten {
				require.Equal(t, 1, repo.bulkUpdateCalls)
			} else {
				require.Zero(t, repo.bulkUpdateCalls)
			}
		})
	}
}
