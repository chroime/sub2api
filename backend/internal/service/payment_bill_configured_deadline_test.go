//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func createConfiguredBillLocalFixture(t *testing.T, s *PaymentService, providerID int64, providerConfig map[string]string, minutes int) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	user, err := s.entClient.User.Create().SetEmail("configured-deadline@test.invalid").SetUsername("configured-deadline").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	order, err := s.createOrderInTx(ctx,
		CreateOrderRequest{UserID: user.ID, PaymentType: payment.TypeAlipay, OrderType: payment.OrderTypeBalance, ClientIP: "127.0.0.1", SrcHost: "fixture.invalid"},
		&User{ID: user.ID, Email: user.Email, Username: user.Username}, nil,
		&PaymentConfig{MaxPendingOrders: 10, OrderTimeoutMin: minutes}, 100, 100, 0, 100, 0,
		&payment.InstanceSelection{InstanceID: strconv.FormatInt(providerID, 10), ProviderKey: payment.TypeAlipayBill, Config: providerConfig, PaymentMode: "qrcode"})
	require.NoError(t, err)
	return order
}

func TestBillAmountConfiguredOrderTimeout(t *testing.T) {
	for _, source := range []string{"local", "external"} {
		for _, minutes := range []int{1, 2, 5} {
			t.Run(source+"_"+strconv.Itoa(minutes)+"_minutes", func(t *testing.T) {
				s, id, cfg := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
				ctx := context.Background()
				s.configService.settingRepo = &paymentConfigSettingRepoStub{values: map[string]string{
					SettingPaymentEnabled: "true", SettingOrderTimeoutMinutes: strconv.Itoa(minutes),
				}}
				var created, expires time.Time
				var localID, externalID int64
				if source == "local" {
					o := createConfiguredBillLocalFixture(t, s, id, cfg, minutes)
					created, expires, localID = o.CreatedAt, o.ExpiresAt, o.ID
				} else {
					response, err := s.CreateBuiltinEasyPayOrder(ctx, builtinEasyPayTestParams(id, "https://merchant.example"))
					require.NoError(t, err)
					o, err := findBuiltinEasyPayOrder(ctx, s.entClient, id, "", response.TradeNo)
					require.NoError(t, err)
					created, expires, externalID = o.CreatedAt, o.ExpiresAt, o.ID
				}
				require.Equal(t, time.Duration(minutes)*time.Minute, expires.Sub(created))
				require.Zero(t, created.Nanosecond())
				require.Zero(t, expires.Nanosecond())
				rows, err := s.entClient.QueryContext(ctx, `SELECT expires_at,reuse_after FROM payment_bill_amount_reservations WHERE order_id=$1 OR epay_order_id=$2`, localID, externalID)
				require.NoError(t, err)
				defer rows.Close()
				require.True(t, rows.Next())
				var reservationExpiry, reuse time.Time
				require.NoError(t, rows.Scan(&reservationExpiry, &reuse))
				require.True(t, expires.Equal(reservationExpiry))
				require.True(t, expires.Equal(reuse), "amount reuse must have the same deadline as payment")
			})
		}
	}
}

func TestBillAmountTwoMinutePaymentBoundaries(t *testing.T) {
	for _, seconds := range []int{61, 119, 120, 121} {
		t.Run(strconv.Itoa(seconds)+"_seconds", func(t *testing.T) {
			s, id, cfg := newBuiltinEasyPayServiceFixture(t, "https://merchant.example")
			ctx := context.Background()
			o := createConfiguredBillLocalFixture(t, s, id, cfg, 2)
			lifetime := o.ExpiresAt.Sub(o.CreatedAt)
			start := time.Now().UTC().Truncate(time.Second).Add(-3 * time.Minute)
			expiry := start.Add(lifetime)
			_, err := s.entClient.ExecContext(ctx, `UPDATE payment_orders SET created_at=$1,expires_at=$2,status='EXPIRED' WHERE id=$3`, start, expiry, o.ID)
			require.NoError(t, err)
			_, err = s.entClient.ExecContext(ctx, `UPDATE payment_bill_amount_reservations SET created_at=$1,expires_at=$2,reuse_after=$3 WHERE order_id=$4`, start, expiry, expiry.Add(30*time.Second), o.ID)
			require.NoError(t, err)
			o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
			require.NoError(t, err)
			credited := alipayGuardFulfillment(t, s, o)
			r, _, err := s.storeBillReceipt(ctx, id, "2088000000000001", provider.AlipayBillEntry{
				AccountLogID: "CONFIGURED_LOG", AlipayOrderNo: "CONFIGURED_TRADE", Amount: "100.00", Direction: "收入", TransAt: start.Add(time.Duration(seconds) * time.Second),
			})
			require.NoError(t, err)
			matched, err := s.reconcileBillReceiptThrough(ctx, r, time.Now().UTC())
			require.NoError(t, err)
			if seconds < 120 {
				require.True(t, matched, "a timely payment remains attributable when the statement arrives after expiry")
				require.Equal(t, 100.0, *credited)
			} else {
				require.False(t, matched, "the deadline is exclusive and must not auto-credit a late payment")
				require.Zero(t, *credited)
			}
		})
	}
}

func TestBillAmountReleasesAtDeadlineWithoutStatementSync(t *testing.T) {
	s, id := newBillReceiptTestService(t)
	start := time.Now().UTC().Truncate(time.Second)
	o := matchingBillLocalOrder(t, s, id, "NO_EXTRA_WAIT", start)
	err := billAmountReservationCheckAt(t, s, o.ExpiresAt.Add(-time.Second))
	require.Equal(t, "ALIPAY_BILL_AMOUNT_BUSY", infraerrors.Reason(err))
	require.Equal(t, "1", infraerrors.FromError(err).Metadata["retry_after_seconds"])
	require.NoError(t, billAmountReservationCheckAt(t, s, o.ExpiresAt), "expiry must release the amount even before a scheduler updates order status")
	require.NoError(t, billAmountReservationCheckAt(t, s, o.ExpiresAt.Add(time.Second)))
}

func TestBillPostgresConfiguredDeadlineStoresNoExtraWait(t *testing.T) {
	s, id := newBillAmountPostgresService(t)
	ctx := context.Background()
	_, cfg, err := s.billProviderConfig(ctx, id)
	require.NoError(t, err)
	o := createConfiguredBillLocalFixture(t, s, id, cfg, 2)
	require.Equal(t, 2*time.Minute, o.ExpiresAt.Sub(o.CreatedAt))
	rows, err := s.entClient.QueryContext(ctx, `SELECT expires_at,reuse_after FROM payment_bill_amount_reservations WHERE order_id=$1`, o.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var expiry, reuse time.Time
	require.NoError(t, rows.Scan(&expiry, &reuse))
	require.True(t, expiry.Equal(o.ExpiresAt))
	require.True(t, reuse.Equal(expiry), "the migrated PostgreSQL constraint must allow immediate reuse at the configured deadline")
}
