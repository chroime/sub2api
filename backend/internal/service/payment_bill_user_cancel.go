package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// cancelBillOrderByUser serializes an explicit cancellation with receipt claims
// and amount allocation. The release marker and audit commit with CANCELLED;
// expiry and administrative cancellation deliberately do not use this path.
func (s *PaymentService) cancelBillOrderByUser(ctx context.Context, original *dbent.PaymentOrder) (string, error) {
	receiver, _ := original.ProviderSnapshot["receiver_id"].(string)
	amount, err := billObservedAmountCents(strconv.FormatFloat(original.PayAmount, 'f', 2, 64))
	if err != nil {
		return "", infraerrors.BadRequest("ALIPAY_BILL_INVALID_AMOUNT", "invalid payment amount")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = s.lockBillAmountKey(ctx, tx, receiver, amount); err != nil {
		return "", err
	}
	order, err := tx.PaymentOrder.Get(ctx, original.ID)
	if err != nil {
		return "", err
	}
	if order.Status != OrderStatusPending {
		return billUserCancelChangedOutcome(order.Status)
	}
	count, err := tx.PaymentOrder.Update().Where(paymentorder.ID(order.ID), paymentorder.Status(OrderStatusPending)).SetStatus(OrderStatusCancelled).Save(ctx)
	if err != nil {
		return "", fmt.Errorf("cancel fixed QR order: %w", err)
	}
	if count != 1 {
		order, err = tx.PaymentOrder.Get(ctx, order.ID)
		if err != nil {
			return "", err
		}
		return billUserCancelChangedOutcome(order.Status)
	}
	now := time.Now().UTC()
	// Use the precise cancellation instant. Official statements are second
	// precision; a replacement created in this same second overlaps the old
	// historical window and must be reviewed instead of guessing ownership.
	_, err = tx.Client().ExecContext(ctx, `UPDATE payment_bill_amount_reservations
		SET user_cancelled_at=CASE WHEN $1<created_at THEN created_at WHEN $1>expires_at THEN expires_at ELSE $1 END
		WHERE order_id=$2 AND user_cancelled_at IS NULL`, now, order.ID)
	if err != nil {
		return "", err
	}
	_, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("ORDER_CANCELLED").
		SetOperator(fmt.Sprintf("user:%d", order.UserID)).SetDetail(`{"detail":"user cancelled order","amount_released":true}`).SetCreatedAt(now).Save(ctx)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return checkPaidResultCancelled, nil
}

func billUserCancelChangedOutcome(status string) (string, error) {
	switch status {
	case OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted:
		return checkPaidResultAlreadyPaid, nil
	default:
		return "", infraerrors.BadRequest("INVALID_STATUS", "order cannot be cancelled in current status")
	}
}
