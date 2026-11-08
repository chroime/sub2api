package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validInputRoutingConfig() GroupInputRoutingConfig {
	return GroupInputRoutingConfig{Enabled: true, Rules: []GroupInputRoutingRule{{
		ID: "first", Name: "Small inputs", Enabled: true, RequestType: "all", MinInputTokens: 0, MaxInputTokens: 2000,
		AccountIDs: []int64{11}, GroupIDs: []int64{21}, FallbackToOriginal: true,
	}}}
}

func TestNormalizeGroupInputRouting(t *testing.T) {
	input := validInputRoutingConfig()
	input.Rules[0].ID = " first "
	input.Rules[0].Name = " Small inputs "
	input.Rules[0].RequestType = " STREAM "
	input.Rules[0].AccountIDs = []int64{12, 11, 12}
	input.Rules[0].GroupIDs = []int64{21, 21}
	got, err := NormalizeGroupInputRouting(input)
	require.NoError(t, err)
	require.Equal(t, "first", got.Rules[0].ID)
	require.Equal(t, "Small inputs", got.Rules[0].Name)
	require.Equal(t, "stream", got.Rules[0].RequestType)
	require.Equal(t, []int64{12, 11}, got.Rules[0].AccountIDs)
	require.Equal(t, []int64{21}, got.Rules[0].GroupIDs)
	got.Rules[0].AccountIDs[0] = 99
	got.Rules[0].GroupIDs[0] = 99
	require.Equal(t, int64(12), input.Rules[0].AccountIDs[0])
	require.Equal(t, int64(21), input.Rules[0].GroupIDs[0])
	require.Equal(t, " first ", input.Rules[0].ID)

	empty, err := NormalizeGroupInputRouting(GroupInputRoutingConfig{})
	require.NoError(t, err)
	raw, err := json.Marshal(empty)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":false,"rules":[]}`, string(raw))
}

func TestNormalizeGroupInputRoutingRejectsInvalidEvenDisabledRules(t *testing.T) {
	for name, mutate := range map[string]func(*GroupInputRoutingConfig){
		"missing id": func(c *GroupInputRoutingConfig) { c.Rules[0].ID = " " },
		"long id":    func(c *GroupInputRoutingConfig) { c.Rules[0].ID = strings.Repeat("界", GroupInputRoutingMaxIDRunes+1) },
		"long name": func(c *GroupInputRoutingConfig) {
			c.Rules[0].Name = strings.Repeat("界", GroupInputRoutingMaxNameRunes+1)
		},
		"duplicate id":     func(c *GroupInputRoutingConfig) { c.Rules = append(c.Rules, c.Rules[0]) },
		"invalid type":     func(c *GroupInputRoutingConfig) { c.Rules[0].RequestType = "websocket" },
		"negative minimum": func(c *GroupInputRoutingConfig) { c.Rules[0].MinInputTokens = -1 },
		"reversed range":   func(c *GroupInputRoutingConfig) { c.Rules[0].MinInputTokens = 101; c.Rules[0].MaxInputTokens = 100 },
		"large minimum":    func(c *GroupInputRoutingConfig) { c.Rules[0].MinInputTokens = GroupInputRoutingMaxInputTokens + 1 },
		"large maximum":    func(c *GroupInputRoutingConfig) { c.Rules[0].MaxInputTokens = GroupInputRoutingMaxInputTokens + 1 },
		"no targets":       func(c *GroupInputRoutingConfig) { c.Rules[0].AccountIDs = nil; c.Rules[0].GroupIDs = nil },
		"invalid account":  func(c *GroupInputRoutingConfig) { c.Rules[0].AccountIDs = []int64{0} },
		"invalid group":    func(c *GroupInputRoutingConfig) { c.Rules[0].GroupIDs = []int64{-1} },
		"too many rules":   func(c *GroupInputRoutingConfig) { c.Rules = make([]GroupInputRoutingRule, GroupInputRoutingMaxRules+1) },
		"too many accounts": func(c *GroupInputRoutingConfig) {
			c.Rules[0].AccountIDs = make([]int64, GroupInputRoutingMaxAccountsPerRule+1)
		},
		"too many groups": func(c *GroupInputRoutingConfig) {
			c.Rules[0].GroupIDs = make([]int64, GroupInputRoutingMaxGroupsPerRule+1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := validInputRoutingConfig()
			config.Enabled, config.Rules[0].Enabled = false, false
			mutate(&config)
			_, err := NormalizeGroupInputRouting(config)
			require.Error(t, err)
		})
	}
}

