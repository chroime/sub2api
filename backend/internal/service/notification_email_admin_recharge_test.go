package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/stretchr/testify/require"
)

const adminRechargeEmailTestEvent = "payment.recharge_success_admin"

func TestNotificationEmailAdminRechargeEventAndOfficialTemplates(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	var found *NotificationEmailEventInfo
	for _, info := range svc.ListEventInfos() {
		if info.Event == adminRechargeEmailTestEvent {
			copy := info
			found = &copy
		}
	}
	require.NotNil(t, found, "administrator recharge events must be discoverable in the template editor")
	require.Equal(t, "admin", found.Category)
	require.False(t, found.Optional, "recharge administration emails are transactional")
	for _, placeholder := range []string{"user_id", "user_email", "user_name", "user_notes", "user_display", "order_id", "out_trade_no", "pay_amount", "pay_amount_display", "payment_currency", "recharge_amount", "payment_method", "completed_at"} {
		require.Contains(t, found.Placeholders, placeholder)
	}
	for _, locale := range []string{"zh", "en"} {
		t.Run(locale, func(t *testing.T) {
			preview, err := svc.PreviewTemplate(t.Context(), NotificationEmailPreviewInput{Event: adminRechargeEmailTestEvent, Locale: locale})
			require.NoError(t, err)
			require.NotEmpty(t, preview.Subject)
			require.NotContains(t, preview.HTML, "{{")
			require.Contains(t, preview.HTML, "CNY")
			require.Contains(t, preview.HTML, "USD")
			require.NotContains(t, preview.HTML, "token=")
		})
	}
}

func TestNotificationEmailAdminRechargeEscapesUserAndSeparatesRecipient(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	preview, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{
		Event: adminRechargeEmailTestEvent, Locale: "zh", RecipientEmail: "admin@example.test", RecipientName: "管理员",
		Variables: map[string]string{
			"user_id": "73", "user_email": "payer@example.test", "user_name": `<img src=x onerror=alert(1)>`,
			"user_notes": `<img src=x onerror=alert(1)>`,
			"order_id":   "73", "out_trade_no": "20261111PAID73", "pay_amount": "0.10", "payment_currency": "CNY", "recharge_amount": "0.14", "payment_method": "支付宝", "completed_at": "2026-11-11 18:00:00 +08:00",
		},
	})
	require.NoError(t, err)
	require.Contains(t, preview.HTML, "管理员")
	require.Contains(t, preview.HTML, "payer@example.test")
	require.Contains(t, preview.HTML, "&lt;img")
	require.NotContains(t, preview.HTML, "<img src=x")
	require.Contains(t, preview.HTML, "CNY 0.10")
	require.Contains(t, preview.HTML, "USD 0.14")
	require.NotContains(t, preview.HTML, "{{")
}

func TestNotificationEmailAdminRechargeLiveMissingValuesDoNotUsePreviewSamples(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	svc := NewNotificationEmailService(repo, nil)
	placeholders := []string{"user_id", "user_email", "user_name", "user_notes", "user_display", "order_id", "out_trade_no", "pay_amount", "pay_amount_display", "payment_currency", "recharge_amount", "payment_method", "completed_at", "recipient_name"}
	parts := make([]string, len(placeholders))
	for i, placeholder := range placeholders {
		parts[i] = "{{" + placeholder + "}}"
	}
	_, err := svc.UpdateTemplate(t.Context(), adminRechargeEmailTestEvent, "en", "Recharge", "<p>"+strings.Join(parts, "|")+"</p>")
	require.NoError(t, err)
	preview, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{Event: adminRechargeEmailTestEvent, Locale: "en", RecipientEmail: "admin@example.test"})
	require.NoError(t, err)
	require.Equal(t, "<p>"+strings.Repeat("|", len(parts)-1)+"</p>", preview.HTML)
}

func TestNotificationEmailAdminRechargeUserDetailsAndSubject(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.Set(t.Context(), SettingKeySiteName, "XenoAI"))
	svc := NewNotificationEmailService(repo, nil)
	for _, tc := range []struct {
		name, notes, amount, amountDisplay, userDisplay string
	}{
		{"no_notes", "", "10.00", "10", "payer@example.test"},
		{"blank_notes", " \r\n\t", "0.10", "0.1", "payer@example.test"},
		{"notes", " VIP customer ", "10.50", "10.5", "payer@example.test（VIP customer）"},
		{"one_hundred", "", "100.00", "100", "payer@example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			variables := map[string]string{
				"user_id": "73", "user_email": "payer@example.test", "user_name": "Ignored username", "user_notes": tc.notes,
				"order_id": "33", "out_trade_no": "20261112194730QJRO5MZ2B3", "pay_amount": tc.amount,
				"payment_currency": "CNY", "recharge_amount": "10.00", "payment_method": "alipay", "completed_at": "2026-11-12 22:49:50 +08:00",
			}
			preview, err := svc.PreviewSend(t.Context(), NotificationEmailSendInput{
				Event: adminRechargeEmailTestEvent, Locale: "zh", RecipientEmail: "admin@example.test", RecipientName: "管理员", Variables: variables,
			})
			require.NoError(t, err)
			require.Equal(t, "[XenoAI] 收款到账通知 - 金额 "+tc.amountDisplay, preview.Subject)
			require.Contains(t, preview.HTML, tc.userDisplay)
			require.Equal(t, 1, strings.Count(preview.HTML, "payer@example.test"))
			require.NotContains(t, preview.HTML, "Ignored username")
			require.NotContains(t, preview.HTML, "充值用户")
			if strings.TrimSpace(tc.notes) == "" {
				require.NotContains(t, preview.HTML, "payer@example.test（")
				require.NotContains(t, preview.HTML, "payer@example.test (")
			}
			for _, detail := range []string{"20261112194730QJRO5MZ2B3", "CNY " + tc.amount, "USD 10.00", "alipay", "2026-11-12 22:49:50 +08:00"} {
				require.Contains(t, preview.HTML, detail)
			}
			editorPreview, err := svc.PreviewTemplate(t.Context(), NotificationEmailPreviewInput{Event: adminRechargeEmailTestEvent, Locale: "zh", Variables: variables})
			require.NoError(t, err)
			require.Equal(t, preview.Subject, editorPreview.Subject)
			require.Contains(t, editorPreview.HTML, tc.userDisplay)
		})
	}
}

