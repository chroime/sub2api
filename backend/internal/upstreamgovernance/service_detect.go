package upstreamgovernance

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type DetectInput struct {
	BaseURL string `json:"base_url"`
	ProxyID *int64 `json:"proxy_id"`
}

type DetectedSite struct {
	Platform        string `json:"platform"`
	Name            string `json:"name"`
	BaseURL         string `json:"base_url"`
	CaptchaRequired bool   `json:"captcha_required"`
	CaptchaSiteKey  string `json:"captcha_site_key,omitempty"`
}

type siteDetector interface {
	Detect(context.Context, Site) (*DetectedSite, error)
}

func (s *Service) Detect(ctx context.Context, input DetectInput) (*DetectedSite, error) {
	site := Site{Name: "detect", Platform: "sub2api", BaseURL: strings.TrimSpace(input.BaseURL), ProxyID: input.ProxyID, IntervalMinutes: 15}
	if !strings.Contains(site.BaseURL, "://") {
		site.BaseURL = "https://" + site.BaseURL
	}
	if e := validateSite(&site); e != nil {
		return nil, e
	}
	detector, ok := s.connector.(siteDetector)
	if !ok {
		return nil, ErrUnsupported
	}
	free, e := s.remoteSlot(ctx)
	if e != nil {
		return nil, e
	}
	defer free()
	return detector.Detect(ctx, site)
}

func (c *platformConnector) Detect(ctx context.Context, site Site) (*DetectedSite, error) {
	base, e := url.Parse(site.BaseURL)
	if e != nil {
		return nil, ErrInvalid
	}
	for _, platform := range []string{"sub2api", "newapi"} {
		site.Platform = platform
		path := "/api/v1/settings/public"
		if platform == "newapi" {
			path = "/api/status"
		}
		var settings struct {
			SiteName     string `json:"site_name"`
			SystemName   string `json:"system_name"`
			Version      string `json:"version"`
			Turnstile    *bool  `json:"turnstile_enabled"`
			NewTurnstile *bool  `json:"turnstile_check"`
			Tencent      bool   `json:"tencent_captcha_enabled"`
			Aliyun       bool   `json:"aliyun_captcha_enabled"`
			Key          string `json:"turnstile_site_key"`
		}
		if e = c.data(ctx, site, Session{}, "GET", path, nil, &settings); e != nil {
			if errors.Is(e, ErrUnsupported) {
				continue
			}
			return nil, e
		}
		name, captcha := settings.SiteName, settings.Turnstile
		if platform == "newapi" {
			name, captcha = settings.SystemName, settings.NewTurnstile
		}
		if name == "" && settings.Version == "" && captcha == nil {
			continue
		}
		if len(name) > 100 || strings.TrimSpace(name) == "" {
			name = base.Hostname()
		}
		if len(name) > 100 || len(settings.Key) > 1024 {
			return nil, ErrUnsupported
		}
		return &DetectedSite{Platform: platform, Name: name, BaseURL: site.BaseURL, CaptchaRequired: captcha != nil && *captcha || settings.Tencent || settings.Aliyun, CaptchaSiteKey: settings.Key}, nil
	}
	return nil, ErrUnsupported
}
