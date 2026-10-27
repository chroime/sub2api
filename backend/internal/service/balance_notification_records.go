package service

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
)

// ProvideNotificationEmailService shares a single recorder with automatic mail,
// batch mail and the administrator's read-only notification panel.
func ProvideNotificationEmailService(settings SettingRepository, mail *EmailService, db *sql.DB) *NotificationEmailService {
	s := NewNotificationEmailService(settings, mail)
	s.balanceRecords = balancenotice.NewService(balancenotice.NewSQLStore(db))
	s.balanceRecords.Start()
	return s
}

func (s *NotificationEmailService) SetBalanceNotificationRecords(records *balancenotice.Service) {
	s.balanceRecords = records
}
func (s *NotificationEmailService) BalanceNotificationRecords() *balancenotice.Service {
	if s == nil {
		return nil
	}
	return s.balanceRecords
}
func (s *NotificationEmailService) StopBalanceNotificationRecords() {
	if s != nil {
		s.balanceRecords.Stop()
	}
}

// Resolve and validate the SMTP transport before counting an actual connection
// attempt. The caller's nil tracker preserves other notification behavior.
func sendRecordedBalanceEmail(ctx context.Context, mail *EmailService, d *balancenotice.Delivery, to, subject, body string) error {
	if d == nil {
		return mail.SendEmail(ctx, to, subject, body)
	}
	config, err := mail.GetSMTPConfig(ctx)
	if err != nil {
		d.Fail(err)
		return err
	}
	if _, err = buildSMTPMessage(config, to, subject, body); err != nil {
		d.Fail(err)
		return err
	}
	d.Attempt()
	err = mail.SendEmailWithConfig(config, to, subject, body)
	if err != nil {
		d.Fail(err)
	} else {
		d.Sent()
	}
	return err
}

// Early batch skips still get an effective threshold snapshot. This bounded
// metadata read cannot change the batch's existing skip/error precedence.
func (s *UserBulkEmailService) balanceThresholdForRecord(user *User) *float64 {
	var threshold float64
	if user.BalanceNotifyThreshold != nil {
		threshold = *user.BalanceNotifyThreshold
	} else {
		if s.settings == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		raw, err := s.settings.GetValue(ctx, SettingKeyBalanceLowNotifyThreshold)
		if err != nil {
			return nil
		}
		threshold, err = strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil
		}
	}
	threshold = resolveBalanceThreshold(threshold, user.BalanceNotifyThresholdType, user.TotalRecharged)
	return &threshold
}
