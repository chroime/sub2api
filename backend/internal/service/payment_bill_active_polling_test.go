//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func TestBillActivePollingEligibility(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, source, status string
		expires              time.Duration
		covered              bool
		want                 bool
	}{
		{name: "idle provider", want: false},
		{name: "local awaiting payment", source: "local", status: "PENDING", expires: 30 * time.Second, want: true},
		{name: "local recently expired", source: "local", status: "EXPIRED", expires: -time.Minute, want: true},
		{name: "local old expiry uses regular recovery", source: "local", status: "EXPIRED", expires: -3 * time.Minute},
		{name: "cancelled original window open", source: "local", status: "CANCELLED", expires: 30 * time.Second, want: true},
		{name: "cancelled waits for bill coverage", source: "local", status: "CANCELLED", expires: -time.Second, want: true},
		{name: "cancelled window already covered", source: "local", status: "CANCELLED", expires: -time.Second, covered: true},
		{name: "local fulfillment retry", source: "local", status: "FAILED", expires: -time.Minute, want: true},
		{name: "local paid", source: "local", status: "PAID", expires: 30 * time.Second, want: true},
		{name: "local recharging", source: "local", status: "RECHARGING", expires: 30 * time.Second, want: true},
		{name: "completed awaits closure coverage", source: "local", status: "COMPLETED", expires: 30 * time.Second, want: true},
		{name: "completed closure covered", source: "local", status: "COMPLETED", expires: 30 * time.Second, covered: true},
		{name: "external pending", source: "external", status: "PENDING", expires: 30 * time.Second, want: true},
		{name: "external recently expired", source: "external", status: "EXPIRED", expires: -time.Minute, want: true},
		{name: "external paid awaits coverage", source: "external", status: "PAID", expires: 30 * time.Second, want: true},
		{name: "external paid closure covered", source: "external", status: "PAID", expires: 30 * time.Second, covered: true},
		{name: "external old expiry uses regular recovery", source: "external", status: "EXPIRED", expires: -3 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id := newBillReceiptTestService(t)
			ctx := context.Background()
			if tc.source == "local" {
				o := matchingBillLocalOrder(t, s, id, "ACTIVE_LOCAL", now.Add(tc.expires-time.Minute))
				_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(tc.status).Save(ctx)
				require.NoError(t, err)
				if tc.status == "COMPLETED" {
					_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET closed_at=$1 WHERE order_id=$2`, now.Add(-time.Second), o.ID)
					require.NoError(t, err)
				}
			} else if tc.source == "external" {
				o := epayReceiptTestOrder(t, s, id, "ACTIVE_EXTERNAL")
				start := now.Add(tc.expires - time.Minute)
				_, err := s.entClient.ExecContext(ctx, `UPDATE payment_epay_orders SET status=$1,created_at=$2,expires_at=$3 WHERE id=$4`, tc.status, start, now.Add(tc.expires), o)
				require.NoError(t, err)
				reserveBillMatchingFixture(t, s, id, 0, o, start)
				if tc.status == "PAID" {
					_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET closed_at=$1 WHERE epay_order_id=$2`, now.Add(-time.Second), o)
					require.NoError(t, err)
				}
			}
			if tc.covered {
				billAmountSuccessfulSyncFixture(t, s, now)
			}
			inst, err := s.entClient.PaymentProviderInstance.UpdateOneID(id).SetEnabled(false).Save(ctx)
			require.NoError(t, err)
			active, err := s.shouldCollectActiveAlipayBillProvider(ctx, inst, now)
			require.NoError(t, err)
			require.Equal(t, tc.want, active, "new-payment enablement must not suppress an already issued order")
		})
	}
}

func TestBillActivePollingSkipsHistoricalRescan(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	billAmountSuccessfulSyncFixture(t, s, now.Add(-10*time.Second))
	var windows [][2]time.Time
	reader := billQueryFixture(func(_ context.Context, start, end time.Time, page int) (*provider.AlipayBillPage, error) {
		windows = append(windows, [2]time.Time{start, end})
		return &provider.AlipayBillPage{Page: page}, nil
	})
	_, err := s.syncAlipayBillsWithReaderMode(ctx, id, "2088000000000001", reader, true)
	require.NoError(t, err)
	require.Len(t, windows, 1, "active collection should only read the recent forward window")
	require.True(t, windows[0][0].After(now.Add(-6*time.Minute)))
	windows = nil
	_, err = s.syncAlipayBillsWithReader(ctx, id, "2088000000000001", reader)
	require.NoError(t, err)
	require.Len(t, windows, 2, "regular collection must retain its independent historical recovery sweep")
	require.True(t, windows[1][0].Before(now.Add(-23*time.Hour)))
}
