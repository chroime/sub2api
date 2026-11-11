//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func reserveBillMatchingFixture(t *testing.T, s *PaymentService, providerID, localID, externalID int64, start time.Time) {
	t.Helper()
	var local, external any
	if localID > 0 {
		local = localID
	}
	if externalID > 0 {
		external = externalID
	}
	_, err := s.entClient.ExecContext(context.Background(), `INSERT INTO payment_bill_amount_reservations(provider_instance_id,receiver_id,amount_minor,order_id,epay_order_id,created_at,expires_at,reuse_after) VALUES($1,$2,10000,$3,$4,$5,$6,$7)`, providerID, "2088000000000001", local, external, start, start.Add(time.Minute), start.Add(3*time.Minute))
	require.NoError(t, err)
}

func TestBillAmountMatchingRecoversPersistedReceiptAfterLaterWindowFailure(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-23*time.Hour - 50*time.Minute)
	o := matchingBillLocalOrder(t, s, id, "INCOMPLETE_SYNC", start)
	credit := alipayGuardFulfillment(t, s, o)
	calls := 0
	failed := billQueryFixture(func(_ context.Context, from, to time.Time, page int) (*provider.AlipayBillPage, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("fixture later page unavailable")
		}
		return &provider.AlipayBillPage{Page: page, Total: 1, Items: []provider.AlipayBillEntry{{AccountLogID: "PERSISTED_LOG", AlipayOrderNo: "PERSISTED_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(10 * time.Second)}}}, nil
	})
	_, err := s.syncAlipayBillsWithReader(ctx, id, "2088000000000001", failed)
	require.Error(t, err)
	require.Zero(t, *credit, "incomplete statement must not choose a receipt")
	empty := billQueryFixture(func(_ context.Context, from, to time.Time, page int) (*provider.AlipayBillPage, error) {
		return &provider.AlipayBillPage{Page: page}, nil
	})
	result, err := s.syncAlipayBillsWithReader(ctx, id, "2088000000000001", empty)
	require.NoError(t, err)
	require.Equal(t, 1, result.Matched)
	require.Equal(t, 100.0, *credit)
}

func matchingBillLocalOrder(t *testing.T, s *PaymentService, providerID int64, name string, start time.Time) *dbent.PaymentOrder {
	t.Helper()
	o := billTestOrder(t, s, providerID, name)
	_, err := s.entClient.ExecContext(context.Background(), `UPDATE payment_orders SET created_at=$1 WHERE id=$2`, start, o.ID)
	require.NoError(t, err)
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetExpiresAt(start.Add(time.Minute)).SetProviderSnapshot(map[string]any{"provider_key": "alipay_bill", "receiver_id": "2088000000000001", "currency": "CNY", "bill_amount_matching": true}).Save(context.Background())
	require.NoError(t, err)
	reserveBillMatchingFixture(t, s, providerID, o.ID, 0, start)
	return o
}

func TestBillAmountMatchingCreditsReservedLocalOrderOnce(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	o := matchingBillLocalOrder(t, s, id, "RESERVED_LOCAL", start)
	credit := alipayGuardFulfillment(t, s, o)
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "WINDOW_LOG", AlipayOrderNo: "WINDOW_TRADE", MerchantOrderNo: "ALIPAY_GENERATED_REFERENCE", Amount: "100.00", Direction: "收入", TransAt: start.Add(18 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched, "a unique reserved amount should match even though fixed QR has another merchant reference")
	o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, o.Status)
	require.Equal(t, 100.0, *credit)
	matched, err = s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, 100.0, *credit, "retry must not duplicate account credit")
}

func TestBillAmountMatchingRejectsLateCancelledAndMultipleReceipts(t *testing.T) {
	for _, kind := range []string{"late", "cancelled", "multiple", "overlapping"} {
		t.Run(kind, func(t *testing.T) {
			s, id := newBillReceiptTestService(t)
			ctx := context.Background()
			start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
			o := matchingBillLocalOrder(t, s, id, "REJECT_"+kind, start)
			when := start.Add(18 * time.Second)
			if kind == "late" {
				when = start.Add(time.Minute)
			}
			if kind == "cancelled" {
				_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusCancelled).Save(ctx)
				require.NoError(t, err)
			}
			if kind == "overlapping" {
				matchingBillLocalOrder(t, s, id, "OVERLAP_OTHER", start)
			}
			r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "REJECT_LOG", AlipayOrderNo: "REJECT_TRADE", Amount: "100.00", Direction: "收入", TransAt: when})
			require.NoError(t, err)
			if kind == "multiple" {
				_, _, err = s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "SECOND_LOG", AlipayOrderNo: "SECOND_TRADE", Amount: "100.00", Direction: "收入", TransAt: when.Add(time.Second)})
				require.NoError(t, err)
			}
			matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
			require.NoError(t, err)
			require.False(t, matched)
			var count int
			rows, err := s.entClient.QueryContext(ctx, `SELECT count(*) FROM payment_bill_claims`)
			require.NoError(t, err)
			require.True(t, rows.Next())
			require.NoError(t, rows.Scan(&count))
			rows.Close()
			require.Zero(t, count)
		})
	}
}

