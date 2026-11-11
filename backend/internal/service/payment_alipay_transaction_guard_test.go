//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAlipayTransactionGuardRejectsDelayedWebhookAfterBillClaim(t *testing.T) {
	s, instanceID := newBillReceiptTestService(t)
	ctx := context.Background()
	old := alipayGuardLegacyOrder(t, s, instanceID, "DELAYED_OFFICIAL")
	fixed := billTestOrder(t, s, instanceID, "CLAIMED_FIXED")
	credited := alipayGuardFulfillment(t, s, old)
	receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{
		AccountLogID: "DELAYED_LOG", AlipayOrderNo: "DELAYED_TRADE", Amount: "100.00", Direction: "收入", TransAt: time.Now(),
	})
	require.NoError(t, err)
	require.NoError(t, s.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, fixed.ID, "admin:1", "verified transfer", true))
	_, err = s.entClient.PaymentProviderInstance.UpdateOneID(instanceID).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	err = s.toPaid(ctx, old, "DELAYED_TRADE", 100, payment.TypeAlipay)
	require.Equal(t, "ALIPAY_BILL_ALREADY_CLAIMED", infraerrors.Reason(err))
	require.Zero(t, *credited, "a delayed legacy webhook must not credit a transaction already claimed for a fixed QR order")
	unchanged, err := s.entClient.PaymentOrder.Get(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, unchanged.Status)
	require.Empty(t, unchanged.PaymentTradeNo)
}

func TestAlipayTransactionGuardRejectsBillClaimAfterOrdinaryPayment(t *testing.T) {
	s, instanceID := newBillReceiptTestService(t)
	ctx := context.Background()
	old := alipayGuardLegacyOrder(t, s, instanceID, "FIRST_OFFICIAL")
	fixed := billTestOrder(t, s, instanceID, "LATER_FIXED")
	credited := alipayGuardFulfillment(t, s, old)
	require.NoError(t, s.toPaid(ctx, old, "FIRST_TRADE", 100, payment.TypeAlipay))
	require.Equal(t, 100.0, *credited)
	receipt, _, err := s.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{
		AccountLogID: "LATER_LOG", AlipayOrderNo: "FIRST_TRADE", Amount: "100.00", Direction: "收入", TransAt: time.Now(),
	})
	require.NoError(t, err)
	err = s.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, fixed.ID, "admin:1", "review", true)
	require.Equal(t, "ALIPAY_BILL_ALREADY_CLAIMED", infraerrors.Reason(err))
	require.NoError(t, s.toPaid(ctx, old, "FIRST_TRADE", 100, payment.TypeAlipay), "the same ordinary order may safely retry its notification")
	require.Equal(t, 100.0, *credited)
	unchanged, err := s.entClient.PaymentOrder.Get(ctx, fixed.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, unchanged.Status)
}

func TestAlipayTransactionGuardPreservesLegacyRecoveryStatuses(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		age          time.Duration
		wantCredit   bool
	}{
		{name: "pending", status: OrderStatusPending, wantCredit: true},
		{name: "cancelled", status: OrderStatusCancelled, wantCredit: true},
		{name: "recent expired", status: OrderStatusExpired, age: time.Minute, wantCredit: true},
		{name: "old expired", status: OrderStatusExpired, age: (paymentGraceMinutes + 1) * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, instanceID := newBillReceiptTestService(t)
			ctx := context.Background()
			order := alipayGuardLegacyOrder(t, s, instanceID, "RECOVER_LEGACY")
			order, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(tc.status).SetUpdatedAt(time.Now().Add(-tc.age)).Save(ctx)
			require.NoError(t, err)
			credited := alipayGuardFulfillment(t, s, order)
			require.NoError(t, s.toPaid(ctx, order, "RECOVER_TRADE", 100, payment.TypeAlipay))
			if tc.wantCredit {
				require.Equal(t, 100.0, *credited)
			} else {
				require.Zero(t, *credited)
			}
		})
	}
}

func TestAlipayTransactionGuardDoesNotRequireBillTablesBeforeFeatureUse(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	instance, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeAlipay).SetName("Official").SetConfig("{}").Save(ctx)
	require.NoError(t, err)
	s := &PaymentService{entClient: client}
	order := alipayGuardLegacyOrder(t, s, instance.ID, "LEGACY_WITHOUT_BILL_TABLES")
	credited := alipayGuardFulfillment(t, s, order)
	require.NoError(t, s.toPaid(ctx, order, "LEGACY_TRADE", 100, payment.TypeAlipay))
	require.Equal(t, 100.0, *credited)
}

func alipayGuardLegacyOrder(t *testing.T, s *PaymentService, instanceID int64, name string) *dbent.PaymentOrder {
	t.Helper()
	order := billTestOrder(t, s, instanceID, name)
	order, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).SetProviderKey(payment.TypeAlipay).
		SetProviderSnapshot(map[string]any{"provider_key": payment.TypeAlipay, "currency": "CNY"}).Save(context.Background())
	require.NoError(t, err)
	return order
}

func alipayGuardFulfillment(t *testing.T, s *PaymentService, order *dbent.PaymentOrder) *float64 {
	t.Helper()
	credited := new(float64)
	redeemRepo := &paymentFulfillmentRedeemRepo{}
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID, Balance: 0}}
	userRepo.updateBalanceFn = func(_ context.Context, id int64, amount float64) error {
		require.Equal(t, order.UserID, id)
		*credited += amount
		return nil
	}
	s.redeemService = NewRedeemService(redeemRepo, userRepo, nil, &paymentFulfillmentRedeemCacheStub{}, nil, s.entClient, nil, nil)
	s.userRepo = userRepo
	return credited
}
