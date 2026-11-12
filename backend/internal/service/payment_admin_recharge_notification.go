package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	entuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// This is called only by the completion winner. Email delivery is independent
// of balance fulfillment; no error here changes a completed payment order.
func (s *PaymentService) sendAdminRechargeSuccessNotifications(ctx context.Context, order *dbent.PaymentOrder) error {
	if s == nil || s.configService == nil || s.notificationEmailService == nil || order == nil ||
		order.Status != OrderStatusCompleted || order.CompletedAt == nil ||
		(order.OrderType != "" && order.OrderType != payment.OrderTypeBalance) {
		return nil
	}
	configCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	enabled, recipients, err := s.configService.GetAdminRechargeNotificationConfig(configCtx)
	cancel()
	if err != nil || !enabled {
		return err
	}
	var notes string
	if s.entClient != nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		profile, err := s.entClient.User.Query().Where(entuser.IDEQ(order.UserID), entuser.DeletedAtIsNil()).Select(entuser.FieldNotes).Only(lookupCtx)
		cancel()
		if err == nil {
			notes = profile.Notes
		}
	}
	const event = NotificationEmailEventPaymentRechargeSuccessAdmin
	orderID := strconv.FormatInt(order.ID, 10)
	currency := PaymentOrderCurrency(order)
	variables := map[string]string{
		"user_id":          strconv.FormatInt(order.UserID, 10),
		"user_email":       order.UserEmail,
		"user_name":        firstNonEmpty(order.UserName, order.UserEmail),
		"user_notes":       notes,
		"order_id":         orderID,
		"out_trade_no":     firstNonEmpty(order.OutTradeNo, orderID),
		"pay_amount":       payment.FormatAmountForCurrency(order.PayAmount, currency),
		"payment_currency": currency,
		"recharge_amount":  payment.FormatAmountForCurrency(order.Amount, "USD"),
		"payment_method":   order.PaymentType,
		"completed_at":     order.CompletedAt.In(time.FixedZone("UTC+08:00", 8*60*60)).Format("2006-01-02 15:04:05 -07:00"),
	}
	var sendErrors []error
	for _, recipient := range recipients {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(sendErrors, err)...)
		}
		delivery := s.notificationEmailService.BalanceNotificationRecords().Begin(balancenotice.Input{
			EventType: event, TriggerType: "automatic", UserID: order.UserID,
			UserEmail: order.UserEmail, UserName: order.UserName, Recipient: recipient,
			DeliveryKey: notificationEmailDeliveryKey(event, "payment_order", orderID, recipient, ""),
		})
		mailCtx, cancel := context.WithTimeout(ctx, emailSendTimeout)
		err := s.notificationEmailService.Send(mailCtx, NotificationEmailSendInput{
			BalanceDelivery: delivery, Event: event, Locale: notificationEmailLocaleChinese,
			RecipientEmail: recipient, RecipientName: "管理员", SourceType: "payment_order", SourceID: orderID,
			Variables: variables,
		})
		cancel()
		if err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("recipient %s: %w", notificationEmailHash(recipient), err))
		}
	}
	return errors.Join(sendErrors...)
}
