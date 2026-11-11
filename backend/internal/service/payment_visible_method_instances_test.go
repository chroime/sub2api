//go:build unit

package service

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAlipayBillSourceRouting(t *testing.T) {
	t.Parallel()

	require.Equal(t, "alipay_bill", NormalizeVisibleMethodSource("alipay", " ALIPAY_BILL "))
	key, ok := VisibleMethodProviderKeyForSource("alipay", "alipay_bill")
	require.True(t, ok)
	require.Equal(t, "alipay_bill", key)
	_, ok = VisibleMethodProviderKeyForSource("wxpay", "alipay_bill")
	require.False(t, ok)
	require.Equal(t, []string{"alipay"}, enabledVisibleMethodsForProvider("alipay_bill", "alipay"))

	instances := []*dbent.PaymentProviderInstance{
		{ID: 1, ProviderKey: "easypay", SupportedTypes: "alipay", Enabled: true},
		{ID: 2, ProviderKey: "alipay_bill", SupportedTypes: "alipay", Enabled: true},
		{ID: 3, ProviderKey: "alipay", SupportedTypes: "alipay", Enabled: true},
	}
	svc := &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentVisibleMethodAlipaySource: "alipay_bill",
	}}}
	key, err := svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", filterEnabledVisibleMethodInstances(instances, "alipay"))
	require.NoError(t, err)
	require.Equal(t, "alipay_bill", key)

	available := buildVisibleMethodSourceAvailability(instances)
	require.True(t, available["alipay_bill"])
	require.Equal(t, []string{"alipay"}, applyVisibleMethodRoutingToEnabledTypes(nil, map[string]string{
		SettingPaymentVisibleMethodAlipayEnabled: "true",
		SettingPaymentVisibleMethodAlipaySource:  "alipay_bill",
	}, available))
}

func TestVisibleMethodSelectedSourceDoesNotFallBackToOnlyOtherProvider(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"official_alipay", "easypay_alipay", "alipay_bill"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			svc := &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
				SettingPaymentVisibleMethodAlipaySource: source,
			}}}
			otherProvider := "easypay"
			if source == "easypay_alipay" {
				otherProvider = "alipay"
			}
			key, err := svc.resolveVisibleMethodProviderKey(context.Background(), payment.TypeAlipay, []*dbent.PaymentProviderInstance{
				{ProviderKey: otherProvider, SupportedTypes: "alipay", Enabled: true},
			})
			require.Error(t, err)
			require.Equal(t, "INVALID_PAYMENT_VISIBLE_METHOD_SOURCE", infraerrors.Reason(err))
			require.Empty(t, key)
		})
	}
}

func TestVisibleMethodMissingSourceKeepsExistingRouting(t *testing.T) {
	t.Parallel()
	svc := &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{}}}
	key, err := svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", []*dbent.PaymentProviderInstance{{ProviderKey: "easypay"}})
	require.NoError(t, err)
	require.Equal(t, "easypay", key)
	key, err = svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", []*dbent.PaymentProviderInstance{{ProviderKey: "easypay"}, {ProviderKey: "alipay"}})
	require.NoError(t, err)
	require.Empty(t, key)
}

func TestVisibleMethodMissingSourceRequiresExplicitAlipayBillSelection(t *testing.T) {
	t.Parallel()
	svc := &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{}}}
	key, err := svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", []*dbent.PaymentProviderInstance{
		{ProviderKey: "alipay_bill"},
	})
	require.Equal(t, "INVALID_PAYMENT_VISIBLE_METHOD_SOURCE", infraerrors.Reason(err))
	require.Empty(t, key)
	key, err = svc.resolveVisibleMethodProviderKey(context.Background(), "alipay", []*dbent.PaymentProviderInstance{
		{ProviderKey: "alipay_bill"}, {ProviderKey: "easypay"},
	})
	require.NoError(t, err)
	require.Equal(t, "easypay", key)
}

func TestUpdatePaymentConfigNormalizesAndValidatesAlipayBillSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
	svc := &PaymentConfigService{settingRepo: repo}
	value := " ALIPAY_BILL "
	require.NoError(t, svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{VisibleMethodAlipaySource: &value}))
	require.Equal(t, "alipay_bill", repo.values[SettingPaymentVisibleMethodAlipaySource])
	require.Equal(t, "INVALID_PAYMENT_VISIBLE_METHOD_SOURCE", infraerrors.Reason(svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{VisibleMethodWxpaySource: &value})))
	value = "unsupported"
	require.Equal(t, "INVALID_PAYMENT_VISIBLE_METHOD_SOURCE", infraerrors.Reason(svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{VisibleMethodAlipaySource: &value})))
	require.Equal(t, "alipay_bill", repo.values[SettingPaymentVisibleMethodAlipaySource])
}

func TestVisibleMethodMissingSourceExcludesAlipayBillLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	for _, tc := range []struct{ key, limits string }{
		{key: "alipay", limits: `{"alipay":{"singleMin":10,"singleMax":100}}`},
		{key: "easypay", limits: `{"alipay":{"singleMin":20,"singleMax":200}}`},
		{key: "alipay_bill", limits: `{"alipay":{"singleMin":1,"singleMax":1000}}`},
	} {
		_, err := client.PaymentProviderInstance.Create().SetProviderKey(tc.key).SetName(tc.key).SetConfig("{}").
			SetSupportedTypes("alipay").SetLimits(tc.limits).SetEnabled(true).Save(ctx)
		require.NoError(t, err)
	}
	svc := &PaymentConfigService{entClient: client, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{}}}
	limits, err := svc.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	require.Equal(t, 10.0, limits.Methods["alipay"].SingleMin)
	require.Equal(t, 200.0, limits.Methods["alipay"].SingleMax)
}
