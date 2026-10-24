//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateOrderInputAcceptsOnlyFixedRechargeAmounts(t *testing.T) {
	svc := &PaymentService{}
	ctx := context.Background()
	for _, amount := range []float64{10, 20, 50, 100, 200, 500, 1000, 2000, 5000} {
		t.Run("allowed_"+strconv.FormatFloat(amount, 'f', -1, 64), func(t *testing.T) {
			plan, err := svc.validateOrderInput(ctx, CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: amount}, &PaymentConfig{})
			require.NoError(t, err)
			require.Nil(t, plan)
		})
	}

	for _, amount := range []float64{0, -10, 1, 9.99, 12.34, 30, 99.999, 100.001, 5001, math.NaN(), math.Inf(1), math.Inf(-1), math.Nextafter(10, 0), math.Nextafter(10, 20)} {
		t.Run("rejected_"+strconv.FormatFloat(amount, 'f', -1, 64), func(t *testing.T) {
			plan, err := svc.validateOrderInput(ctx, CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: amount}, &PaymentConfig{})
			require.Nil(t, plan)
			require.Equal(t, "INVALID_AMOUNT", infraerrors.Reason(err))
		})
	}
}

func TestValidateOrderInputKeepsRechargeRange(t *testing.T) {
	svc := &PaymentService{}
	cfg := &PaymentConfig{MinAmount: 20, MaxAmount: 200}
	for _, amount := range []float64{20, 50, 100, 200} {
		_, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: amount}, cfg)
		require.NoError(t, err, "amount %v must remain available at inclusive range boundaries", amount)
	}
	for _, amount := range []float64{10, 500} {
		_, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: amount}, cfg)
		require.Equal(t, "INVALID_AMOUNT", infraerrors.Reason(err))
		require.Equal(t, map[string]string{"min": "20.00", "max": "200.00"}, infraerrors.FromError(err).Metadata)
	}
}

func TestRechargeAmountsReturnsIndependentSlices(t *testing.T) {
	cfg := &PaymentConfig{}
	amounts := cfg.RechargeAmounts()
	amounts[0] = 12.34
	require.Equal(t, []float64{10, 20, 50, 100, 200, 500, 1000, 2000, 5000}, cfg.RechargeAmounts())
	_, err := (&PaymentService{}).validateOrderInput(context.Background(), CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: 12.34}, cfg)
	require.Equal(t, "INVALID_AMOUNT", infraerrors.Reason(err))
}

func TestValidateOrderInputUsesNominalRechargeAmountBeforeAdjustments(t *testing.T) {
	svc := &PaymentService{}
	for _, mode := range []string{RechargeBonusModeBonus, RechargeBonusModeDiscount} {
		t.Run(mode, func(t *testing.T) {
			cfg := &PaymentConfig{
				BalanceRechargeMultiplier: 0.14,
				RechargeFeeRate:           2.5,
				RechargeBonusMode:         mode,
				RechargeBonusTiers:        []RechargeBonusTier{{MinAmount: 100, BonusPercent: 20}},
			}
			_, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: 100}, cfg)
			require.NoError(t, err)
			quote := quoteRechargeBonus(cfg, 100, payment.DefaultPaymentCurrency)
			if mode == RechargeBonusModeDiscount {
				require.Equal(t, 80.0, quote.PayBase)
				require.Equal(t, 14.0, quote.Credited)
			} else {
				require.Equal(t, 100.0, quote.PayBase)
				require.Equal(t, 16.8, quote.Credited)
			}
		})
	}
}

type rechargeAmountUserRepoSpy struct {
	UserRepository
	getCalls int
}

func (s *rechargeAmountUserRepoSpy) GetByID(context.Context, int64) (*User, error) {
	s.getCalls++
	return nil, errors.New("user lookup must not run for a custom recharge amount")
}

func TestCreateOrderRejectsCustomRechargeBeforeSideEffects(t *testing.T) {
	for _, orderType := range []string{"", payment.OrderTypeBalance} {
		t.Run("order_type_"+orderType, func(t *testing.T) {
			userRepo := &rechargeAmountUserRepoSpy{}
			svc := &PaymentService{
				configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}},
				userRepo:      userRepo,
			}
			resp, err := svc.CreateOrder(context.Background(), CreateOrderRequest{UserID: 42, OrderType: orderType, Amount: 12.34, PaymentType: payment.TypeAlipay})
			require.Nil(t, resp)
			require.Equal(t, "INVALID_AMOUNT", infraerrors.Reason(err))
			require.Zero(t, userRepo.getCalls, "invalid amount must be rejected before user lookup, provider selection, or order creation")
		})
	}
}

func TestValidateOrderInputSubscriptionStillUsesServerPlanPrice(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	group := client.Group.Create().SetName("Recharge preset subscription test").SetRateMultiplier(1).SetStatus(payment.EntityStatusActive).SetSubscriptionType(SubscriptionTypeSubscription).SaveX(ctx)
	plan := client.SubscriptionPlan.Create().SetGroupID(group.ID).SetName("Monthly plan").SetPrice(9.99).SetForSale(true).SetValidityDays(30).SetValidityUnit("days").SaveX(ctx)
	svc := &PaymentService{
		configService: &PaymentConfigService{entClient: client},
		groupRepo:     &subscriptionGroupRepoStub{group: &Group{ID: group.ID, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription}},
	}
	for _, amount := range []float64{0, 12.34, math.NaN()} {
		got, err := svc.validateOrderInput(ctx, CreateOrderRequest{OrderType: payment.OrderTypeSubscription, Amount: amount, PlanID: plan.ID}, &PaymentConfig{BalanceDisabled: true, MinAmount: 1000, MaxAmount: 2000})
		require.NoError(t, err)
		require.Equal(t, plan.ID, got.ID)
		require.Equal(t, 9.99, got.Price)
	}
}
