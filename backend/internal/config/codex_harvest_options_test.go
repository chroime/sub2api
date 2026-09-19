package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCodexHarvestOptionsHaveSafeIndependentDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()
	var cfg Config
	require.NoError(t, viper.Unmarshal(&cfg))
	for _, key := range []string{"gateway.openai_codex_ticket", "gateway.openai_codex_ticket_332"} {
		require.Equal(t, 3, viper.GetInt(key+".harvest_concurrency"))
		require.False(t, viper.GetBool(key+".verify_enabled"))
		require.Empty(t, viper.Get(key+".harvest_proxy_ids"))
	}
	for _, profile := range []OpenAICodexTicketConfig{cfg.Gateway.OpenAICodexTicket, cfg.Gateway.OpenAICodexTicket332} {
		require.False(t, profile.VerifyEnabled)
		require.Equal(t, []int64{}, profile.HarvestProxyIDs)
		require.Equal(t, 3, profile.HarvestConcurrency)
	}
}

func TestCodexHarvestOptionsEnvironmentIsIndependent(t *testing.T) {
	for _, mode := range []string{"292", "332"} {
		t.Run(mode, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			setDefaults()
			// Keep these tests isolated from the machine's deployment environment.
			viper.SetEnvPrefix("CODEX_HARVEST_OPTIONS_TEST")
			viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
			viper.AutomaticEnv()
			prefix := "CODEX_HARVEST_OPTIONS_TEST_GATEWAY_OPENAI_CODEX_TICKET"
			if mode == "332" {
				prefix += "_332"
			}
			t.Setenv(prefix+"_VERIFY_ENABLED", "true")
			t.Setenv(prefix+"_HARVEST_PROXY_IDS", "7,9")
			t.Setenv(prefix+"_HARVEST_CONCURRENCY", "5")

			var cfg Config
			require.NoError(t, viper.Unmarshal(&cfg))
			selected, other := cfg.Gateway.OpenAICodexTicket, cfg.Gateway.OpenAICodexTicket332
			if mode == "332" {
				selected, other = other, selected
			}
			require.True(t, selected.VerifyEnabled)
			require.Equal(t, []int64{7, 9}, selected.HarvestProxyIDs)
			require.Equal(t, 5, selected.HarvestConcurrency)
			require.False(t, other.VerifyEnabled)
			require.Equal(t, []int64{}, other.HarvestProxyIDs)
			require.Equal(t, 3, other.HarvestConcurrency)
		})
	}
}

func TestValidateCodexTicketHarvestOptions(t *testing.T) {
	maxProxyIDs := make([]int64, 32)
	for i := range maxProxyIDs {
		maxProxyIDs[i] = int64(i + 1)
	}
	tests := []struct {
		name        string
		proxyIDs    []int64
		concurrency int
		wantError   string
	}{
		{name: "minimum concurrency without pool", concurrency: 1},
		{name: "empty pool", proxyIDs: []int64{}, concurrency: 3},
		{name: "maximum pool and concurrency", proxyIDs: maxProxyIDs, concurrency: 16},
		{name: "zero concurrency", concurrency: 0, wantError: "harvest_concurrency"},
		{name: "negative concurrency", concurrency: -1, wantError: "harvest_concurrency"},
		{name: "excessive concurrency", concurrency: 17, wantError: "harvest_concurrency"},
		{name: "zero proxy ID", proxyIDs: []int64{0}, concurrency: 3, wantError: "positive IDs"},
		{name: "negative proxy ID", proxyIDs: []int64{-1}, concurrency: 3, wantError: "positive IDs"},
		{name: "duplicate proxy ID", proxyIDs: []int64{7, 9, 7}, concurrency: 3, wantError: "unique IDs"},
		{name: "excessive pool size", proxyIDs: append(append([]int64{}, maxProxyIDs...), 33), concurrency: 3, wantError: "at most 32"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCodexTicketHarvestOptions(tt.proxyIDs, tt.concurrency)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestConfigValidateRejectsCodexHarvestOptionsForBothModes(t *testing.T) {
	for _, mode := range []string{"openai_codex_ticket", "openai_codex_ticket_332"} {
		t.Run(mode, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			setDefaults()
			for _, invalid := range []struct {
				name  string
				key   string
				value any
			}{
				{name: "concurrency", key: "harvest_concurrency", value: 0},
				{name: "proxy IDs", key: "harvest_proxy_ids", value: []int64{0}},
			} {
				t.Run(invalid.name, func(t *testing.T) {
					var cfg Config
					require.NoError(t, viper.Unmarshal(&cfg))
					profile := &cfg.Gateway.OpenAICodexTicket
					if mode == "openai_codex_ticket_332" {
						profile = &cfg.Gateway.OpenAICodexTicket332
					}
					switch value := invalid.value.(type) {
					case int:
						profile.HarvestConcurrency = value
					case []int64:
						profile.HarvestProxyIDs = value
					default:
						t.Fatalf("unexpected fixture type %T", value)
					}
					err := cfg.Validate()
					require.ErrorContains(t, err, "gateway."+mode)
					require.ErrorContains(t, err, invalid.key)
				})
			}
		})
	}
}
