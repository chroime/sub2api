//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func newBillAvailabilityTestService(t *testing.T) *PaymentConfigService {
	t.Helper()
	return &PaymentConfigService{
		entClient:     newPaymentConfigServiceTestClient(t),
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		settingRepo:   &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentVisibleMethodAlipaySource: VisibleMethodSourceAlipayBill}},
	}
}

func createBillAvailabilityInstance(t *testing.T, s *PaymentConfigService, name, rules string, enabled bool) *dbent.PaymentProviderInstance {
	t.Helper()
	config, err := s.encryptProviderConfig(payment.TypeAlipayBill, map[string]string{
		"appId": "app-fixture", "receiverId": "2088000000000001", "privateKey": "private-secret", "publicKey": "public-secret", "appAuthToken": "auth-secret", "qrRules": rules,
	})
	require.NoError(t, err)
	inst, err := s.entClient.PaymentProviderInstance.Create().SetName(name).SetProviderKey(payment.TypeAlipayBill).
		SetSupportedTypes(payment.TypeAlipay).SetConfig(config).SetPaymentMode("qrcode").SetEnabled(enabled).Save(context.Background())
	require.NoError(t, err)
	return inst
}

func TestBillAvailabilityExposesOnlyUnionOfEnabledPayableAmounts(t *testing.T) {
	s := newBillAvailabilityTestService(t)
	createBillAvailabilityInstance(t, s, "first", `[{"id":"a","amount":"10","qr_url":"https://qr.alipay.com/secret-a","enabled":true},{"id":"b","amount":"0.01","qr_url":"https://qr.alipay.com/secret-b","enabled":true},{"id":"c","amount":"30.00","qr_url":"https://qr.alipay.com/secret-c","enabled":false}]`, true)
	createBillAvailabilityInstance(t, s, "second", `[{"id":"a","amount":"10.00","qr_url":"https://qr.alipay.com/secret-d","enabled":true},{"id":"b","amount":"20.01","qr_url":"https://qr.alipay.com/secret-e","enabled":true}]`, true)
	createBillAvailabilityInstance(t, s, "disabled", `[{"id":"a","amount":"40","qr_url":"https://qr.alipay.com/secret-f","enabled":true}]`, false)
	limits, err := s.GetAvailableMethodLimits(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(limits.Methods[payment.TypeAlipay])
	require.NoError(t, err)
	var method struct {
		PayAmounts       []string `json:"pay_amounts"`
		FixedAmountsOnly bool     `json:"fixed_amounts_only"`
	}
	require.NoError(t, json.Unmarshal(raw, &method))
	require.True(t, method.FixedAmountsOnly)
	require.Equal(t, []string{"0.01", "10.00", "20.01"}, method.PayAmounts)
	for _, sensitive := range []string{"secret", "https://", "qrRules", "2088"} {
		require.NotContains(t, string(raw), sensitive)
	}
}

func TestBillAvailabilityOmitsMethodsWithoutUsableRules(t *testing.T) {
	for _, rules := range []string{"[]", `[{"id":"a","amount":"10","qr_url":"https://qr.alipay.com/a","enabled":false}]`, "not-json"} {
		t.Run(rules, func(t *testing.T) {
			s := newBillAvailabilityTestService(t)
			createBillAvailabilityInstance(t, s, "empty", rules, true)
			limits, err := s.GetAvailableMethodLimits(context.Background())
			require.NoError(t, err)
			require.NotContains(t, limits.Methods, payment.TypeAlipay)
		})
	}
}

func TestBillAvailabilityDoesNotRestrictSelectedOfficialSource(t *testing.T) {
	s := newBillAvailabilityTestService(t)
	s.settingRepo = &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentVisibleMethodAlipaySource: VisibleMethodSourceOfficialAlipay}}
	createBillAvailabilityInstance(t, s, "bill", `[{"id":"a","amount":"10","qr_url":"https://qr.alipay.com/a","enabled":true}]`, true)
	_, err := s.entClient.PaymentProviderInstance.Create().SetName("official").SetProviderKey(payment.TypeAlipay).
		SetSupportedTypes(payment.TypeAlipay).SetConfig("{}").SetEnabled(true).Save(context.Background())
	require.NoError(t, err)
	limits, err := s.GetAvailableMethodLimits(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(limits.Methods[payment.TypeAlipay])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fixed_amounts_only")
	require.NotContains(t, string(raw), "pay_amounts")
}

func TestBillAvailabilitySelectsMatchingInstanceAcrossDisjointRules(t *testing.T) {
	s := newBillAvailabilityTestService(t)
	ten := createBillAvailabilityInstance(t, s, "ten", `[{"id":"a","amount":"10","qr_url":"https://qr.alipay.com/ten","enabled":true}]`, true)
	twenty := createBillAvailabilityInstance(t, s, "twenty", `[{"id":"a","amount":"20.01","qr_url":"https://qr.alipay.com/twenty","enabled":true}]`, true)
	createBillAvailabilityInstance(t, s, "disabled-rule", `[{"id":"a","amount":"10","qr_url":"https://qr.alipay.com/disabled","enabled":false}]`, true)
	createBillAvailabilityInstance(t, s, "invalid", "not-json", true)
	lb := newVisibleMethodLoadBalancer(payment.NewDefaultLoadBalancer(s.entClient, s.encryptionKey), s)
	for _, strategy := range []payment.Strategy{payment.StrategyRoundRobin, payment.StrategyLeastAmount} {
		for _, tc := range []struct {
			amount float64
			want   int64
		}{{10, ten.ID}, {20.01, twenty.ID}, {10, ten.ID}, {20.01, twenty.ID}} {
			selection, err := lb.SelectInstance(context.Background(), "", payment.TypeAlipay, strategy, tc.amount)
			require.NoError(t, err)
			require.Equal(t, strconv.FormatInt(tc.want, 10), selection.InstanceID)
		}
	}
	for _, amount := range []float64{0, 10.01, 20, 20.011} {
		selection, err := lb.SelectInstance(context.Background(), "", payment.TypeAlipay, payment.StrategyRoundRobin, amount)
		require.Nil(t, selection)
		require.Equal(t, "ALIPAY_BILL_AMOUNT_UNAVAILABLE", infraerrors.Reason(err))
		if err != nil {
			require.False(t, strings.Contains(err.Error(), "secret"))
		}
	}
}

func TestBillAvailabilityRejectsUnavailableAmountBeforeOrderCreation(t *testing.T) {
	s := newBillAvailabilityTestService(t)
	createBillAvailabilityInstance(t, s, "ten", `[{"id":"a","amount":"10","qr_url":"https://qr.alipay.com/ten","enabled":true}]`, true)
	service := &PaymentService{configService: s, entClient: s.entClient, loadBalancer: newVisibleMethodLoadBalancer(payment.NewDefaultLoadBalancer(s.entClient, s.encryptionKey), s)}
	selection, err := service.selectCreateOrderInstance(context.Background(), CreateOrderRequest{PaymentType: payment.TypeAlipay}, &PaymentConfig{}, 10.01)
	require.Nil(t, selection)
	require.Equal(t, "ALIPAY_BILL_AMOUNT_UNAVAILABLE", infraerrors.Reason(err))
	count, err := s.entClient.PaymentOrder.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}
