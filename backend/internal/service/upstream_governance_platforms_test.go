package service

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceNativePlatformImport(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery("SELECT id FROM accounts").WithArgs("native-marker").WillReturnRows(sqlmock.NewRows([]string{"id"}))
			admin := &governanceAdminStub{group: &Group{ID: 3, Platform: platform, Status: StatusActive}}
			local := &governanceLocalAccounts{db: db, admin: admin}
			_, err = local.ApplyAccount(t.Context(), gov.AccountChange{Marker: "native-marker", Name: "Imported", Platform: platform, BaseURL: "https://relay.example", APIKey: "fixture-key", GroupID: 3, CostMultiplier: 1})
			require.NoError(t, err)
			require.Equal(t, platform, admin.account.Platform)
			require.Equal(t, AccountTypeAPIKey, admin.account.Type)
			require.Equal(t, "https://relay.example", admin.account.Credentials["base_url"])
			require.True(t, upstreamBillingRateSyncEnabled(admin.account))
			if IsCNProvider(platform) || platform == PlatformOpenCodeGo {
				require.Equal(t, APIProtocolChatCompletions, admin.account.GetAPIProtocol())
				require.Equal(t, "https://relay.example/v1/chat/completions", buildOpenAIChatCompletionsURL(admin.account.GetOpenAIBaseURL()))
			}
			if platform == PlatformOpenCodeGo {
				require.Equal(t, APIProtocolChatCompletions, admin.account.ResolveOpenCodeGoUpstreamProtocol("claude-fixture"))
				require.Equal(t, APIProtocolChatCompletions, admin.account.ResolveOpenCodeGoUpstreamProtocol("gpt-fixture"))
			}
			if platform == PlatformAntigravity {
				require.Equal(t, "https://relay.example/antigravity", admin.account.GetBaseURL())
				require.Equal(t, "https://relay.example/antigravity", admin.account.GetGeminiBaseURL("https://unused.example"))
			}
			admin.group.Platform = PlatformComposite
			_, err = local.Target(t.Context(), 3, platform)
			require.NoError(t, err)
			admin.group.Platform = "unsupported-platform"
			_, err = local.Target(t.Context(), 3, platform)
			require.ErrorIs(t, err, gov.ErrInvalid)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGovernanceRelayImportPreservesExplicitProtocol(t *testing.T) {
	previous := map[string]any{"api_protocol": APIProtocolResponses, "custom": "preserve"}
	credentials := governanceImportCredentials(previous, gov.AccountChange{Platform: PlatformOpenCodeGo, BaseURL: "https://relay.example", APIKey: "fixture-key", AccountConfig: &gov.AccountConfig{}})
	require.Equal(t, APIProtocolResponses, credentials["api_protocol"])
	require.Equal(t, "preserve", credentials["custom"])
	require.NotContains(t, previous, "api_key", "normalization must not mutate the prior credentials")
}
