package service

import "strings"

const (
	HomepageTemplateClassic = "classic"
	HomepageTemplateBrand   = "brand"
	HomepageTemplateCompact = "compact"
	HomepageTemplateCustom  = "custom"
)

func IsHomepageTemplate(value string) bool {
	switch value {
	case HomepageTemplateClassic, HomepageTemplateBrand, HomepageTemplateCompact, HomepageTemplateCustom:
		return true
	default:
		return false
	}
}

// ResolveHomepageTemplate honors an explicit selection before legacy controls.
// Deployment upgrades initialize the normal template from the previous branch;
// fresh installations without a selection use the classic homepage.
func ResolveHomepageTemplate(value, content string, compact bool) string {
	if IsHomepageTemplate(value) {
		return value
	}
	if strings.TrimSpace(content) != "" {
		return HomepageTemplateCustom
	}
	if compact {
		return HomepageTemplateCompact
	}
	return HomepageTemplateClassic
}
