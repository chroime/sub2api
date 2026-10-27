//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResolveHomepageTemplate(t *testing.T) {
	for _, tc := range []struct {
		name, template, content string
		compact                 bool
		want                    string
	}{
		{"new installation", "", "", false, "classic"},
		{"legacy custom", "", "<p>Hello</p>", true, "custom"},
		{"legacy compact", "", " \n", true, "compact"},
		{"explicit classic overrides legacy", "classic", "https://example.com", true, "classic"},
		{"explicit brand overrides legacy", "brand", "<p>Old content</p>", true, "brand"},
		{"explicit compact", "compact", "<p>Old content</p>", false, "compact"},
		{"explicit custom", "custom", "<p>Hello</p>", false, "custom"},
		{"invalid stored value", "unsupported", "", false, "classic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResolveHomepageTemplate(tc.template, tc.content, tc.compact))
		})
	}
}

func TestHomepageTemplatePublicAdminAndInjectionAgree(t *testing.T) {
	for _, template := range []string{"classic", "brand", "compact", "custom"} {
		t.Run(template, func(t *testing.T) {
			values := map[string]string{SettingKeyHomepageTemplate: template, SettingKeyHomeContent: "<p>Retained content</p>", SettingKeyCompactHomeEnabled: "true"}
			svc := NewSettingService(&settingPublicRepoStub{values: values}, &config.Config{})
			public, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, template, public.HomepageTemplate)
			require.Equal(t, template, svc.parseSettings(values).HomepageTemplate)
			injected, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			require.Equal(t, template, injected.(*PublicSettingsInjectionPayload).HomepageTemplate)
		})
	}
}

func TestHomepageTemplateUpdateValidationAndLegacyOmission(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{HomepageTemplate: "brand"})
	require.NoError(t, err)
	require.Equal(t, "brand", updates[SettingKeyHomepageTemplate])
	_, err = svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{HomepageTemplate: "not-a-template"})
	require.Error(t, err)
	updates, err = svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{})
	require.NoError(t, err)
	require.NotContains(t, updates, SettingKeyHomepageTemplate, "old clients must not reset the saved selection")
}
