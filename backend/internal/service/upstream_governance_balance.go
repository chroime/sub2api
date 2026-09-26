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
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] 上游余额不足 / Upstream balance low: %s", name, notice.SiteName))
	body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:24px;background:#f8fafc;color:#0f172a;font-family:Arial,sans-serif">
<main style="max-width:640px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:28px">
<h1 style="font-size:22px;margin:0 0 20px">上游余额不足 / Upstream balance low</h1>
<p><strong>%s</strong> 的可用余额已达到或低于告警阈值。<br>The upstream available balance is at or below its configured threshold.</p>
<table style="border-collapse:collapse;width:100%%;line-height:1.8">
<tr><td>站点 / Site</td><td>%s</td></tr><tr><td>平台 / Platform</td><td>%s</td></tr>
<tr><td>可用余额 / Available</td><td><strong>%s</strong></td></tr>
<tr><td>告警阈值 / Threshold</td><td>%s</td></tr><tr><td>采集时间 / Observed (UTC)</td><td>%s</td></tr></table>
<p>请在上游站点检查余额并及时充值。<br>Review the upstream balance and recharge when needed.</p>
<p style="font-size:12px;color:#64748b">金额保留上游原生单位 / Values retain the upstream native unit.<br>%s</p>
</main></body></html>`, html.EscapeString(notice.SiteName), html.EscapeString(notice.BaseURL), html.EscapeString(notice.Platform),
		html.EscapeString(fmt.Sprintf("%g %s", notice.Balance, notice.Unit)), html.EscapeString(fmt.Sprintf("%g %s", notice.Threshold, notice.Unit)),
		html.EscapeString(notice.ObservedAt.UTC().Format("2006-01-02 15:04:05")), html.EscapeString(name))
	return n.mail.SendEmail(ctx, recipient, subject, body)
}
