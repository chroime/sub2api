package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	GroupInputRoutingMaxRules           = 32
	GroupInputRoutingMaxAccountsPerRule = 128
	GroupInputRoutingMaxGroupsPerRule   = 32
	GroupInputRoutingMaxInputTokens     = 10_000_000
	GroupInputRoutingMaxIDRunes         = 64
	GroupInputRoutingMaxNameRunes       = 128
	GroupInputRoutingTokensAll          = "all"
	GroupInputRoutingTokensRange        = "range"
)

// GroupInputRoutingConfig restricts a matching request to one rule's candidate
// accounts. Requests that match no rule continue through the source group.
type GroupInputRoutingConfig struct {
	Enabled bool                    `json:"enabled"`
	Rules   []GroupInputRoutingRule `json:"rules"`
}

type GroupInputRoutingRule struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Enabled            bool    `json:"enabled"`
	RequestType        string  `json:"request_type"`
	InputTokensMode    string  `json:"input_tokens_mode"`
	MinInputTokens     int     `json:"min_input_tokens"`
	MaxInputTokens     int     `json:"max_input_tokens"`
	AccountIDs         []int64 `json:"account_ids"`
	GroupIDs           []int64 `json:"group_ids"`
	FallbackToOriginal bool    `json:"fallback_to_original"`
}

