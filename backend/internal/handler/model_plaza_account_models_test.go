//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func plazaAccountForTest(id int64, platform string, mapping map[string]any) service.Account {
	return service.Account{ID: id, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Schedulable: true, Credentials: map[string]any{"model_mapping": mapping}}
}

func TestPlazaAccountModels_OnlyUsesSelectedGroupsAccountModels(t *testing.T) {
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		10: {plazaAccountForTest(1, service.PlatformOpenAI, map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"})},
		20: {plazaAccountForTest(2, service.PlatformOpenAI, map[string]any{"kimi-k3": "kimi-k3"})},
	}})
	candidates := []service.PlazaAccountModel{{Name: "gpt-6.1-sol", Platform: service.PlatformOpenAI}, {Name: "kimi-k3", Platform: service.PlatformOpenAI}}
	for _, tt := range []struct {
		groupID int64
		want    string
	}{{10, "gpt-6.1-sol"}, {20, "kimi-k3"}} {
		models := h.PlazaAccountModels(context.Background(), &service.Group{ID: tt.groupID, Platform: service.PlatformOpenAI}, candidates)
		require.Equal(t, []service.PlazaAccountModel{{Name: tt.want, Platform: service.PlatformOpenAI, ChannelRestrictionVerified: true}}, models)
	}
}

func TestPlazaAccountModels_ChannelCandidatesCanUseUnmappedProviderAccounts(t *testing.T) {
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		10: {plazaAccountForTest(1, service.PlatformKimi, nil)},
	}})
	models := h.PlazaAccountModels(context.Background(), &service.Group{ID: 10, Platform: service.PlatformKimi},
		[]service.PlazaAccountModel{{Name: "kimi-k3-1", Platform: service.PlatformKimi}, {Name: "gpt-6.1-sol", Platform: service.PlatformOpenAI}})
	require.Equal(t, []service.PlazaAccountModel{{Name: "kimi-k3-1", Platform: service.PlatformKimi, ChannelRestrictionVerified: true}}, models)
}

func TestPlazaAccountModels_PausedAccountsDoNotContributeButCooldownDoes(t *testing.T) {
	paused := plazaAccountForTest(1, service.PlatformOpenAI, map[string]any{"paused-model": "paused-model"})
	paused.Schedulable = false
	cooling := plazaAccountForTest(2, service.PlatformOpenAI, map[string]any{"available-model": "available-model"})
	until := time.Now().Add(time.Hour)
	cooling.TempUnschedulableUntil = &until
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{10: {paused, cooling}}})
	models := h.PlazaAccountModels(context.Background(), &service.Group{ID: 10, Platform: service.PlatformOpenAI}, nil)
	require.Equal(t, []service.PlazaAccountModel{{Name: "available-model", Platform: service.PlatformOpenAI, ChannelRestrictionVerified: true}}, models)
}

func TestPlazaAccountModels_WrongPlatformDoesNotInventDefaultModels(t *testing.T) {
	account := plazaAccountForTest(1, service.PlatformAntigravity, map[string]any{"claude-custom": "claude-sonnet-4-5"})
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{10: {account}}})
	for _, platform := range []string{service.PlatformGemini, service.PlatformAnthropic, service.PlatformOpenAI} {
		models := h.PlazaAccountModels(context.Background(), &service.Group{ID: 10, Platform: platform}, nil)
		require.Empty(t, models, "an unrelated account cannot trigger platform defaults")
	}
}

func TestPlazaAccountModels_MixedSchedulingKeepsSupportedCustomAlias(t *testing.T) {
	account := plazaAccountForTest(1, service.PlatformAntigravity, map[string]any{"claude-custom": "claude-sonnet-4-5"})
	account.Extra = map[string]any{"mixed_scheduling": true}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{10: {account}}})
	models := h.PlazaAccountModels(context.Background(), &service.Group{ID: 10, Platform: service.PlatformAnthropic}, nil)
	require.Contains(t, models, service.PlazaAccountModel{Name: "claude-custom", Platform: service.PlatformAnthropic, ChannelRestrictionVerified: true})
}

func TestPlazaAccountModels_ChannelTargetsAreCheckedBeforePublicAllowlist(t *testing.T) {
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		10: {plazaAccountForTest(1, service.PlatformOpenAI, map[string]any{"upstream-model": "upstream-model"})},
	}})
	group := &service.Group{ID: 10, Platform: service.PlatformOpenAI,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"public-alias"}}}
	models := h.PlazaAccountModels(context.Background(), group, []service.PlazaAccountModel{{Name: "upstream-model", Platform: service.PlatformOpenAI}})
	require.Equal(t, []service.PlazaAccountModel{{Name: "upstream-model", Platform: service.PlatformOpenAI, ChannelRestrictionVerified: true}}, models,
		"the plaza service applies the allowlist to the public alias after channel mapping")
}

func TestPlazaAccountModels_GovernancePassthroughDoesNotBypassAccountModelList(t *testing.T) {
	account := plazaAccountForTest(1, service.PlatformOpenAI, map[string]any{"reviewed-model": "upstream-model"})
	account.Extra = map[string]any{"upstream_governance_marker": "reviewed-import", "openai_passthrough": true}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{10: {account}}})
	models := h.PlazaAccountModels(context.Background(), &service.Group{ID: 10, Platform: service.PlatformOpenAI},
		[]service.PlazaAccountModel{{Name: "gpt-6.1-sol", Platform: service.PlatformOpenAI}})
	require.Equal(t, []service.PlazaAccountModel{{Name: "reviewed-model", Platform: service.PlatformOpenAI, ChannelRestrictionVerified: true}}, models)
}

func TestPlazaAccountModels_WildcardDoesNotReviveRetiredExactMapping(t *testing.T) {
	account := plazaAccountForTest(1, service.PlatformOpenAI, map[string]any{
		"gpt-*": "gpt-6.1-sol", "gpt-broken": "gpt-5.5",
	})
	account.Type = service.AccountTypeOAuth
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{10: {account}}})
	models := h.PlazaAccountModels(context.Background(), &service.Group{ID: 10, Platform: service.PlatformOpenAI},
		[]service.PlazaAccountModel{{Name: "gpt-6.1-sol", Platform: service.PlatformOpenAI}, {Name: "gpt-broken", Platform: service.PlatformOpenAI}})
	require.Equal(t, []service.PlazaAccountModel{{Name: "gpt-6.1-sol", Platform: service.PlatformOpenAI, ChannelRestrictionVerified: true}}, models,
		"only concrete candidates whose actual exact mapping passes may be returned")
}
