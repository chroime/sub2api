package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type readinessMail struct {
	config *SMTPConfig
	err    error
	sends  int
}

func (m *readinessMail) GetSMTPConfig(context.Context) (*SMTPConfig, error) { return m.config, m.err }
func (m *readinessMail) SendEmail(context.Context, string, string, string) error {
	m.sends++
	return nil
}

func TestGovernanceBalanceReadinessChecksCurrentConfigurationWithoutSending(t *testing.T) {
	mail := &readinessMail{err: ErrEmailNotConfigured}
	adapter := &governanceBalanceNotifier{mail: mail}
	result := adapter.Readiness(t.Context(), []string{"admin@example.com"})
	require.False(t, result.Ready)
	require.Equal(t, "smtp_not_configured", result.Reason)
	require.Equal(t, 1, result.RecipientCount)
	mail.err = nil
	mail.config = &SMTPConfig{Host: "smtp.example.com", Port: 587, From: "sender@example.com", FromName: "Fixture"}
	result = adapter.Readiness(t.Context(), []string{"admin@example.com"})
	require.True(t, result.Ready)
	require.Equal(t, "ready", result.Reason)
	mail.config.From = "invalid address"
	result = adapter.Readiness(t.Context(), []string{"admin@example.com"})
	require.False(t, result.Ready)
	require.Equal(t, "smtp_invalid", result.Reason)
	result = adapter.Readiness(t.Context(), []string{"not-an-email"})
	require.False(t, result.Ready)
	require.Equal(t, "recipients_unavailable", result.Reason)
	require.Zero(t, mail.sends)
}

func TestGovernanceBalanceReadinessUsesRealSMTPSettingsAndRecipientRouting(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	adapter := &governanceBalanceNotifier{settings: repo, mail: NewEmailService(repo, nil), admins: governanceBalanceAdminFixture{&User{Email: "owner@example.com", Role: RoleAdmin, Status: StatusActive}}}
	result := adapter.Readiness(t.Context(), nil)
	require.Equal(t, "smtp_not_configured", result.Reason)
	require.Equal(t, 1, result.RecipientCount)
	require.NoError(t, repo.SetMultiple(t.Context(), map[string]string{SettingKeySMTPHost: "smtp.invalid.example", SettingKeySMTPPort: "587", SettingKeySMTPFrom: "sender@example.com"}))
	require.NoError(t, repo.Set(t.Context(), SettingKeyAccountQuotaNotifyEmails, `[{"email":"one@example.com","verified":true},{"email":"two@example.com","verified":true},{"email":"ignored@example.com","verified":false}]`))
	result = adapter.Readiness(t.Context(), nil)
	require.True(t, result.Ready, "configuration readiness performs no DNS or SMTP request")
	require.Equal(t, 2, result.RecipientCount)
	result = adapter.Readiness(t.Context(), []string{"site@example.com"})
	require.True(t, result.Ready)
	require.Equal(t, 1, result.RecipientCount)
}
