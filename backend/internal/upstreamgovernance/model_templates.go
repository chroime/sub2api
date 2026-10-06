package upstreamgovernance

import (
	"context"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Model templates contain selected whitelist entries, never API credentials.
// Import previews retain the resolved mapping rather than a template reference.
type ModelTemplate struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Platform  string   `json:"platform"`
	Models    []string `json:"models"`
	IsDefault bool     `json:"is_default"`
}

type ModelTemplates struct {
	Version   int64           `json:"version"`
	Templates []ModelTemplate `json:"templates"`
}

type modelTemplateStore interface {
	LoadModelTemplates(context.Context) (*ModelTemplates, error)
	SaveModelTemplates(context.Context, *ModelTemplates, int64) error
}

var modelTemplateID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func normalizeModelTemplates(input ModelTemplates) (*ModelTemplates, error) {
	if input.Version < 0 || len(input.Templates) > 50 {
		return nil, ErrInvalid
	}
	out := &ModelTemplates{Version: input.Version, Templates: make([]ModelTemplate, 0, len(input.Templates))}
	ids, defaults := map[string]bool{}, map[string]bool{}
	for _, value := range input.Templates {
		value.Name = strings.TrimSpace(value.Name)
		if !modelTemplateID.MatchString(value.ID) || ids[value.ID] || !validTransport(value.Platform) ||
			value.Name == "" || utf8.RuneCountInString(value.Name) > 100 ||
			strings.ContainsAny(value.Name, "\r\n\x00") || len(value.Models) == 0 || len(value.Models) > 500 {
			return nil, ErrInvalid
		}
		ids[value.ID] = true
		if value.IsDefault {
			if defaults[value.Platform] {
				return nil, ErrInvalid
			}
			defaults[value.Platform] = true
		}
		models := make([]string, 0, len(value.Models))
		seen := map[string]bool{}
		for _, raw := range value.Models {
			model := strings.TrimSpace(raw)
			if model == "" || len(model) > 200 || strings.Contains(model, "*") || strings.ContainsFunc(model, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				return nil, ErrInvalid
			}
			if !seen[model] {
				seen[model] = true
				models = append(models, model)
			}
		}
		value.Models = models
		out.Templates = append(out.Templates, value)
	}
	return out, nil
}

func (s *Service) ModelTemplates(ctx context.Context) (*ModelTemplates, error) {
	store, ok := s.store.(modelTemplateStore)
	if !ok {
		return nil, ErrUnsupported
	}
	return store.LoadModelTemplates(ctx)
}

func (s *Service) SaveModelTemplates(ctx context.Context, input ModelTemplates) (*ModelTemplates, error) {
	if input.Version == math.MaxInt64 {
		return nil, ErrInvalid
	}
	value, err := normalizeModelTemplates(input)
	if err != nil {
		return nil, err
	}
	store, ok := s.store.(modelTemplateStore)
	if !ok {
		return nil, ErrUnsupported
	}
	if err := store.SaveModelTemplates(ctx, value, input.Version); err != nil {
		return nil, err
	}
	return value, nil
}
