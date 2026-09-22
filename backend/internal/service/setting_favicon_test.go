//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_SiteFaviconRoundTrip(t *testing.T) {
	const favicon = "data:image/png;base64,YWJj"
	readService := NewSettingService(&settingGetAllRepoStub{values: map[string]string{
		"site_logo":    "/page-logo.svg",
		"site_favicon": favicon,
	}}, &config.Config{})
	settings, err := readService.GetAllSettings(context.Background())
	require.NoError(t, err)

	writeRepo := &settingUpdateRepoStub{}
	writeService := NewSettingService(writeRepo, &config.Config{})
	require.NoError(t, writeService.UpdateSettings(context.Background(), settings))
	require.Equal(t, favicon, writeRepo.updates["site_favicon"])
	require.Equal(t, "/page-logo.svg", writeRepo.updates["site_logo"])
}

func TestSettingService_SiteFaviconInjectionShape(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{name: "independent", values: map[string]string{"site_favicon": "/tab-icon.svg"}, want: "/tab-icon.svg"},
		{name: "missing", values: map[string]string{}, want: ""},
		{name: "cleared", values: map[string]string{"site_favicon": ""}, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.values["site_logo"] = "/page-logo.svg"
			svc := NewSettingService(&settingPublicRepoStub{values: tt.values}, &config.Config{})
			payload, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(raw, &fields))
			require.Contains(t, fields, "site_favicon")
			require.Equal(t, tt.want, fields["site_favicon"])
			require.Equal(t, "/page-logo.svg", fields["site_logo"])
		})
	}
}
