package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingPaymentAdminRechargeNotifyEnabled = "PAYMENT_ADMIN_RECHARGE_NOTIFY_ENABLED"
	SettingPaymentAdminRechargeNotifyEmails  = "PAYMENT_ADMIN_RECHARGE_NOTIFY_EMAILS"
	maxPaymentAdminRechargeNotifyEmails      = 10
)

// Payment receipts use their own configured recipients; quota-notification
// recipients and user registration addresses are not fallback destinations.
func (s *PaymentConfigService) GetAdminRechargeNotificationConfig(ctx context.Context) (bool, []string, error) {
	if s == nil || s.settingRepo == nil {
		return false, []string{}, errors.New("payment notification settings are unavailable")
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingPaymentAdminRechargeNotifyEnabled,
		SettingPaymentAdminRechargeNotifyEmails,
	})
	if err != nil {
		return false, []string{}, fmt.Errorf("get payment recharge notification settings: %w", err)
	}
	return parsePaymentAdminRechargeNotificationConfig(values)
}

func normalizePaymentAdminRechargeNotifyEmails(raw []string) ([]string, error) {
	if len(raw) > maxPaymentAdminRechargeNotifyEmails {
		return nil, fmt.Errorf("at most %d payment notification email addresses are allowed", maxPaymentAdminRechargeNotifyEmails)
	}
	emails := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, value := range raw {
		// Reject line breaks before trimming: even a trailing newline is not a
		// valid recipient field, and must never reach SMTP headers.
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("payment notification recipients must be plain valid email addresses")
		}
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || len(value) > 254 {
			return nil, errors.New("payment notification recipients must be plain valid email addresses")
		}
		address, err := mail.ParseAddress(value)
		if err != nil || address.Name != "" || address.Address != value {
			return nil, errors.New("payment notification recipients must be plain valid email addresses")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		emails = append(emails, value)
	}
	return emails, nil
}

func parsePaymentAdminRechargeNotifyEmails(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var emails []string
	if err := json.Unmarshal([]byte(raw), &emails); err != nil {
		return nil, errors.New("payment notification recipients must be a JSON array of email addresses")
	}
	return normalizePaymentAdminRechargeNotifyEmails(emails)
}

func parsePaymentAdminRechargeNotificationConfig(values map[string]string) (bool, []string, error) {
	enabled := values[SettingPaymentAdminRechargeNotifyEnabled] == "true"
	emails, err := parsePaymentAdminRechargeNotifyEmails(values[SettingPaymentAdminRechargeNotifyEmails])
	if err != nil {
		return false, []string{}, err
	}
	if enabled && len(emails) == 0 {
		return false, []string{}, errors.New("enabled payment recharge notifications require at least one recipient")
	}
	return enabled, emails, nil
}

// Validate the effective enabled state before preparing any writes. Only
// explicitly supplied fields enter the resulting patch; saved drafts survive
// unrelated payment updates and a switch-only disable operation.
func (s *PaymentConfigService) resolveAdminRechargeNotificationUpdate(ctx context.Context, req UpdatePaymentConfigRequest) (map[string]string, error) {
	if req.AdminRechargeNotifyEnabled == nil && req.AdminRechargeNotifyEmails == nil {
		return nil, nil
	}
	invalid := func(err error) (map[string]string, error) {
		return nil, infraerrors.BadRequest("INVALID_ADMIN_RECHARGE_NOTIFY_CONFIG", err.Error())
	}
	var enabled bool
	var emails []string
	var err error
	if req.AdminRechargeNotifyEnabled != nil {
		enabled = *req.AdminRechargeNotifyEnabled
	}
	if req.AdminRechargeNotifyEmails != nil {
		emails, err = normalizePaymentAdminRechargeNotifyEmails(*req.AdminRechargeNotifyEmails)
		if err != nil {
			return invalid(err)
		}
	}
	// Explicitly disabling does not need to read or repair a saved draft.
	// Enabling with omitted recipients and recipient-only patches must check
	// the other stored field so a narrow update cannot enable an empty list.
	if req.AdminRechargeNotifyEnabled == nil || (enabled && req.AdminRechargeNotifyEmails == nil) {
		if s == nil || s.settingRepo == nil {
			return nil, errors.New("payment notification settings are unavailable")
		}
		stored, readErr := s.settingRepo.GetMultiple(ctx, []string{
			SettingPaymentAdminRechargeNotifyEnabled,
			SettingPaymentAdminRechargeNotifyEmails,
		})
		if readErr != nil {
			return nil, fmt.Errorf("get saved payment recharge notification settings: %w", readErr)
		}
		if req.AdminRechargeNotifyEnabled == nil {
			enabled = stored[SettingPaymentAdminRechargeNotifyEnabled] == "true"
		}
		if enabled && req.AdminRechargeNotifyEmails == nil {
			emails, err = parsePaymentAdminRechargeNotifyEmails(stored[SettingPaymentAdminRechargeNotifyEmails])
			if err != nil {
				return invalid(err)
			}
		}
	}
	if enabled && len(emails) == 0 {
		return invalid(errors.New("enabled payment recharge notifications require at least one recipient"))
	}
	updates := make(map[string]string, 2)
	if req.AdminRechargeNotifyEnabled != nil {
		updates[SettingPaymentAdminRechargeNotifyEnabled] = strconv.FormatBool(*req.AdminRechargeNotifyEnabled)
	}
	if req.AdminRechargeNotifyEmails != nil {
		encoded, err := json.Marshal(emails)
		if err != nil {
			return nil, fmt.Errorf("encode payment recharge notification recipients: %w", err)
		}
		updates[SettingPaymentAdminRechargeNotifyEmails] = string(encoded)
	}
	return updates, nil
}
