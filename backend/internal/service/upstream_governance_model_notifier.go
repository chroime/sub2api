package service

import (
	"context"
	"fmt"
	"html"
	"strings"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

var _ gov.ModelNotifier = (*governanceBalanceNotifier)(nil)

// Model monitoring shares the system SMTP and administrator recipient resolver.
func (n *governanceBalanceNotifier) SendModel(ctx context.Context, recipient string, notice gov.ModelNotice) error {
	if n.mail == nil {
		return ErrEmailNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name := defaultSiteName
	if n.settings != nil {
		if configured, err := n.settings.GetValue(ctx, SettingKeySiteName); err == nil && strings.TrimSpace(configured) != "" {
			name = configured
		}
	}
	title := "上游模型监测异常 / Upstream model alert"
	if notice.Kind == "recovery" || notice.Kind == "recovered" || notice.Kind == "quality_recovered" {
		title = "上游模型监测恢复 / Upstream model recovered"
	}
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] %s: %s", name, title, notice.SiteName))
	body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:24px;background:#f8fafc;color:#0f172a;font-family:Arial,sans-serif">
<main style="max-width:640px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:28px">
<h1 style="font-size:22px;margin:0 0 20px">%s</h1>
<table style="border-collapse:collapse;width:100%%;line-height:1.8">
<tr><td>上游 / Upstream</td><td>%s</td></tr><tr><td>地址 / URL</td><td>%s</td></tr>
<tr><td>监测策略 / Policy</td><td>%s</td></tr><tr><td>模型 / Model</td><td>%s</td></tr>
<tr><td>事件 / Event</td><td>%s</td></tr><tr><td>检测时间（北京时间，+08:00）</td><td>%s</td></tr></table>
<p style="white-space:pre-wrap">%s</p>
<p>请在管理员后台的上游治理中心查看原始记录与核验依据。Token 差异需结合计数口径判断。<br>Review the original evidence in the administrator upstream governance center. Token differences depend on accounting scope.</p>
<p style="font-size:12px;color:#64748b">%s</p></main></body></html>`, html.EscapeString(title), html.EscapeString(notice.SiteName), html.EscapeString(notice.BaseURL), html.EscapeString(notice.PolicyName), html.EscapeString(notice.Model), html.EscapeString(notice.Kind), html.EscapeString(formatEmailTime(notice.ObservedAt)), html.EscapeString(notice.Detail), html.EscapeString(name))
	return n.mail.SendEmail(ctx, recipient, subject, body)
}
