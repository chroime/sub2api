package upstreamgovernance

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func importTemplateFixture() ImportTemplates {
	return ImportTemplates{Version: 0, Templates: []ImportTemplate{{ID: "basic", Name: "Basic", IsDefault: true, Settings: ImportTemplateSettings{Concurrency: 5000, Priority: 1, QuotaEnabled: true, QuotaDailyLimit: 10000, QuotaWeeklyLimit: 700000, QuotaLimit: 10000000, UpstreamBillingRateSyncEnabled: true, OpenAILongContextBillingEnabled: true}}}}
}

func TestImportTemplatesNormalizeNamesAndPreserveExplicitZeroSettings(t *testing.T) {
	input := importTemplateFixture()
	input.Templates[0].Name = " 默认参数 "
	input.Templates[0].Settings = ImportTemplateSettings{Concurrency: 1}
	got, err := normalizeImportTemplates(input)
	require.NoError(t, err)
	require.Equal(t, "默认参数", got.Templates[0].Name)
	require.Equal(t, " 默认参数 ", input.Templates[0].Name)
	require.Equal(t, ImportTemplateSettings{Concurrency: 1}, got.Templates[0].Settings)
	empty, err := normalizeImportTemplates(ImportTemplates{})
	require.NoError(t, err)
	require.NotNil(t, empty.Templates)
	require.Empty(t, empty.Templates)
}

func TestImportTemplatesValidationRejectsInvalidAndNonfiniteValues(t *testing.T) {
	for name, change := range map[string]func(*ImportTemplates){
		"negative version": func(v *ImportTemplates) { v.Version = -1 },
		"too many":         func(v *ImportTemplates) { v.Templates = make([]ImportTemplate, 51) },
		"duplicate id":     func(v *ImportTemplates) { v.Templates = append(v.Templates, v.Templates[0]) },
		"duplicate default": func(v *ImportTemplates) {
			other := v.Templates[0]
			other.ID = "other"
			v.Templates = append(v.Templates, other)
		},
		"bad id":            func(v *ImportTemplates) { v.Templates[0].ID = "../bad" },
		"long id":           func(v *ImportTemplates) { v.Templates[0].ID = strings.Repeat("a", 65) },
		"blank name":        func(v *ImportTemplates) { v.Templates[0].Name = "  " },
		"long name":         func(v *ImportTemplates) { v.Templates[0].Name = strings.Repeat("字", 101) },
		"control name":      func(v *ImportTemplates) { v.Templates[0].Name = "name\u0085control" },
		"zero concurrency":  func(v *ImportTemplates) { v.Templates[0].Settings.Concurrency = 0 },
		"large concurrency": func(v *ImportTemplates) { v.Templates[0].Settings.Concurrency = math.MaxInt32 + 1 },
		"negative priority": func(v *ImportTemplates) { v.Templates[0].Settings.Priority = -1 },
		"large priority":    func(v *ImportTemplates) { v.Templates[0].Settings.Priority = math.MaxInt32 + 1 },
		"negative quota":    func(v *ImportTemplates) { v.Templates[0].Settings.QuotaDailyLimit = -.1 },
		"infinite quota":    func(v *ImportTemplates) { v.Templates[0].Settings.QuotaWeeklyLimit = math.Inf(1) },
		"nan quota":         func(v *ImportTemplates) { v.Templates[0].Settings.QuotaLimit = math.NaN() },
	} {
		t.Run(name, func(t *testing.T) {
			input := importTemplateFixture()
			change(&input)
			_, err := normalizeImportTemplates(input)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	input := importTemplateFixture()
	input.Version = math.MaxInt64
	_, err := NewService(nil, nil, nil, nil, false).SaveImportTemplates(t.Context(), input)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestImportTemplatesStrictJSONRequiresEveryFieldAndRejectsUnknowns(t *testing.T) {
	raw, err := json.Marshal(importTemplateFixture())
	require.NoError(t, err)
	var decoded ImportTemplates
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, importTemplateFixture(), decoded)
	var valid map[string]any
	require.NoError(t, json.Unmarshal(raw, &valid))
	for _, field := range []string{"concurrency", "priority", "quota_enabled", "quota_daily_limit", "quota_weekly_limit", "quota_limit", "upstream_billing_rate_sync_enabled", "openai_long_context_billing_enabled"} {
		for _, null := range []bool{false, true} {
			t.Run(field+map[bool]string{false: " missing", true: " null"}[null], func(t *testing.T) {
				var value map[string]any
				require.NoError(t, json.Unmarshal(raw, &value))
				settings := value["templates"].([]any)[0].(map[string]any)["settings"].(map[string]any)
				if null {
					settings[field] = nil
				} else {
					delete(settings, field)
				}
				broken, e := json.Marshal(value)
				require.NoError(t, e)
				var out ImportTemplates
				require.Error(t, json.Unmarshal(broken, &out))
			})
		}
	}
	for _, body := range []string{`{}`, `{"version":null,"templates":[]}`, `{"version":0,"templates":null}`, `{"version":0,"templates":[null]}`, `{"version":0,"templates":[],"credentials":{}}`, strings.Replace(string(raw), `"settings":{`, `"settings":{"model_mapping":{},`, 1), strings.Replace(string(raw), `"settings":{`, `"settings":{"credentials":{"secret":"fixture"}},`, 1), strings.Replace(string(raw), `"is_default":true,`, "", 1), strings.Replace(string(raw), `"priority":1`, `"priority":0.5`, 1)} {
		var out ImportTemplates
		require.Error(t, json.Unmarshal([]byte(body), &out), body)
	}
	zero := `{"version":0,"templates":[{"id":"zero","name":"Zero","is_default":false,"settings":{"concurrency":1,"priority":0,"quota_enabled":false,"quota_daily_limit":0,"quota_weekly_limit":0,"quota_limit":0,"upstream_billing_rate_sync_enabled":false,"openai_long_context_billing_enabled":false}}]}`
	require.NoError(t, json.Unmarshal([]byte(zero), &decoded))
	require.Equal(t, ImportTemplateSettings{Concurrency: 1}, decoded.Templates[0].Settings)
}
