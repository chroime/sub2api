package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotificationServiceLinksUsesFrontendOriginAndConfiguredUserCanvas(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(t.Context(), map[string]string{
		SettingKeyFrontendURL:     "https://platform.example/app/",
		SettingKeyAPIBaseURL:      "https://api.example/v1",
		SettingKeyDocURL:          "https://guide.example/start",
		SettingKeyCustomMenuItems: `[{"id":"private-canvas","label":"Canvas","url":"https://private.example","visibility":"admin"},{"id":"public-canvas","label":"无限画布","url":"https://canvas.example","visibility":"user"}]`,
	}))
	svc := NewNotificationEmailService(repo, nil)
	links, err := svc.ResolveServiceLinks(t.Context())
	require.NoError(t, err)
	require.Equal(t, "https://platform.example/app", links.PlatformURL)
	require.Equal(t, "https://guide.example/start", links.DocsURL)
	require.Equal(t, "https://platform.example/app/model-plaza", links.ModelPlazaURL)
	require.Equal(t, "https://canvas.example", links.CanvasURL)
}

func TestNotificationServiceLinksNeverInventsMissingPublicAddresses(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(t.Context(), map[string]string{
		SettingKeyAPIBaseURL:      "https://api.example/v1",
		SettingKeyDocURL:          "javascript:alert(1)",
		SettingKeyCustomMenuItems: `[{"label":"Canvas","url":"https://user:secret@canvas.example","visibility":"user"}]`,
	}))
	svc := NewNotificationEmailService(repo, nil)
	links, err := svc.ResolveServiceLinks(t.Context())
	require.NoError(t, err)
	require.Equal(t, NotificationEmailLinks{}, links)
	preview, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{Event: NotificationEmailEventSystemHealthSnapshot, Locale: "zh", RecipientEmail: "user@example.com"})
	require.NoError(t, err)
	require.NotContains(t, preview.HTML, "https://example.com")
	require.NotContains(t, preview.HTML, "javascript:")
}

func TestNotificationServiceLinksDerivesBuiltInRoutesButNotCanvasHost(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.Set(t.Context(), SettingKeyFrontendURL, "https://platform.example"))
	links, err := NewNotificationEmailService(repo, nil).ResolveServiceLinks(t.Context())
	require.NoError(t, err)
	require.Equal(t, "https://platform.example/docs", links.DocsURL)
	require.Empty(t, links.CanvasURL, "the canvas host must be explicitly configured")
}

func TestNotificationGroupsUpdateIncludesFourServiceLinksWithoutFooterUnsubscribe(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, locale := range []string{"zh", "en"} {
		preview, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{
			Event: NotificationEmailEventSystemHealthSnapshot, Locale: locale,
			RecipientEmail: "user@example.com", RecipientName: "Returning user",
			Variables: map[string]string{
				"platform_url":    "https://platform.example",
				"docs_url":        "https://guide.example",
				"model_plaza_url": "https://platform.example/model-plaza",
				"canvas_url":      "https://canvas.example",
			},
			RawHTMLVariables: map[string]string{"group_summary_html": `<table><tr><td>GPT Pro</td><td>0.06x</td></tr></table>`},
		})
		require.NoError(t, err)
		require.Contains(t, preview.HTML, `href="https://platform.example"`)
		require.Contains(t, preview.HTML, `href="https://guide.example"`)
		require.Contains(t, preview.HTML, `href="https://platform.example/model-plaza"`)
		require.Contains(t, preview.HTML, `href="https://canvas.example"`)
		require.NotContains(t, preview.HTML, "退订系统健康状态通知")
		require.NotContains(t, preview.HTML, "Unsubscribe from system health updates")
		require.Contains(t, preview.HTML, "0.06x")
	}
}
