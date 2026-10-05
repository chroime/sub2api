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
	title := modelNoticeTitle(notice.Kind)
	kind := modelNoticeKindLabel(notice.Kind)
	detail := localizeModelNoticeDetail(notice.Detail)
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] %s: %s", name, title, notice.SiteName))
	body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:24px;background:#f8fafc;color:#0f172a">
<main style="max-width:640px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:28px">
<h1 style="font-size:22px;margin:0 0 20px">%s</h1>
<table style="border-collapse:collapse;width:100%%;line-height:1.8">
<tr><td>上游名称</td><td>%s</td></tr><tr><td>站点地址</td><td>%s</td></tr>
<tr><td>监测策略</td><td>%s</td></tr><tr><td>模型</td><td>%s</td></tr>
<tr><td>事件类型</td><td>%s</td></tr><tr><td>检测时间：北京时间（UTC+08:00）</td><td>%s</td></tr></table>
<p style="white-space:pre-wrap">%s</p>
<p>请在管理员后台的上游治理中心查看原始记录与核验依据。令牌差异需结合计数口径判断，不能单独认定为篡改。</p>
<p style="font-size:12px;color:#64748b">%s</p></main></body></html>`, html.EscapeString(title), html.EscapeString(notice.SiteName), html.EscapeString(notice.BaseURL), html.EscapeString(notice.PolicyName), html.EscapeString(notice.Model), html.EscapeString(kind), html.EscapeString(formatEmailTime(notice.ObservedAt)), html.EscapeString(detail), html.EscapeString(name))
	return n.mail.SendEmail(ctx, recipient, subject, body)
}

func modelNoticeTitle(kind string) string {
	switch kind {
	case "recovery", "recovered", "quality_recovered":
		return "上游模型监测已恢复"
	case "quality_failed":
		return "上游模型质量检测异常"
	case "quality_review_retracted":
		return "上游模型质量复核已撤回"
	case "token_suspicious":
		return "上游模型令牌数据需复核"
	case "failure":
		return "上游模型监测异常"
	default:
		return "上游模型监测通知"
	}
}

func modelNoticeKindLabel(kind string) string {
	switch kind {
	case "recovery", "recovered":
		return "监测已恢复"
	case "quality_recovered":
		return "质量检测已恢复"
	case "quality_failed":
		return "质量检测异常"
	case "quality_review_retracted":
		return "质量复核已撤回"
	case "token_suspicious":
		return "令牌数据异常（需要复核）"
	case "failure":
		return "连续检测失败"
	default:
		return "模型监测通知"
	}
}

func localizeModelNoticeDetail(detail string) string {
	// New worker notices are written in Chinese. Keep a small compatibility
	// translation for queued notices created by older releases so retries cannot
	// reintroduce English labels into administrator mail.
	for _, replacement := range []struct{ old, new string }{
		{"Administrator-reviewed quality", "管理员质量复核"},
		{"Original model results are unchanged.", "原始模型结果未修改。"},
		{"token_suspicious", "令牌数据异常"},
		{"failed tiers", "失败层级"},
		{"passed tiers", "通过层级"},
		{"batch", "批次"},
		{"failed=", "失败="},
		{"succeeded=", "成功="},
		{"token_suspicious=", "令牌异常="},
		{"Token", "令牌"},
		{"token", "令牌"},
	} {
		detail = strings.ReplaceAll(detail, replacement.old, replacement.new)
	}
	return detail
}
