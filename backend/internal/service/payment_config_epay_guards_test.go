//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBuiltinEasyPayConfigMasksSeparateMerchantKey(t *testing.T) {
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	key := strings.Repeat("d", 64)
	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{
		"epayKey": key, "epayEnabled": "true", "epayNotifyOrigins": `["https://merchant.example.com"]`, "epayKeyConfigured": "false",
	}})
	require.NoError(t, err)
	require.NotContains(t, updated.Config, key)
	stored, err := svc.decryptProviderConfig(payment.TypeAlipayBill, updated.Config)
	require.NoError(t, err)
	require.Equal(t, key, stored["epayKey"])
	require.NotContains(t, stored, "epayKeyConfigured")
	response, err := svc.ProviderInstanceResponseForAdmin(updated)
	require.NoError(t, err)
	require.Equal(t, "true", response.Config["epayKeyConfigured"])
	require.NotContains(t, response.Config, "epayKey")
	updated, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"epayKey": ""}})
	require.NoError(t, err)
	stored, err = svc.decryptProviderConfig(payment.TypeAlipayBill, updated.Config)
	require.NoError(t, err)
	require.Equal(t, key, stored["epayKey"], "blank masked form retains the stored key")
}

func TestBuiltinEasyPayConfigProtectsUnresolvedOrdersAndCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name, status, notify string
		expires              time.Duration
		blocked              bool
	}{
		{"pending payment", "PENDING", "none", time.Hour, true},
		{"expired pending payment", "PENDING", "none", -time.Hour, false},
		{"pending notification", "PAID", "pending", -time.Hour, true},
		{"delivered notification", "PAID", "delivered", -time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, instance := newAlipayBillConfigFixture(t)
			_, err := svc.entClient.ExecContext(ctx, `INSERT INTO payment_epay_orders (provider_instance_id,status,expires_at,notify_status) VALUES ($1,$2,$3,$4)`, instance.ID, tc.status, time.Now().UTC().Add(tc.expires), tc.notify)
			require.NoError(t, err)
			_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"epayKey": strings.Repeat("e", 64)}})
			if tc.blocked {
				require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
			} else {
				require.NoError(t, err)
			}
			_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"epayEnabled": "false"}})
			require.NoError(t, err, "stopping new gateway orders must not require losing old-order recovery")
		})
	}
}

func TestBuiltinEasyPayConfigHistoryPreservesRecipientAndProvider(t *testing.T) {
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.entClient.ExecContext(ctx, `INSERT INTO payment_epay_orders (provider_instance_id,status,expires_at,notify_status) VALUES ($1,'PAID',$2,'delivered')`, instance.ID, time.Now().UTC().Add(-time.Hour))
	require.NoError(t, err)
	for _, field := range []string{"appId", "receiverId"} {
		_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{field: "changed"}})
		require.Equal(t, "PAYMENT_RECONCILIATION_HISTORY", infraerrors.Reason(err))
	}
	require.Equal(t, "PAYMENT_RECONCILIATION_HISTORY", infraerrors.Reason(svc.DeleteProviderInstance(ctx, instance.ID)))
}
