package service

import (
	"context"
	"net/mail"
	"strings"
	"testing"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceModelMailUsesSystemSMTPAndEscapesEvidence(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	server := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
	adapter := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}
	notice := gov.ModelNotice{SiteID: 7, SiteName: "<script>site</script>\r\nInjected", BaseURL: "https://fixture.example", PolicyName: "token-audit", Model: "<model>", Kind: "token_suspicious", Detail: "<img src=x onerror=alert(1)>", ObservedAt: time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC)}
	require.NoError(t, adapter.SendModel(t.Context(), "admin@example.test", notice))
	require.Equal(t, int64(1), server.messageCount())
	body := server.lastMessageBody(t)
	require.Contains(t, body, "&lt;script&gt;site&lt;/script&gt;")
	require.Contains(t, body, "&lt;img src=x onerror=alert(1)&gt;")
	require.NotContains(t, body, "<script>")
	require.Contains(t, body, "2026-09-27 22:00:00")
	require.Contains(t, body, "令牌数据异常")
	require.NoError(t, repo.Set(t.Context(), SettingKeySMTPFrom, "updated-sender@example.test"))
	notice.Kind = "recovery"
	require.NoError(t, adapter.SendModel(t.Context(), "admin@example.test", notice))
	message, err := mail.ReadMessage(strings.NewReader(server.lastMessage()))
	require.NoError(t, err)
	require.Contains(t, message.Header.Get("From"), "updated-sender@example.test")
	require.Contains(t, server.lastMessageBody(t), "上游模型监测已恢复")
}

func TestGovernanceModelMailMissingSMTPAndCancellation(t *testing.T) {
	adapter := &governanceBalanceNotifier{}
	require.ErrorIs(t, adapter.SendModel(t.Context(), "admin@example.test", gov.ModelNotice{}), ErrEmailNotConfigured)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter.mail = NewEmailService(newNotificationEmailMemorySettingRepo(), nil)
	require.ErrorIs(t, adapter.SendModel(ctx, "admin@example.test", gov.ModelNotice{}), context.Canceled)
}

func TestGovernanceModelMailUsesChineseLabelsAndTrustSummary(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	server := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
	adapter := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}
	notice := gov.ModelNotice{
		SiteID:     7,
		SiteName:   "测试上游",
		BaseURL:    "https://fixture.example",
		PolicyName: "模型可信度",
		Model:      "gpt-6-astra",
		Kind:       "token_suspicious",
		Detail:     "输入 token 差异 12.50%，糖果答案待复核。",
		ObservedAt: time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC),
	}
	require.NoError(t, adapter.SendModel(t.Context(), "admin@example.test", notice))
	body := server.lastMessageBody(t)
	require.Contains(t, body, "上游模型令牌数据需复核")
	require.Contains(t, body, "需要复核")
	require.Contains(t, body, "2026-10-02 21:00:00")
	require.Contains(t, body, "北京时间（UTC+08:00）")
	require.NotContains(t, body, "Upstream")
	require.NotContains(t, body, "Token")
	require.NotContains(t, body, "Event")
	require.NotContains(t, body, "token_suspicious")
}

func TestGovernanceModelMailLocalizesQueuedEnglishDetails(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	server := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
	adapter := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}
	notice := gov.ModelNotice{
		SiteName:   "测试上游",
		BaseURL:    "https://fixture.example",
		PolicyName: "可信度",
		Model:      "gpt-6-astra",
		Kind:       "quality_failed",
		Detail:     "Administrator-reviewed quality: failed tiers=1, passed tiers=2. Original model results are unchanged.",
		ObservedAt: time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC),
	}
	require.NoError(t, adapter.SendModel(t.Context(), "admin@example.test", notice))
	body := server.lastMessageBody(t)
	require.Contains(t, body, "管理员质量复核")
	require.Contains(t, body, "失败层级=1")
	require.NotContains(t, body, "Administrator")
	require.NotContains(t, body, "quality")
	require.NotContains(t, body, "Original")
}