func TestNormalizeGroupInputRoutingAcceptsInclusiveZeroBasedRanges(t *testing.T) {
	config := validInputRoutingConfig()
	config.Rules[0].MinInputTokens = 0
	config.Rules[0].MaxInputTokens = 0
	_, err := NormalizeGroupInputRouting(config)
	require.NoError(t, err)

	config.Rules[0].MinInputTokens = 100
	config.Rules[0].MaxInputTokens = 200
	_, err = NormalizeGroupInputRouting(config)
	require.NoError(t, err)
}

func TestGroupInputRoutingRuleDecodesLegacyThresholdWithoutExpandingScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		min  int
		max  int
	}{
		{"legacy threshold", `{"max_input_tokens":100}`, 0, 99},
		{"legacy minimum threshold", `{"max_input_tokens":1}`, 0, 0},
		{"legacy maximum threshold", `{"max_input_tokens":10000000}`, 0, 9999999},
		{"explicit range", `{"min_input_tokens":0,"max_input_tokens":100}`, 0, 100},
		{"nonzero lower bound", `{"min_input_tokens":50,"max_input_tokens":100}`, 50, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rule GroupInputRoutingRule
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &rule))
			require.Equal(t, tc.min, rule.MinInputTokens)
			require.Equal(t, tc.max, rule.MaxInputTokens)
			encoded, err := json.Marshal(rule)
			require.NoError(t, err)
			var roundTrip GroupInputRoutingRule
			require.NoError(t, json.Unmarshal(encoded, &roundTrip))
			require.Equal(t, rule, roundTrip, "round trips must not reduce the upper bound twice")
		})
	}
}

func TestGroupInputRoutingRuleRejectsNullOrMissingRangeBounds(t *testing.T) {
	for _, raw := range []string{
		`{"min_input_tokens":null,"max_input_tokens":100}`,
		`{"min_input_tokens":0,"max_input_tokens":null}`,
		`{"min_input_tokens":0}`,
	} {
		var rule GroupInputRoutingRule
		require.Error(t, json.Unmarshal([]byte(raw), &rule), raw)
	}
}

func TestGroupInputRoutingRuleKeepsInvalidLegacyThresholdsInvalid(t *testing.T) {
	for _, threshold := range []int{0, -1, GroupInputRoutingMaxInputTokens + 1} {
		config := validInputRoutingConfig()
		raw, err := json.Marshal(map[string]any{
			"id": "legacy", "request_type": "all", "max_input_tokens": threshold, "account_ids": []int64{11},
		})
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &config.Rules[0]))
		_, err = NormalizeGroupInputRouting(config)
		require.Error(t, err, "invalid legacy threshold %d must remain invalid", threshold)
	}
}

func TestGroupInputRoutingRuleInputTokensModeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		mode string
		min  int
		max  int
	}{
		{"missing mode preserves legacy threshold", `{"max_input_tokens":100}`, "range", 0, 99},
		{"missing mode preserves inclusive bounds", `{"min_input_tokens":50,"max_input_tokens":100}`, "range", 50, 100},
		{"explicit range supports legacy threshold", `{"input_tokens_mode":"range","max_input_tokens":100}`, "range", 0, 99},
		{"all allows omitted bounds", `{"input_tokens_mode":"all"}`, "all", 0, 0},
		{"all preserves inactive bounds", `{"input_tokens_mode":"all","min_input_tokens":50,"max_input_tokens":100}`, "all", 50, 100},
		{"all does not convert inactive legacy maximum", `{"input_tokens_mode":"all","max_input_tokens":100}`, "all", 0, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rule GroupInputRoutingRule
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &rule))
			require.Equal(t, tc.min, rule.MinInputTokens)
			require.Equal(t, tc.max, rule.MaxInputTokens)
			encoded, err := json.Marshal(rule)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(encoded, &fields))
			require.Equal(t, tc.mode, fields["input_tokens_mode"], "serialized rules must carry the effective mode through DTO and auth-cache round trips")
			var roundTrip GroupInputRoutingRule
			require.NoError(t, json.Unmarshal(encoded, &roundTrip))
			require.Equal(t, rule, roundTrip)
		})
	}
}

