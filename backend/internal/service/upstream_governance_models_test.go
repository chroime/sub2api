package service

import (
	"context"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func governanceModelAccount(t *testing.T, platform string, mapping map[string]string) *Account {
	t.Helper()
	config, err := gov.NormalizeAccountConfig(nil, platform, nil)
	require.NoError(t, err)
	config.ModelMapping = mapping
	change := gov.AccountChange{Marker: "governance-fixture", Platform: platform, BaseURL: "https://relay.example", APIKey: "fixture-key", AccountConfig: config}
	return &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: governanceImportCredentials(nil, change), Extra: governanceImportExtra(nil, change)}
}

func TestGovernanceImportedModelWhitelistDoesNotAddProviderModels(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			account := governanceModelAccount(t, platform, map[string]string{"claude-selected": "claude-selected"})
			require.Equal(t, map[string]string{"claude-selected": "claude-selected"}, account.GetModelMapping())
			require.True(t, account.IsModelSupported("claude-selected"))
			require.False(t, account.IsModelSupported("gemini-3.8-flash"))
			require.False(t, account.IsModelSupported("grok-4.5"))
			gateway := &GatewayService{}
			require.True(t, gateway.isModelSupportedByAccountWithContext(context.Background(), account, "claude-selected"))
			require.False(t, gateway.isModelSupportedByAccountWithContext(context.Background(), account, "gemini-3.8-flash"))
			require.False(t, gateway.isModelSupportedByAccount(account, "gemini-3.8-flash"))
		})
	}
}

func TestGovernanceUnrestrictedImportDoesNotApplyProviderWhitelist(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			account := governanceModelAccount(t, platform, nil)
			require.Empty(t, account.GetModelMapping())
			require.True(t, account.IsModelSupported("custom-upstream-model"))
			gateway := &GatewayService{}
			require.True(t, gateway.isModelSupportedByAccountWithContext(context.Background(), account, "custom-upstream-model"))
			require.True(t, gateway.isModelSupportedByAccount(account, "custom-upstream-model"))
			if platform == PlatformAntigravity {
				require.Equal(t, "custom-upstream-model", mapAntigravityModel(account, "custom-upstream-model"))
				require.True(t, (&GeminiMessagesCompatService{}).isModelSupportedByAccount(account, "custom-upstream-model"))
			}
		})
	}
}

func TestGovernanceAntigravityDoesNotInferUnselectedAliasesOrThinkingVariants(t *testing.T) {
	account := governanceModelAccount(t, PlatformAntigravity, map[string]string{
		"gemini-pro-agent": "gemini-pro-agent", "gemini-3.8-flash-high": "gemini-3.8-flash-high",
	})
	require.False(t, account.IsModelSupported("gemini-3.1-pro-preview"))
	require.Empty(t, mapAntigravityModel(account, "gemini-3.1-pro-preview"))
	require.Empty(t, (&AntigravityGatewayService{}).getMappedModelForThinkingLevel(account, "gemini-3.8-flash", "high"))
	require.Equal(t, "gemini-3.8-flash-high", mapAntigravityModel(account, "models/gemini-3.8-flash-high"))
	account.Credentials["model_mapping"] = map[string]any{"gemini-3.1-pro-preview": "gemini-3.1-pro-preview"}
	require.False(t, account.IsModelSupported("gemini-3.1-pro-preview-customtools"))
	model, matched := account.ResolveMappedModel("gemini-3.1-pro-preview-customtools")
	require.False(t, matched)
	require.Equal(t, "gemini-3.1-pro-preview-customtools", model)
	account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"}
	ctx := WithThinkingEnabled(context.Background(), true, false)
	require.True(t, (&GatewayService{}).isModelSupportedByAccountWithContext(ctx, account, "claude-sonnet-4-5"), "relay models must not require an additional OAuth thinking alias")
}

func TestGovernanceModelWhitelistSurvivesPassthroughSetting(t *testing.T) {
	account := governanceModelAccount(t, PlatformOpenAI, map[string]string{"gpt-selected": "gpt-selected"})
	account.Extra["openai_passthrough"] = true
	require.True(t, account.IsModelSupported("gpt-selected"))
	require.False(t, account.IsModelSupported("not-selected"))
	require.False(t, (&GatewayService{}).isModelSupportedByAccount(account, "not-selected"))
	delete(account.Extra, governanceMarkerKey)
	require.True(t, account.IsModelSupported("not-selected"), "ordinary passthrough behavior must remain unchanged")
	require.True(t, (&GatewayService{}).isModelSupportedByAccount(account, "not-selected"))
}

func TestGovernanceModelPolicyPreservesOrdinaryAndOAuthDefaults(t *testing.T) {
	account := governanceModelAccount(t, PlatformAntigravity, map[string]string{"claude-selected": "claude-selected"})
	delete(account.Extra, governanceMarkerKey)
	require.True(t, account.IsModelSupported("gemini-3.8-flash"))
	account.Extra[governanceMarkerKey] = "governance-fixture"
	require.False(t, account.IsModelSupported("gemini-3.8-flash"), "changing ownership must not reuse an expanded mapping cache")
	account.Type = AccountTypeOAuth
	require.True(t, account.IsModelSupported("gemini-3.8-flash"), "OAuth behavior must remain unchanged even with leftover metadata")
	account.Type = AccountTypeAPIKey
	require.False(t, account.IsModelSupported("gemini-3.8-flash"))
	account.Credentials["model_mapping"] = map[string]any{"custom-*": "selected-target"}
	require.True(t, account.IsModelSupported("custom-alias"))
	require.Equal(t, "selected-target", account.GetMappedModel("custom-alias"))
}
