package service

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

var _ gov.ChangeNotifier = (*governanceBalanceNotifier)(nil)

func governanceChangeKindText(kind string) string {
	switch kind {
	case "rate_change":
		return "倍率变化"
	case "group_change", "catalog_change":
		return "分组目录变化"
	case "pricing_change":
		return "本地调价变化"
	case "protection_change":
		return "亏损保护变化"
	case "balance":
		return "余额变化"
	case "key":
		return "托管密钥变化"
	case "model":
		return "模型检测变化"
	default:
		return "治理变化"
	}
}

func governanceChangeSeverityText(severity string) string {
	switch severity {
	case "critical":
		return "严重"
	case "warning":
		return "警告"
	case "info":
		return "提示"
	default:
		return "待确认"
	}
}

// SendChange uses the same system SMTP transport and administrator recipient
// resolver as balance/key/model governance alerts. No additional SMTP or
// recipient settings are introduced for catalog and pricing changes.
func (n *governanceBalanceNotifier) SendChange(ctx context.Context, recipient string, notice gov.ChangeNotice) error {
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
	title := strings.TrimSpace(notice.Subject)
	if title == "" {
		title = "上游治理变化"
	}
	upstreamName := strings.TrimSpace(notice.SiteName)
	if upstreamName == "" {
		upstreamName = "见事件详情"
	}
	baseURL := strings.TrimSpace(notice.BaseURL)
	if baseURL == "" {
		baseURL = "见事件详情"
	}
	// Subject is rendered by the governance event producer but is still passed
	// through the existing SMTP header sanitizer before it reaches the transport.
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] %s", name, title))
	observed := notice.ObservedAt
	if observed.IsZero() {
		observed = time.Now()
	}
	body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:24px;background:#f8fafc;color:#0f172a;font-family:Arial,sans-serif">
<main style="max-width:680px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:28px">
<h1 style="font-size:22px;margin:0 0 20px">%s</h1>
<table style="border-collapse:collapse;width:100%%;line-height:1.8">
<tr><td>上游名称</td><td>%s</td></tr><tr><td>站点URL</td><td>%s</td></tr>
<tr><td>事件类型</td><td>%s</td></tr><tr><td>严重级别</td><td>%s</td></tr>
<tr><td>观察时间：北京时间（UTC+08:00）</td><td>%s</td></tr></table>
<p style="white-space:pre-wrap">%s</p>
<p>请在智能运维中查看原始目录、倍率、定价与保护记录。</p>
<p style="font-size:12px;color:#64748b">%s</p></main></body></html>`,
		html.EscapeString(title), html.EscapeString(upstreamName), html.EscapeString(baseURL),
		html.EscapeString(governanceChangeKindText(notice.Kind)), html.EscapeString(governanceChangeSeverityText(notice.Severity)),
		html.EscapeString(formatEmailTime(observed)), html.EscapeString(notice.Body), html.EscapeString(name))
	return n.mail.SendEmail(ctx, recipient, subject, body)
}
