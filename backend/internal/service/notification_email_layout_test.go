package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupUpdateEmailLayoutAppliesToSavedTemplatesOnlyForGroupUpdates(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	const oldHTML = `<html><head><style>.container{max-width:640px}</style></head><body><div class="container"><div class="content">{{group_summary_html}}</div></div></body></html>`
	_, err := svc.UpdateTemplate(t.Context(), NotificationEmailEventSystemHealthSnapshot, "zh", "Group update", oldHTML)
	require.NoError(t, err)
	preview, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{Event: NotificationEmailEventSystemHealthSnapshot, Locale: "zh"})
	require.NoError(t, err)
	require.Contains(t, preview.HTML, `id="sub2api-group-email-layout"`)
	require.Contains(t, preview.HTML, "max-width: 960px !important")
	require.Contains(t, preview.HTML, "@media")
	saved, err := svc.GetTemplate(t.Context(), NotificationEmailEventSystemHealthSnapshot, "zh")
	require.NoError(t, err)
	require.Equal(t, oldHTML, saved.HTML, "layout must not overwrite the administrator's saved content")
	other, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{Event: NotificationEmailEventAuthVerifyCode, Locale: "zh"})
	require.NoError(t, err)
	require.NotContains(t, other.HTML, "sub2api-group-email-layout")
}

func TestGroupUpdateEmailTableKeepsLongDescriptionsAndMobileLabels(t *testing.T) {
	groups := &bulkEmailGroupsStub{groups: []Group{
		{ID: 1, Name: "OpenAI", Description: "OpenAI streaming TTFT verification", Status: StatusActive, RateMultiplier: .1},
		{ID: 2, Name: "[Demo] Public Claude + MiniMax", Description: "Demo data for homepage preview only; not a production channel.", Status: StatusActive, RateMultiplier: .065},
	}}
	users := &bulkEmailUsersStub{users: map[int64]*User{1: {ID: 1, Email: "user@example.com", Username: "邮件通知测试"}}}
	sender := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	svc := &UserBulkEmailService{users: users, sender: sender, groups: groups, accounts: &bulkEmailAccountsStub{}}
	links := bulkEmailTestLinks()
	preview, err := svc.Preview(t.Context(), UserBulkEmailPreviewInput{Event: NotificationEmailEventSystemHealthSnapshot, Locale: "zh", UserID: 1, Links: &links})
	require.NoError(t, err)
	require.Contains(t, preview.HTML, `class="group-summary-table"`)
	require.Contains(t, preview.HTML, `<colgroup>`)
	for _, label := range []string{"分组", "描述", "当前倍率"} {
		require.Contains(t, preview.HTML, `data-label="`+label+`"`)
		require.Contains(t, preview.HTML, `style="display:none;mso-hide:all;">`+label+`</span>`)
	}
	require.Contains(t, preview.HTML, "[Demo] Public Claude + MiniMax")
	require.Contains(t, preview.HTML, "Demo data for homepage preview only; not a production channel.")
	require.Contains(t, preview.HTML, "0.065x")
	require.NotContains(t, preview.HTML, `data-label="可用状态"`)
	require.Equal(t, 3, strings.Count(preview.HTML, `<th scope="col"`))
	require.Equal(t, 6, strings.Count(preview.HTML, `<td data-label=`))
	if dir := os.Getenv("NOTIFICATION_PREVIEW_DIR"); dir != "" {
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "groups-email.html"), []byte(preview.HTML), 0644))
	}
}

func TestGroupUpdateEditorPreviewUsesTheRealThreeColumnStructure(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, locale := range []string{"zh", "en"} {
		preview, err := svc.PreviewTemplate(t.Context(), NotificationEmailPreviewInput{Event: NotificationEmailEventSystemHealthSnapshot, Locale: locale})
		require.NoError(t, err)
		require.Contains(t, preview.HTML, `class="group-summary-table"`)
		require.Equal(t, 3, strings.Count(preview.HTML, `<th scope="col"`))
		require.Equal(t, 3, strings.Count(preview.HTML, `<td data-label=`))
		require.NotContains(t, preview.HTML, `data-label="可用状态"`)
		require.NotContains(t, preview.HTML, `data-label="Availability"`)
		require.Contains(t, preview.HTML, `class="group-cell-label"`)
	}
}
