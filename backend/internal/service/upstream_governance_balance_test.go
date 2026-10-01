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

type governanceBalanceAdminFixture struct{ user *User }

func (r governanceBalanceAdminFixture) GetFirstAdmin(context.Context) (*User, error) {
	return r.user, nil
}

func TestGovernanceBalanceRecipientsUseVerifiedSettingsThenActiveAdmin(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	adapter := &governanceBalanceNotifier{settings: repo, admins: governanceBalanceAdminFixture{&User{Email: "owner@example.com", Role: RoleAdmin, Status: StatusActive}}}
	ctx := t.Context()
	require.NoError(t, repo.Set(ctx, SettingKeyAccountQuotaNotifyEmails, `[{"email":"verified@example.com","verified":true},{"email":"ignored@example.com","verified":false},{"email":"disabled@example.com","verified":true,"disabled":true}]`))
	recipients, err := adapter.Recipients(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"verified@example.com"}, recipients)
	require.NoError(t, repo.Set(ctx, SettingKeyAccountQuotaNotifyEmails, `[]`))
	recipients, err = adapter.Recipients(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"owner@example.com"}, recipients)
	recipients, err = adapter.Recipients(ctx, []string{"site@example.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"site@example.com"}, recipients)
	adapter.admins = governanceBalanceAdminFixture{&User{Email: "inactive@example.com", Role: RoleAdmin, Status: StatusDisabled}}
	_, err = adapter.Recipients(ctx, nil)
	require.Error(t, err)
}

func TestGovernanceBalanceMailUsesCurrentSystemSMTPAndEscapesRemoteNames(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	server := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
	adapter := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}
	notice := gov.BalanceNotice{SiteID: 7, SiteName: "<script>upstream</script>\r\nInjected", BaseURL: "https://upstream.example", Platform: "newapi", Balance: 500, Threshold: 1000, Unit: "quota", ObservedAt: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, adapter.Send(t.Context(), "admin@example.com", notice))
	require.Equal(t, int64(1), server.messageCount())
	body := server.lastMessageBody(t)
	require.Contains(t, body, "&lt;script&gt;upstream&lt;/script&gt;")
	require.NotContains(t, body, "<script>")
	require.Contains(t, body, "500 quota")
	require.Contains(t, body, "2026-09-26 08:00:00")
	require.Contains(t, body, "北京时间，+08:00")
	require.NotContains(t, body, "$500")
	require.NoError(t, repo.Set(t.Context(), SettingKeySMTPFrom, "updated-sender@example.com"))
	require.NoError(t, adapter.Send(t.Context(), "admin@example.com", notice))
	message, err := mail.ReadMessage(strings.NewReader(server.lastMessage()))
	require.NoError(t, err)
	require.Contains(t, message.Header.Get("From"), "updated-sender@example.com")
	require.Equal(t, int64(2), server.messageCount())
}
