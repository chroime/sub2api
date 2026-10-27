package service

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// NotificationEmailLinks are the public destinations shown in a groups-update
// campaign. Campaign overrides never change the global platform settings.
type NotificationEmailLinks struct {
	PlatformURL   string `json:"platform_url"`
	DocsURL       string `json:"docs_url"`
	ModelPlazaURL string `json:"model_plaza_url"`
	CanvasURL     string `json:"canvas_url"`
}

func (links NotificationEmailLinks) Variables() map[string]string {
	return map[string]string{
		"platform_url":    links.PlatformURL,
		"docs_url":        links.DocsURL,
		"model_plaza_url": links.ModelPlazaURL,
		"canvas_url":      links.CanvasURL,
	}
}

func notificationServiceLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "\r\n") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return raw
}

// ResolveServiceLinks uses the configured website and user-visible canvas
// menu. API endpoints and request Host headers are not website destinations.
// Unknown public hosts remain empty so an operator can supply them in preview.
func (s *NotificationEmailService) ResolveServiceLinks(ctx context.Context) (NotificationEmailLinks, error) {
	links := NotificationEmailLinks{}
	if s == nil || s.settingRepo == nil {
		return links, nil
	}
	settings, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyFrontendURL, SettingKeyDocURL, SettingKeyCustomMenuItems})
	if err != nil {
		return links, err
	}
	links.PlatformURL = strings.TrimRight(notificationServiceLink(settings[SettingKeyFrontendURL]), "/")
	links.DocsURL = notificationServiceLink(settings[SettingKeyDocURL])
	if links.PlatformURL != "" {
		if links.DocsURL == "" {
			links.DocsURL = links.PlatformURL + "/docs"
		}
		links.ModelPlazaURL = links.PlatformURL + "/model-plaza"
	}
	var menus []struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		URL        string `json:"url"`
		Visibility string `json:"visibility"`
	}
	if json.Unmarshal([]byte(settings[SettingKeyCustomMenuItems]), &menus) == nil {
		for _, menu := range menus {
			if menu.Visibility != "user" {
				continue
			}
			name := strings.ToLower(menu.ID + " " + menu.Label)
			if strings.Contains(name, "canvas") || strings.Contains(name, "画布") {
				if candidate := notificationServiceLink(menu.URL); candidate != "" {
					links.CanvasURL = candidate
					break
				}
			}
		}
	}
	return links, nil
}
