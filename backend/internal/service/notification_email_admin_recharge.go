package service

import (
	"fmt"
	"html"
	"strings"
)

func adminRechargeEmailVariables(variables map[string]string) {
	email := strings.TrimSpace(variables["user_email"])
	notes := strings.TrimSpace(variables["user_notes"])
	variables["user_display"] = email
	if email != "" && notes != "" {
		variables["user_display"] += "（" + notes + "）"
	}
	amount := strings.TrimSpace(variables["pay_amount"])
	if strings.Contains(amount, ".") {
		amount = strings.TrimRight(strings.TrimRight(amount, "0"), ".")
	}
	variables["pay_amount_display"] = amount
}

func adminRechargeEmailTemplate(locale string) string {
	labels := []string{"User", "User ID", "Order ID", "Merchant order", "Actual payment", "Balance credited", "Payment method", "Completed at"}
	title := "User recharge completed"
	intro := "<p>Hello {{recipient_name}},</p><p>A user's recharge has completed and the balance has been credited.</p>"
	hint := "This notification is sent after the balance is credited. Actual payment and credited balance may use different currencies."
	if normalizeNotificationLocale(locale) == notificationEmailLocaleChinese {
		labels = []string{"用户", "用户 ID", "订单号", "商户订单号", "实付金额", "到账余额", "支付方式", "完成时间"}
		title = "用户充值成功"
		intro = "<p>{{recipient_name}}，您好：</p><p>用户充值已完成，余额已实际到账。</p>"
		hint = "本通知在余额到账后发送。实付金额与到账余额的币种可能不同，请分别核对。"
	}
	values := []string{
		"{{user_display}}", "{{user_id}}", "{{order_id}}", "{{out_trade_no}}",
		"<strong>{{payment_currency}} {{pay_amount}}</strong>", "<strong>USD {{recharge_amount}}</strong>",
		"{{payment_method}}", "{{completed_at}}",
	}
	var content strings.Builder
	content.WriteString(intro)
	// Inline sizing and wrapping also work in clients that strip media queries.
	content.WriteString(`<table class="recharge-details" role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;table-layout:fixed;border-collapse:collapse;text-align:left;">`)
	for index, label := range labels {
		fmt.Fprintf(&content, `<tr><td width="112" valign="top" style="width:112px;box-sizing:border-box;padding:8px 12px 8px 0;vertical-align:top;white-space:nowrap;font-size:14px;line-height:1.6;">%s</td><td valign="top" style="padding:8px 0;vertical-align:top;word-break:break-all;word-wrap:break-word;overflow-wrap:anywhere;font-size:15px;line-height:1.6;">%s</td></tr>`, html.EscapeString(label), values[index])
	}
	content.WriteString(`</table><p class="muted">` + hint + `</p>`)
	body := notificationEmailCard("#16a34a", title, content.String())
	const style = `<style>
@media only screen and (max-width: 480px) {
  body { padding: 12px !important; }
  .header, .content, .footer { padding-left: 20px !important; padding-right: 20px !important; }
  .header, .content { padding-top: 24px !important; padding-bottom: 24px !important; }
}
</style>`
	return strings.Replace(body, "</head>", style+"</head>", 1)
}
