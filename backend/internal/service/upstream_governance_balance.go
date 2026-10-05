package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

type governanceBalanceMail interface {
	SendEmail(context.Context, string, string, string) error
}

type governanceBalanceAdminReader interface {
	GetFirstAdmin(context.Context) (*User, error)
}

// The adapter deliberately has no SMTP settings of its own. EmailService reads
// the system SMTP transport and From address again for every delivery.
type governanceBalanceNotifier struct {
	mail     governanceBalanceMail
	settings SettingRepository
	admins   governanceBalanceAdminReader
}

func (n *governanceBalanceNotifier) Recipients(ctx context.Context, override []string) ([]string, error) {
	if len(override) > 0 {
		return append([]string{}, override...), nil
	}
	if n.settings != nil {
		raw, err := n.settings.GetValue(ctx, SettingKeyAccountQuotaNotifyEmails)
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			return nil, err
		}
		if recipients := filterVerifiedEmails(ParseNotifyEmails(raw)); len(recipients) > 0 {
			return recipients, nil
		}
	}
	if n.admins != nil {
		admin, err := n.admins.GetFirstAdmin(ctx)
		if err != nil {
			return nil, err
		}
		if admin != nil && admin.IsAdmin() && admin.IsActive() && strings.TrimSpace(admin.Email) != "" {
			return []string{strings.TrimSpace(admin.Email)}, nil
		}
	}
	return nil, errors.New("no administrator notification recipient configured")
}

func (n *governanceBalanceNotifier) Send(ctx context.Context, recipient string, notice gov.BalanceNotice) error {
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
	title := "上游余额不足"
	if notice.Recovered {
		title = "上游余额已恢复"
	}
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] %s：%s", name, title, notice.SiteName))
	description := "当前余额已达到或低于告警阈值，请及时充值。"
	if notice.Recovered {
		description = "当前余额已回升至告警阈值以上，余额告警已恢复。"
	}
	body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:24px;background:#f8fafc;color:#0f172a;font-family:Arial,sans-serif">
<main style="max-width:640px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:28px">
<h1 style="font-size:22px;margin:0 0 20px">%s</h1>
<p><strong>%s</strong>：%s</p>
<table style="border-collapse:collapse;width:100%%;line-height:1.8">
<tr><td>上游名称</td><td>%s</td></tr><tr><td>站点URL</td><td>%s</td></tr><tr><td>平台</td><td>%s</td></tr>
<tr><td>当前余额</td><td><strong>%s</strong></td></tr>
<tr><td>告警阈值</td><td>%s</td></tr><tr><td>通知时间：北京时间（UTC+08:00）</td><td>%s</td></tr></table>
<p>金额保留上游原生单位。</p>
<p style="font-size:12px;color:#64748b">%s</p>
</main></body></html>`, html.EscapeString(title), html.EscapeString(notice.SiteName), html.EscapeString(description), html.EscapeString(notice.SiteName), html.EscapeString(notice.BaseURL), html.EscapeString(notice.Platform),
		html.EscapeString(fmt.Sprintf("%g %s", notice.Balance, notice.Unit)), html.EscapeString(fmt.Sprintf("%g %s", notice.Threshold, notice.Unit)),
		html.EscapeString(formatEmailTime(notice.ObservedAt)), html.EscapeString(name))
	return n.mail.SendEmail(ctx, recipient, subject, body)
}
