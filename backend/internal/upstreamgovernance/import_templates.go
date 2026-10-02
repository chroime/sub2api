package upstreamgovernance

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ImportTemplateSettings is a strict allowlist of reusable account settings.
// It intentionally does not embed AccountConfig: models, identity, credentials,
// costs and routing associations must never enter template storage.
type ImportTemplateSettings struct {
	Concurrency                     int     `json:"concurrency"`
	Priority                        int     `json:"priority"`
	QuotaEnabled                    bool    `json:"quota_enabled"`
	QuotaDailyLimit                 float64 `json:"quota_daily_limit"`
	QuotaWeeklyLimit                float64 `json:"quota_weekly_limit"`
	QuotaLimit                      float64 `json:"quota_limit"`
	UpstreamBillingRateSyncEnabled  bool    `json:"upstream_billing_rate_sync_enabled"`
	OpenAILongContextBillingEnabled bool    `json:"openai_long_context_billing_enabled"`
}

type ImportTemplate struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	IsDefault bool                   `json:"is_default"`
	Settings  ImportTemplateSettings `json:"settings"`
}

type ImportTemplates struct {
	Version   int64            `json:"version"`
	Templates []ImportTemplate `json:"templates"`
}

// The explicit wire shapes distinguish absent/null fields from valid zero and
// false values. Their strict decoders also protect stored-data reads, not only
// the HTTP handler's outer decoder.
func decodeImportTemplateJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return ErrInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (s *ImportTemplateSettings) UnmarshalJSON(data []byte) error {
	var input struct {
		Concurrency                     *int     `json:"concurrency"`
		Priority                        *int     `json:"priority"`
		QuotaEnabled                    *bool    `json:"quota_enabled"`
		QuotaDailyLimit                 *float64 `json:"quota_daily_limit"`
		QuotaWeeklyLimit                *float64 `json:"quota_weekly_limit"`
		QuotaLimit                      *float64 `json:"quota_limit"`
		UpstreamBillingRateSyncEnabled  *bool    `json:"upstream_billing_rate_sync_enabled"`
		OpenAILongContextBillingEnabled *bool    `json:"openai_long_context_billing_enabled"`
	}
	if err := decodeImportTemplateJSON(data, &input); err != nil {
		return err
	}
	if input.Concurrency == nil || input.Priority == nil || input.QuotaEnabled == nil || input.QuotaDailyLimit == nil || input.QuotaWeeklyLimit == nil || input.QuotaLimit == nil || input.UpstreamBillingRateSyncEnabled == nil || input.OpenAILongContextBillingEnabled == nil {
		return ErrInvalid
	}
	*s = ImportTemplateSettings{Concurrency: *input.Concurrency, Priority: *input.Priority, QuotaEnabled: *input.QuotaEnabled, QuotaDailyLimit: *input.QuotaDailyLimit, QuotaWeeklyLimit: *input.QuotaWeeklyLimit, QuotaLimit: *input.QuotaLimit, UpstreamBillingRateSyncEnabled: *input.UpstreamBillingRateSyncEnabled, OpenAILongContextBillingEnabled: *input.OpenAILongContextBillingEnabled}
	return nil
}
func (t *ImportTemplate) UnmarshalJSON(data []byte) error {
	var input struct {
		ID        *string                 `json:"id"`
		Name      *string                 `json:"name"`
		IsDefault *bool                   `json:"is_default"`
		Settings  *ImportTemplateSettings `json:"settings"`
	}
	if err := decodeImportTemplateJSON(data, &input); err != nil {
		return err
	}
	if input.ID == nil || input.Name == nil || input.IsDefault == nil || input.Settings == nil {
		return ErrInvalid
	}
	*t = ImportTemplate{ID: *input.ID, Name: *input.Name, IsDefault: *input.IsDefault, Settings: *input.Settings}
	return nil
}
func (c *ImportTemplates) UnmarshalJSON(data []byte) error {
	var input struct {
		Version   *int64            `json:"version"`
		Templates *[]ImportTemplate `json:"templates"`
	}
	if err := decodeImportTemplateJSON(data, &input); err != nil {
		return err
	}
	if input.Version == nil || input.Templates == nil {
		return ErrInvalid
	}
	*c = ImportTemplates{Version: *input.Version, Templates: *input.Templates}
	return nil
}

var importTemplateID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func normalizeImportTemplates(input ImportTemplates) (*ImportTemplates, error) {
	if input.Version < 0 || len(input.Templates) > 50 {
		return nil, ErrInvalid
	}
	result := &ImportTemplates{Version: input.Version, Templates: make([]ImportTemplate, 0, len(input.Templates))}
	ids := map[string]bool{}
	hasDefault := false
	for _, item := range input.Templates {
		if !importTemplateID.MatchString(item.ID) || ids[item.ID] || strings.ContainsFunc(item.Name, unicode.IsControl) {
			return nil, ErrInvalid
		}
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" || utf8.RuneCountInString(item.Name) > 100 {
			return nil, ErrInvalid
		}
		settings := item.Settings
		if settings.Concurrency < 1 || int64(settings.Concurrency) > math.MaxInt32 || settings.Priority < 0 || int64(settings.Priority) > math.MaxInt32 || !validRate(settings.QuotaDailyLimit) || !validRate(settings.QuotaWeeklyLimit) || !validRate(settings.QuotaLimit) {
			return nil, ErrInvalid
		}
		if item.IsDefault {
			if hasDefault {
				return nil, ErrInvalid
			}
			hasDefault = true
		}
		ids[item.ID] = true
		result.Templates = append(result.Templates, item)
	}
	return result, nil
}

type importTemplateStore interface {
	LoadImportTemplates(context.Context) (*ImportTemplates, error)
	SaveImportTemplates(context.Context, *ImportTemplates, int64) error
}

func (s *Service) ImportTemplates(ctx context.Context) (*ImportTemplates, error) {
	store, ok := s.store.(importTemplateStore)
	if !ok {
		return nil, ErrUnsupported
	}
	return store.LoadImportTemplates(ctx)
}
func (s *Service) SaveImportTemplates(ctx context.Context, input ImportTemplates) (*ImportTemplates, error) {
	if input.Version == math.MaxInt64 {
		return nil, ErrInvalid
	}
	value, err := normalizeImportTemplates(input)
	if err != nil {
		return nil, err
	}
	store, ok := s.store.(importTemplateStore)
	if !ok {
		return nil, ErrUnsupported
	}
	if err = store.SaveImportTemplates(ctx, value, input.Version); err != nil {
		return nil, err
	}
	return value, nil
}
