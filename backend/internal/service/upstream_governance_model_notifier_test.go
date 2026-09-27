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
	require.Contains(t, body, "2026-09-27 14:00:00")
	require.Contains(t, body, "token_suspicious")
	require.NoError(t, repo.Set(t.Context(), SettingKeySMTPFrom, "updated-sender@example.test"))
	notice.Kind = "recovery"
	require.NoError(t, adapter.SendModel(t.Context(), "admin@example.test", notice))
	message, err := mail.ReadMessage(strings.NewReader(server.lastMessage()))
	require.NoError(t, err)
	require.Contains(t, message.Header.Get("From"), "updated-sender@example.test")
	require.Contains(t, server.lastMessageBody(t), "Upstream model recovered")
}

func TestGovernanceModelMailMissingSMTPAndCancellation(t *testing.T) {
	adapter := &governanceBalanceNotifier{}
	require.ErrorIs(t, adapter.SendModel(t.Context(), "admin@example.test", gov.ModelNotice{}), ErrEmailNotConfigured)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter.mail = NewEmailService(newNotificationEmailMemorySettingRepo(), nil)
	require.ErrorIs(t, adapter.SendModel(ctx, "admin@example.test", gov.ModelNotice{}), context.Canceled)
}
