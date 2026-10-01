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

func TestGovernanceKeyMailUsesSystemSMTPAndEscapesDetails(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	server := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
	adapter := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}
	notice := gov.KeyNotice{Kind: "missing", SiteID: 7, SiteName: "<script>site</script>\r\nInjected", BaseURL: "https://fixture.example", RemoteGroupID: "<group>", RemoteKeyID: "123", ObservedAt: time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)}
	require.NoError(t, adapter.SendKey(t.Context(), "admin@example.test", notice))
	require.Equal(t, int64(1), server.messageCount())
	body := server.lastMessageBody(t)
	require.Contains(t, body, "&lt;script&gt;site&lt;/script&gt;")
	require.Contains(t, body, "&lt;group&gt;")
	require.NotContains(t, body, "<script>")
	require.Contains(t, body, "2026-09-30 16:00:00")
	require.NotContains(t, body, "fixture-inference-key")
	require.NoError(t, repo.Set(t.Context(), SettingKeySMTPFrom, "updated-sender@example.test"))
	notice.Kind = "recovered"
	require.NoError(t, adapter.SendKey(t.Context(), "admin@example.test", notice))
	message, err := mail.ReadMessage(strings.NewReader(server.lastMessage()))
	require.NoError(t, err)
	require.Contains(t, message.Header.Get("From"), "updated-sender@example.test")
	require.Contains(t, server.lastMessageBody(t), "recovered")
}

func TestGovernanceKeyMailMissingSMTPAndCancellation(t *testing.T) {
	adapter := &governanceBalanceNotifier{}
	require.ErrorIs(t, adapter.SendKey(t.Context(), "admin@example.test", gov.KeyNotice{}), ErrEmailNotConfigured)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter.mail = NewEmailService(newNotificationEmailMemorySettingRepo(), nil)
	require.ErrorIs(t, adapter.SendKey(ctx, "admin@example.test", gov.KeyNotice{}), context.Canceled)
}