func TestCloneGroupInputRoutingEmitsRangeForLegacyStructLiterals(t *testing.T) {
	config := validInputRoutingConfig()
	cloned := CloneGroupInputRouting(config)
	raw, err := json.Marshal(cloned)
	require.NoError(t, err)
	var fields struct {
		Rules []map[string]any `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.Equal(t, "range", fields.Rules[0]["input_tokens_mode"])
	var decoded GroupInputRoutingConfig
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, cloned, decoded)
}

func TestNormalizeGroupInputRoutingAllModeRetainsInactiveBoundsUntilRange(t *testing.T) {
	for _, bounds := range []string{
		`"min_input_tokens":-1,"max_input_tokens":100`,
		`"min_input_tokens":101,"max_input_tokens":100`,
		`"min_input_tokens":0,"max_input_tokens":10000001`,
	} {
		raw := `{"enabled":true,"rules":[{"id":"mode","enabled":false,"request_type":"sync","input_tokens_mode":"all","account_ids":[11],` + bounds + `}]}`
		var config GroupInputRoutingConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		normalized, err := NormalizeGroupInputRouting(config)
		require.NoError(t, err, "all mode must not use inactive bounds")
		require.Equal(t, config.Rules[0].MinInputTokens, normalized.Rules[0].MinInputTokens)
		require.Equal(t, config.Rules[0].MaxInputTokens, normalized.Rules[0].MaxInputTokens)
		var ranged GroupInputRoutingConfig
		require.NoError(t, json.Unmarshal([]byte(strings.Replace(raw, `"input_tokens_mode":"all"`, `"input_tokens_mode":"range"`, 1)), &ranged))
		_, err = NormalizeGroupInputRouting(ranged)
		require.Error(t, err, "switching to range must validate preserved bounds, even for a disabled rule")
	}
}

func TestGroupInputRoutingRejectsInvalidInputTokensModesAndProvidedBounds(t *testing.T) {
	for _, fields := range []string{
		`"input_tokens_mode":"unknown","min_input_tokens":0,"max_input_tokens":100`,
		`"input_tokens_mode":"","min_input_tokens":0,"max_input_tokens":100`,
		`"input_tokens_mode":null,"min_input_tokens":0,"max_input_tokens":100`,
		`"input_tokens_mode":42,"min_input_tokens":0,"max_input_tokens":100`,
		`"input_tokens_mode":"range","min_input_tokens":null,"max_input_tokens":100`,
		`"input_tokens_mode":"range","min_input_tokens":0,"max_input_tokens":null`,
		`"input_tokens_mode":"range","max_input_tokens":"100"`,
		`"input_tokens_mode":"range","min_input_tokens":0`,
	} {
		raw := `{"enabled":false,"rules":[{"id":"invalid","enabled":false,"request_type":"sync","account_ids":[11],` + fields + `}]}`
		var config GroupInputRoutingConfig
		err := json.Unmarshal([]byte(raw), &config)
		if err == nil {
			_, err = NormalizeGroupInputRouting(config)
		}
		require.Error(t, err, raw)
	}
}

func TestGroupInputRoutingAllModeIgnoresInvalidHiddenRangeDrafts(t *testing.T) {
	for _, bounds := range []string{
		`"min_input_tokens":0,"max_input_tokens":1.5`,
		`"min_input_tokens":null,"max_input_tokens":100`,
		`"min_input_tokens":0,"max_input_tokens":null`,
	} {
		raw := `{"enabled":true,"rules":[{"id":"all-lengths","enabled":true,"request_type":"sync","input_tokens_mode":"all","account_ids":[11],` + bounds + `}]}`
		var config GroupInputRoutingConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		_, err := NormalizeGroupInputRouting(config)
		require.NoError(t, err, "hidden interval drafts must not prevent saving all lengths")
	}
}
