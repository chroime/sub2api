package service

import (
	"context"
	"fmt"
	"html"
	"strings"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

var _ gov.KeyNotifier = (*governanceBalanceNotifier)(nil)

// Key notices share the existing administrator recipient resolver and system
// SMTP sender. Only a remote key ID is included; plaintext is never emailed.
func (n *governanceBalanceNotifier) SendKey(ctx context.Context, recipient string, notice gov.KeyNotice) error {
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
	title := "上游密钥已确认缺失 / Upstream key missing"
	if notice.Kind == "recovered" {
		title = "上游密钥已恢复 / Upstream key recovered"
	}
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] %s: %s", name, title, notice.SiteName))
	body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:24px;background:#f8fafc;color:#0f172a;font-family:Arial,sans-serif">
<main style="max-width:640px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:8px;padding:28px">
<h1 style="font-size:20px;margin:0 0 20px">%s</h1>
<table style="border-collapse:collapse;width:100%%;line-height:1.8">
<tr><td>上游 / Upstream</td><td>%s</td></tr><tr><td>地址 / URL</td><td>%s</td></tr>
<tr><td>分组 ID / Group ID</td><td>%s</td></tr><tr><td>密钥 ID / Key ID</td><td>%s</td></tr>
<tr><td>事件 / Event</td><td>%s</td></tr><tr><td>核验时间（北京时间，+08:00）</td><td>%s</td></tr></table>
<p>请在智能运维中核对托管密钥和受影响账户。邮件不包含 API Key 明文。<br>Review managed keys and affected accounts in Smart Operations. This email contains no API key value.</p>
<p style="font-size:12px;color:#64748b">%s</p></main></body></html>`,
		html.EscapeString(title), html.EscapeString(notice.SiteName), html.EscapeString(notice.BaseURL),
		html.EscapeString(notice.RemoteGroupID), html.EscapeString(notice.RemoteKeyID),
		html.EscapeString(notice.Kind), html.EscapeString(formatEmailTime(notice.ObservedAt)), html.EscapeString(name))
	return n.mail.SendEmail(ctx, recipient, subject, body)
}