// UnmarshalJSON keeps old API payloads and cached auth snapshots equivalent to
// their former strict upper threshold. A missing mode means range; all mode may
// omit bounds and preserves any supplied numeric bounds for later editing.
func (r *GroupInputRoutingRule) UnmarshalJSON(data []byte) error {
	type ruleJSON GroupInputRoutingRule
	var decoded struct {
		ruleJSON
		InputTokensMode json.RawMessage `json:"input_tokens_mode"`
		MinInputTokens  json.RawMessage `json:"min_input_tokens"`
		MaxInputTokens  json.RawMessage `json:"max_input_tokens"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = GroupInputRoutingRule(decoded.ruleJSON)
	r.InputTokensMode = GroupInputRoutingTokensRange
	if len(decoded.InputTokensMode) > 0 {
		if strings.TrimSpace(string(decoded.InputTokensMode)) == "null" || json.Unmarshal(decoded.InputTokensMode, &r.InputTokensMode) != nil {
			return fmt.Errorf("input_tokens_mode must be all or range")
		}
		r.InputTokensMode = strings.ToLower(strings.TrimSpace(r.InputTokensMode))
	}
	if r.InputTokensMode != GroupInputRoutingTokensAll && r.InputTokensMode != GroupInputRoutingTokensRange {
		return fmt.Errorf("input_tokens_mode must be all or range")
	}
	if r.InputTokensMode == GroupInputRoutingTokensAll {
		// Inactive bounds are just editor drafts. Keep valid integers, but an
		// incomplete/decimal draft must not prevent saving an unrestricted rule.
		if len(decoded.MinInputTokens) > 0 {
			_ = json.Unmarshal(decoded.MinInputTokens, &r.MinInputTokens)
		}
		if len(decoded.MaxInputTokens) > 0 {
			_ = json.Unmarshal(decoded.MaxInputTokens, &r.MaxInputTokens)
		}
		return nil
	}
	if len(decoded.MaxInputTokens) == 0 && r.InputTokensMode == GroupInputRoutingTokensRange {
		return fmt.Errorf("max_input_tokens must be an integer")
	}
	if len(decoded.MaxInputTokens) > 0 && (strings.TrimSpace(string(decoded.MaxInputTokens)) == "null" || json.Unmarshal(decoded.MaxInputTokens, &r.MaxInputTokens) != nil) {
		return fmt.Errorf("max_input_tokens must be an integer")
	}
	if len(decoded.MinInputTokens) == 0 {
		if r.InputTokensMode == GroupInputRoutingTokensRange {
			// Invalid legacy thresholds remain invalid after conversion.
			if r.MaxInputTokens > 0 && r.MaxInputTokens <= GroupInputRoutingMaxInputTokens {
				r.MaxInputTokens--
			} else if r.MaxInputTokens <= 0 {
				r.MaxInputTokens = -1
			}
		}
	} else {
		if strings.TrimSpace(string(decoded.MinInputTokens)) == "null" || json.Unmarshal(decoded.MinInputTokens, &r.MinInputTokens) != nil {
			return fmt.Errorf("min_input_tokens must be an integer")
		}
	}
	return nil
}

// CloneGroupInputRouting owns all mutable slices and emits empty JSON arrays for
// unset lists, so legacy/default values can be edited and cleared consistently.
func CloneGroupInputRouting(config GroupInputRoutingConfig) GroupInputRoutingConfig {
	clone := GroupInputRoutingConfig{Enabled: config.Enabled, Rules: make([]GroupInputRoutingRule, len(config.Rules))}
	for i, rule := range config.Rules {
		clone.Rules[i] = rule
		if clone.Rules[i].InputTokensMode == "" {
			clone.Rules[i].InputTokensMode = GroupInputRoutingTokensRange
		}
		clone.Rules[i].AccountIDs = append([]int64{}, rule.AccountIDs...)
		clone.Rules[i].GroupIDs = append([]int64{}, rule.GroupIDs...)
	}
	return clone
}

// NormalizeGroupInputRouting validates even disabled rules: enabling an already
// saved rule must not activate an unvalidated target or unbounded configuration.
// Repository-dependent target existence/platform checks belong to the admin service.
func NormalizeGroupInputRouting(config GroupInputRoutingConfig) (GroupInputRoutingConfig, error) {
	if len(config.Rules) > GroupInputRoutingMaxRules {
		return GroupInputRoutingConfig{}, fmt.Errorf("input_routing supports at most %d rules", GroupInputRoutingMaxRules)
	}
	config = CloneGroupInputRouting(config)
	seen := make(map[string]struct{}, len(config.Rules))
	for i := range config.Rules {
		rule := &config.Rules[i]
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Name = strings.TrimSpace(rule.Name)
		rule.RequestType = strings.ToLower(strings.TrimSpace(rule.RequestType))
		rule.InputTokensMode = strings.ToLower(strings.TrimSpace(rule.InputTokensMode))
		if rule.ID == "" || utf8.RuneCountInString(rule.ID) > GroupInputRoutingMaxIDRunes {
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %d requires an ID of 1-%d characters", i+1, GroupInputRoutingMaxIDRunes)
		}
		if _, exists := seen[rule.ID]; exists {
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule ID %q is duplicated", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if utf8.RuneCountInString(rule.Name) > GroupInputRoutingMaxNameRunes {
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q name exceeds %d characters", rule.ID, GroupInputRoutingMaxNameRunes)
		}
		switch rule.RequestType {
		case "sync", "stream", "all":
		default:
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q request_type must be sync, stream, or all", rule.ID)
		}
		switch rule.InputTokensMode {
		case GroupInputRoutingTokensAll:
		case GroupInputRoutingTokensRange:
			if rule.MinInputTokens < 0 || rule.MinInputTokens > GroupInputRoutingMaxInputTokens ||
				rule.MaxInputTokens < rule.MinInputTokens || rule.MaxInputTokens > GroupInputRoutingMaxInputTokens {
				return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q token range must be between 0 and %d with min_input_tokens <= max_input_tokens", rule.ID, GroupInputRoutingMaxInputTokens)
			}
		default:
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q input_tokens_mode must be all or range", rule.ID)
		}
		var err error
		rule.AccountIDs, err = normalizeGroupInputRoutingIDs(rule.AccountIDs, GroupInputRoutingMaxAccountsPerRule)
		if err != nil {
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q account_ids: %w", rule.ID, err)
		}
		rule.GroupIDs, err = normalizeGroupInputRoutingIDs(rule.GroupIDs, GroupInputRoutingMaxGroupsPerRule)
		if err != nil {
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q group_ids: %w", rule.ID, err)
		}
		if len(rule.AccountIDs)+len(rule.GroupIDs) == 0 {
			return GroupInputRoutingConfig{}, fmt.Errorf("input_routing rule %q requires at least one account or group target", rule.ID)
		}
	}
	return config, nil
}

func normalizeGroupInputRoutingIDs(ids []int64, limit int) ([]int64, error) {
	if len(ids) > limit {
		return nil, fmt.Errorf("at most %d targets are allowed", limit)
	}
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("target IDs must be positive")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}
