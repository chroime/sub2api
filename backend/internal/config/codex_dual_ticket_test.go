package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCodexDualTicketConfigDefaultsAndIndependentEnvironment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()
	var cfg Config
	require.NoError(t, viper.Unmarshal(&cfg))
	require.Equal(t, OpenAICodexTicketConfig{
		TargetLength: 292, TTLSeconds: 3600, RefreshBeforeSeconds: 600,
		HarvestProbeIntervalSeconds: 6, HarvestAttemptTimeoutSeconds: 25,
		FailClosed: true, Models: []string{"gpt-6-astra", "gpt-5.6-sol"},
	}, cfg.Gateway.OpenAICodexTicket)
	require.Equal(t, OpenAICodexTicketConfig{
		TargetLength: 332, TTLSeconds: 3600, RefreshBeforeSeconds: 600,
		HarvestProbeIntervalSeconds: 6, HarvestAttemptTimeoutSeconds: 25,
		Models: []string{"gpt-6-astra", "gpt-5.6-sol"},
	}, cfg.Gateway.OpenAICodexTicket332)
	// Use a private test prefix; never read the machine's deployment settings.
	viper.SetEnvPrefix("CODEX_DUAL_TICKET_TEST")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	t.Setenv("CODEX_DUAL_TICKET_TEST_GATEWAY_OPENAI_CODEX_TICKET_332_ENABLED", "true")
	t.Setenv("CODEX_DUAL_TICKET_TEST_GATEWAY_OPENAI_CODEX_TICKET_332_FAIL_CLOSED", "true")
	t.Setenv("CODEX_DUAL_TICKET_TEST_GATEWAY_OPENAI_CODEX_TICKET_332_HARVEST_PROXY_URL", "http://test.example:8080")
	require.NoError(t, viper.Unmarshal(&cfg))
	require.True(t, cfg.Gateway.OpenAICodexTicket332.Enabled)
	require.True(t, cfg.Gateway.OpenAICodexTicket332.FailClosed)
	require.Equal(t, "http://test.example:8080", cfg.Gateway.OpenAICodexTicket332.HarvestProxyURL)
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled)
	require.True(t, cfg.Gateway.OpenAICodexTicket.FailClosed)
	require.Empty(t, cfg.Gateway.OpenAICodexTicket.HarvestProxyURL)
}
