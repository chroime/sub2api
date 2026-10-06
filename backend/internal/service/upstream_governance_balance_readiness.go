package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

type governanceSMTPConfigReader interface {
	GetSMTPConfig(context.Context) (*SMTPConfig, error)
}

func (n *governanceBalanceNotifier) Readiness(ctx context.Context, overrides []string) gov.BalanceDeliveryReadiness {
	result := gov.BalanceDeliveryReadiness{Reason: "recipients_unavailable"}
	recipients, err := n.Recipients(ctx, overrides)
	if err != nil || len(recipients) == 0 || len(recipients) > 10 {
		return result
	}
	seen := make(map[string]bool)
	for _, recipient := range recipients {
		parsed, err := mail.ParseAddress(recipient)
		if err != nil || parsed.Address != recipient || len(recipient) > 320 || strings.ContainsAny(recipient, "\r\n") {
			return result
		}
		seen[strings.ToLower(recipient)] = true
	}
	result.RecipientCount = len(seen)
	reader, ok := n.mail.(governanceSMTPConfigReader)
	if !ok {
		result.Reason = "email_unavailable"
		return result
	}
	config, err := reader.GetSMTPConfig(ctx)
	if errors.Is(err, ErrEmailNotConfigured) {
		result.Reason = "smtp_not_configured"
		return result
	}
	if err != nil || config == nil {
		result.Reason = "email_unavailable"
		return result
	}
	if strings.TrimSpace(config.Host) == "" {
		result.Reason = "smtp_not_configured"
		return result
	}
	if config.Port < 1 || config.Port > 65535 {
		result.Reason = "smtp_invalid"
		return result
	}
	// Reuse the transport's address/header checks without opening a connection.
	if _, err = buildSMTPMessage(config, recipients[0], "Configuration check", ""); err != nil {
		result.Reason = "smtp_invalid"
		return result
	}
	result.Ready, result.Reason = true, "ready"
	return result
}
