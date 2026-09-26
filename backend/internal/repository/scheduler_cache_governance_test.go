package repository

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataPreservesGovernanceModelPolicy(t *testing.T) {
	platforms := []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformAntigravity, service.PlatformGrok, service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek, service.PlatformMiniMax, service.PlatformOpenCodeGo}
	for _, platform := range platforms {
		for _, restricted := range []bool{false, true} {
			name := platform + "/unrestricted"
			if restricted {
				name = platform + "/restricted"
			}
			t.Run(name, func(t *testing.T) {
				account := service.Account{
					Platform: platform, Type: service.AccountTypeAPIKey,
					Credentials: map[string]any{},
					Extra: map[string]any{
						"upstream_governance_marker": "governance-fixture",
						"unrelated_private_value":    "drop-me",
					},
				}
				if restricted {
					account.Credentials["model_mapping"] = map[string]any{"claude-selected": "claude-selected"}
				}
				payload, err := json.Marshal(buildSchedulerMetadataAccount(account))
				require.NoError(t, err)
				var restored service.Account
				require.NoError(t, json.Unmarshal(payload, &restored))
				require.Equal(t, "governance-fixture", restored.Extra["upstream_governance_marker"])
				require.NotContains(t, restored.Extra, "unrelated_private_value")
				require.True(t, restored.IsModelSupported("claude-selected"))
				for _, model := range []string{"gemini-3.8-flash", "grok-4.5", "custom-upstream-model"} {
					require.Equal(t, !restricted, restored.IsModelSupported(model), model)
				}
			})
		}
	}
}
