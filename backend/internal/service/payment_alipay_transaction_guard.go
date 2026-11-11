package service

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// lockAlipayTransaction serializes bill claims and ordinary Alipay confirmation
// using the underlying official transaction ID. The lock is released on commit
// or rollback, including when two configured instances share a receiving account.
func (s *PaymentService) lockAlipayTransaction(ctx context.Context, tx *dbent.Tx, tradeNo string) error {
	if s.entClient.Driver().Dialect() != dialect.Postgres {
		return nil
	}
	_, err := tx.Client().ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "alipay_trade:"+strings.TrimSpace(tradeNo))
	if err != nil {
		return fmt.Errorf("lock Alipay transaction: %w", err)
	}
	return nil
}

// guardAlipayPaidTransition reserves transaction ownership before the usual
// PAID compare-and-set. Installations without a fixed-QR provider keep their
// existing payment path and do not require the bill tables for confirmation.
func (s *PaymentService) guardAlipayPaidTransition(ctx context.Context, order *dbent.PaymentOrder, tradeNo string, transition func(*dbent.Client) (int, error)) (int, error) {
	if payment.GetBasePaymentType(order.PaymentType) != payment.TypeAlipay || strings.TrimSpace(tradeNo) == "" {
		return transition(s.entClient)
	}
	configured, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKey(payment.TypeAlipayBill)).Exist(ctx)
	if err != nil {
		return 0, fmt.Errorf("check Alipay bill coexistence: %w", err)
	}
	if !configured {
		return transition(s.entClient)
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin Alipay payment confirmation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.lockAlipayTransaction(ctx, tx, tradeNo); err != nil {
		return 0, err
	}
	used, err := tx.PaymentOrder.Query().Where(paymentorder.IDNEQ(order.ID), paymentorder.PaymentTradeNo(tradeNo)).Exist(ctx)
	if err != nil {
		return 0, err
	}
	if !used {
		rows, err := tx.Client().QueryContext(ctx, `SELECT EXISTS (SELECT 1 FROM payment_bill_claims WHERE alipay_order_no=$1 AND (order_id IS NULL OR order_id<>$2))`, tradeNo, order.ID)
		if err != nil {
			return 0, err
		}
		if rows.Next() {
			err = rows.Scan(&used)
		}
		rowsErr := rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if rowsErr != nil {
			return 0, rowsErr
		}
	}
	if used {
		return 0, infraerrors.Conflict("ALIPAY_BILL_ALREADY_CLAIMED", "this Alipay transaction already belongs to another payment order")
	}
	count, err := transition(tx.Client())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit Alipay payment confirmation: %w", err)
	}
	return count, nil
}