func TestNotificationEmailAdminRechargeSubjectUsesConfiguredSiteName(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	svc := NewNotificationEmailService(repo, nil)
	for _, siteName := range []string{"Oreniva", "星河 AI"} {
		require.NoError(t, repo.Set(t.Context(), SettingKeySiteName, siteName))
		for _, locale := range []string{"zh", "en"} {
			input := NotificationEmailSendInput{
				Event: adminRechargeEmailTestEvent, Locale: locale, RecipientEmail: "admin@example.test",
				Variables: map[string]string{"pay_amount": "100.00"},
			}
			preview, err := svc.PreviewSend(t.Context(), input)
			require.NoError(t, err)
			want := "[" + siteName + "] 收款到账通知 - 金额 100"
			if locale == "en" {
				want = "[" + siteName + "] Payment received - amount 100"
			}
			require.Equal(t, want, preview.Subject)
			editorPreview, err := svc.PreviewTemplate(t.Context(), NotificationEmailPreviewInput{Event: input.Event, Locale: locale, Variables: input.Variables})
			require.NoError(t, err)
			require.Equal(t, want, editorPreview.Subject)
		}
	}
}

func TestNotificationEmailAdminRechargeMobilePreview(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, locale := range []string{"zh", "en"} {
		preview, err := svc.PreviewTemplate(t.Context(), NotificationEmailPreviewInput{
			Event: adminRechargeEmailTestEvent, Locale: locale, Variables: map[string]string{
				"user_email": "mobile.recharge.notice@example.test", "user_name": "mobile.recharge.notice@example.test", "user_notes": "VIP 客户 / priority customer",
				"order_id": "1337", "out_trade_no": "20261112194730QJRO5MZ2B3", "pay_amount": "20.00", "recharge_amount": "20.00",
			},
		})
		require.NoError(t, err)
		require.Equal(t, 1, strings.Count(preview.HTML, "mobile.recharge.notice@example.test"))
		if dir := os.Getenv("NOTIFICATION_PREVIEW_DIR"); dir != "" {
			require.NoError(t, os.MkdirAll(dir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "recharge-admin-"+locale+".html"), []byte(preview.HTML), 0644))
		}
	}
}

func TestNotificationEmailAdminRechargeTracksActualDeliveryAndDuplicates(t *testing.T) {
	notifications, _, _, store, smtp := balanceRecordFixture(t)
	input := NotificationEmailSendInput{Event: adminRechargeEmailTestEvent, Locale: "zh", RecipientEmail: "admin@example.test", SourceType: "payment_order", SourceID: "73", Variables: map[string]string{"order_id": "73", "user_id": "7", "user_email": "payer@example.test", "pay_amount": "0.10", "payment_currency": "CNY", "recharge_amount": "0.10"}}
	for attempt := 0; attempt < 2; attempt++ {
		input.BalanceDelivery = notifications.BalanceNotificationRecords().Begin(balancenotice.Input{EventType: adminRechargeEmailTestEvent, UserID: 7, UserEmail: "payer@example.test", Recipient: input.RecipientEmail, DeliveryKey: notificationEmailDeliveryKey(input.Event, input.SourceType, input.SourceID, input.RecipientEmail, "")})
		require.NotNil(t, input.BalanceDelivery)
		delivered, err := notifications.SendDelivered(t.Context(), input)
		require.NoError(t, err)
		require.Equal(t, attempt == 0, delivered)
	}
	require.Equal(t, int64(1), smtp.messageCount())
	require.Len(t, store.rows, 2)
	states := map[string]int{}
	for _, row := range store.rows {
		require.Equal(t, adminRechargeEmailTestEvent, row.EventType)
		require.Equal(t, "payer@example.test", row.UserEmail)
		require.Equal(t, "admin@example.test", row.Recipient)
		states[row.Status]++
	}
	require.Equal(t, map[string]int{"sent": 1, "suppressed": 1}, states)
}