func TestBillAmountMatchingExternalOrderQueuesNotificationOnly(t *testing.T) {
	s, id, _ := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
	ctx := context.Background()
	created, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
	require.NoError(t, err)
	o, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, "", created.TradeNo)
	require.NoError(t, err)
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET created_at=$1,expires_at=$2 WHERE id=$3`, start, start.Add(time.Minute), o.ID)
	require.NoError(t, err)
	_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET created_at=$1,expires_at=$2,reuse_after=$3 WHERE epay_order_id=$4`, start, start.Add(time.Minute), start.Add(3*time.Minute), o.ID)
	require.NoError(t, err)
	o, err = findBuiltinEasyPayOrder(ctx, s.entClient, id, "", created.TradeNo)
	require.NoError(t, err)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "EXTERNAL_LOG", AlipayOrderNo: "EXTERNAL_TRADE", MerchantOrderNo: "STATIC_REFERENCE", Amount: "100.00", Direction: "收入", TransAt: o.CreatedAt.Truncate(time.Second).Add(5 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched)
	o, err = findBuiltinEasyPayOrder(ctx, s.entClient, id, "", created.TradeNo)
	require.NoError(t, err)
	require.Equal(t, "PAID", o.Status)
	require.Equal(t, "pending", o.NotifyStatus)
	count, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestBillAmountMatchingConfirmsBeforeOriginalDeadline(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	o := matchingBillLocalOrder(t, s, id, "WAIT_COMPLETE_WINDOW", start)
	credit := alipayGuardFulfillment(t, s, o)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "EARLY_LOG", AlipayOrderNo: "EARLY_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(2 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, matched, "an observed unique payment must not wait for the remaining payment minute")
	require.Equal(t, 100.0, *credit)
	updated, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, updated.Status)
}

func TestBillAmountMatchingRequiresStatementThroughPaymentSecond(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	o := matchingBillLocalOrder(t, s, id, "STATEMENT_COVERAGE", start)
	credit := alipayGuardFulfillment(t, s, o)
	r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "COVERAGE_LOG", AlipayOrderNo: "COVERAGE_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(18 * time.Second)})
	require.NoError(t, err)
	matched, err := s.reconcileBillReceiptThrough(ctx, r, r.TransAt)
	require.NoError(t, err)
	require.False(t, matched)
	require.Zero(t, *credit, "coverage must include the whole official payment second")
	matched, err = s.reconcileBillReceiptThrough(ctx, r, r.TransAt.Add(time.Second))
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, 100.0, *credit)
}

func TestBillAmountMatchingRejectsFractionalExpirationSecond(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pk := "alipay_bill"
	o := &dbent.PaymentOrder{ProviderKey: &pk, PayAmount: 100, Status: OrderStatusPending, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(900 * time.Millisecond), ProviderSnapshot: map[string]any{"provider_key": pk, "receiver_id": "2088000000000001", "bill_amount_matching": true}}
	r := &PaymentBillReceipt{ReceiverID: "2088000000000001", AlipayOrderNo: "FRACTION_TRADE", Amount: "100.00", TransAt: now}
	require.Equal(t, "amount_window_expired", billLocalReceiptReasonForMode(r, o, billClaimReservedAmount))
}

func TestBillAmountDisabledProviderStillCollectsLocalOrder(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	matchingBillLocalOrder(t, s, id, "DISABLED_LOCAL", start)
	inst, err := s.entClient.PaymentProviderInstance.UpdateOneID(id).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	eligible, err := s.shouldCollectAlipayBillProvider(ctx, inst, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, eligible)
}

func TestBillAmountMatchingSyncStoresWholeBatchBeforeChoosingOrder(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	o := matchingBillLocalOrder(t, s, id, "BATCH_AMBIGUOUS", start)
	reader := billQueryFixture(func(_ context.Context, from, to time.Time, page int) (*provider.AlipayBillPage, error) {
		p := &provider.AlipayBillPage{Page: page}
		for i, trade := range []string{"BATCH_A", "BATCH_B"} {
			when := start.Add(time.Duration(15+i) * time.Second)
			if !when.Before(from) && when.Before(to) {
				p.Items = append(p.Items, provider.AlipayBillEntry{AccountLogID: trade, AlipayOrderNo: trade, Amount: "100.00", Direction: "收入", TransAt: when})
			}
		}
		p.Total = len(p.Items)
		return p, nil
	})
	result, err := s.syncAlipayBillsWithReader(ctx, id, "2088000000000001", reader)
	require.NoError(t, err)
	require.Zero(t, result.Matched)
	require.Equal(t, 2, result.Review)
	o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, o.Status)
}
