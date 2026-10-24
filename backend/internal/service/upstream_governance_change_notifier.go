package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
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
	if strings.HasPrefix(recipient, "qq:") {
		return n.sendAstrBot(ctx, strings.TrimPrefix(recipient, "qq:"), notice)
	}
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
	if notice.Kind == "group_changes" {
		// The persisted body already contains source context, one line per
		// selected event and a single final Beijing timestamp.
		content := strings.ReplaceAll(html.EscapeString(notice.Body), "\n", "<br>\n")
		body := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head><body style="margin:0;padding:16px;background:#f8fafc;color:#0f172a;font-family:Arial,sans-serif">
<main style="max-width:960px;margin:auto;background:#fff;border:1px solid #e2e8f0;border-radius:8px;padding:24px">
<div style="font-size:15px;line-height:1.9;overflow-wrap:anywhere;word-break:break-word">%s</div>
</main></body></html>`, content)
		return n.mail.SendEmail(ctx, recipient, subject, body)
	}
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

func (n *governanceBalanceNotifier) globalSettings(ctx context.Context) (gov.GlobalSettings, error) {
	if n != nil && n.runtimeGlobalSettings != nil {
		return n.runtimeGlobalSettings(ctx)
	}
	if n == nil || n.settings == nil {
		return gov.GlobalSettings{}, ErrSettingNotFound
	}
	raw, err := n.settings.GetValue(ctx, gov.GlobalSettingsKey)
	if err != nil {
		return gov.GlobalSettings{}, err
	}
	return gov.ParseGlobalSettings(raw)
}

func (n *governanceBalanceNotifier) sendAstrBot(ctx context.Context, umo string, notice gov.ChangeNotice) error {
	settings, err := n.globalSettings(ctx)
	if err != nil || !settings.AstrBot.Enabled || settings.AstrBot.BaseURL == "" || settings.AstrBot.APIKey == "" {
		if err != nil {
			return err
		}
		return ErrEmailNotConfigured
	}
	path := settings.AstrBot.EndpointPath
	if path == "" {
		path = "/api/v1/im/message"
	}
	endpoint := governanceAstrBotEndpoint(settings.AstrBot.BaseURL, path)
	messageText := strings.TrimSpace(notice.Subject + "\n" + notice.Body)
	if strings.TrimSpace(notice.AstrBotText) != "" {
		messageText = strings.TrimSpace(notice.AstrBotText)
	}
	body, err := json.Marshal(map[string]any{
		"umo":     umo,
		"message": []map[string]string{{"type": "plain", "text": messageText}},
	})
	if err != nil {
		return err
	}
	status, responseBody, err := n.postJSON(ctx, endpoint, settings.AstrBot.APIKey, body, settings.AstrBot.TimeoutSeconds)
	if err != nil {
		return err
	}
	// Older AstrBot deployments expose the plural path. Retry only that known
	// compatibility variation after a 404; all other failures use the queue's
	// normal retry policy.
	if status == http.StatusNotFound && strings.HasSuffix(path, "/message") && !strings.HasSuffix(path, "/messages") {
		fallback := governanceAstrBotEndpoint(settings.AstrBot.BaseURL, strings.TrimSuffix(path, "/message")+"/messages")
		status, responseBody, err = n.postJSON(ctx, fallback, settings.AstrBot.APIKey, body, settings.AstrBot.TimeoutSeconds)
	}
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("astrbot notification returned status %d: %s", status, strings.TrimSpace(responseBody))
	}
	return nil
}

func governanceAstrBotEndpoint(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	path = "/" + strings.TrimLeft(path, "/")
	const apiVersion = "/api/v1"
	if strings.HasSuffix(baseURL, apiVersion) {
		if path == apiVersion || strings.HasPrefix(path, apiVersion+"/") {
			path = strings.TrimPrefix(path, apiVersion)
		}
	} else if strings.HasPrefix(path, "/im/") {
		path = apiVersion + path
	}
	return baseURL + path
}

func (n *governanceBalanceNotifier) postJSON(ctx context.Context, endpoint, apiKey string, body []byte, timeoutSeconds int64) (int, string, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if readErr != nil {
		return response.StatusCode, "", readErr
	}
	return response.StatusCode, string(data), nil
}
