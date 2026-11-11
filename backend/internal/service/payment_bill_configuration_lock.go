package service

import (
	"context"
	"strconv"
	"strings"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// The same row lock is taken by bill-provider edits/deletion. Configuration
// cannot change between validating receiver identity and creating its history.
func (s *PaymentService) lockBillProviderConfig(ctx context.Context, tx *dbent.Tx, id int64, receiver string) (*dbent.PaymentProviderInstance, map[string]string, error) {
	query := tx.PaymentProviderInstance.Query().Where(paymentproviderinstance.ID(id))
	if s.entClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	inst, err := query.Only(ctx)
	if err != nil {
		return nil, nil, err
	}
	if inst.ProviderKey != payment.TypeAlipayBill {
		return nil, nil, infraerrors.Conflict("ALIPAY_BILL_PROVIDER_CHANGED", "payment provider changed")
	}
	cfg, err := s.configService.decryptProviderConfig(inst.ProviderKey, inst.Config)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(cfg["receiverId"]) != receiver {
		return nil, nil, infraerrors.Conflict("ALIPAY_BILL_PROVIDER_CHANGED", "receiving account changed; retry using its current configuration")
	}
	return inst, cfg, nil
}

func (s *PaymentService) validateBillOrderSelection(ctx context.Context, tx *dbent.Tx, sel *payment.InstanceSelection, amount float64) error {
	if sel == nil || sel.ProviderKey != payment.TypeAlipayBill {
		return nil
	}
	id, err := strconv.ParseInt(sel.InstanceID, 10, 64)
	if err != nil {
		return err
	}
	inst, cfg, err := s.lockBillProviderConfig(ctx, tx, id, strings.TrimSpace(sel.Config["receiverId"]))
	if err != nil {
		return err
	}
	if !inst.Enabled || cfg["appId"] != sel.Config["appId"] || cfg["qrRules"] != sel.Config["qrRules"] {
		return infraerrors.Conflict("ALIPAY_BILL_PROVIDER_CHANGED", "payment configuration changed; please retry")
	}
	// Check eligibility before inserting any order. No unavailable QR amount
	// should consume the user's pending-order slots.
	fixed, err := provider.NewAlipayBill(sel.InstanceID, cfg)
	if err != nil {
		return err
	}
	_, err = fixed.CreatePayment(ctx, payment.CreatePaymentRequest{Amount: strconv.FormatFloat(amount, 'f', 2, 64), PaymentType: payment.TypeAlipay})
	return err
}
