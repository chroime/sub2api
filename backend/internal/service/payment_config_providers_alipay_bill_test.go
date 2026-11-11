//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAlipayBillProviderConfigStoredEncryptedAndSecretsMasked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := &PaymentConfigService{entClient: newPaymentConfigServiceTestClient(t), encryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	cfg := alipayBillDraftConfig()
	instance, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey: "alipay_bill", Name: "Fixed Alipay", Config: cfg,
		SupportedTypes: []string{"alipay"}, PaymentMode: "qrcode",
	})
	require.NoError(t, err)
	require.False(t, json.Valid([]byte(instance.Config)))
	require.NotContains(t, instance.Config, cfg["privateKey"])
	plaintext, err := payment.Decrypt(instance.Config, svc.encryptionKey)
	require.NoError(t, err)
	var stored map[string]string
	require.NoError(t, json.Unmarshal([]byte(plaintext), &stored))
	require.Equal(t, cfg, stored)
	masked, err := svc.decryptAndMaskConfig("alipay_bill", instance.Config)
	require.NoError(t, err)
	for _, field := range []string{"privateKey", "publicKey", "alipayPublicKey", "appAuthToken"} {
		require.NotContains(t, masked, field)
	}
	require.Equal(t, "2088000000000001", masked["receiverId"])

	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{
		"privateKey": "", "appAuthToken": "", "qrRules": "[]",
	}})
	require.NoError(t, err)
	require.False(t, json.Valid([]byte(updated.Config)))
	stored, err = svc.decryptConfig(updated.Config)
	require.NoError(t, err)
	require.Equal(t, "private-key-for-test", stored["privateKey"])
	require.Equal(t, "app-auth-for-test", stored["appAuthToken"])
}

func TestAlipayBillProviderRejectsSaveWithoutEncryptionKey(t *testing.T) {
	t.Parallel()
	svc := &PaymentConfigService{entClient: newPaymentConfigServiceTestClient(t)}
	instance, err := svc.CreateProviderInstance(context.Background(), CreateProviderInstanceRequest{
		ProviderKey: "alipay_bill", Name: "Fixed Alipay", Config: alipayBillDraftConfig(),
		SupportedTypes: []string{"alipay"}, PaymentMode: "qrcode",
	})
	require.Nil(t, instance)
	require.Equal(t, "PAYMENT_CONFIG_ENCRYPTION_NOT_CONFIGURED", infraerrors.Reason(err))
}

func TestAlipayBillProviderRejectsUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		types              []string
		mode               string
		refund, userRefund bool
	}{
		{name: "other payment type", types: []string{"wxpay"}, mode: "qrcode"},
		{name: "mixed payment types", types: []string{"alipay", "wxpay"}, mode: "qrcode"},
		{name: "redirect", types: []string{"alipay"}, mode: "redirect"},
		{name: "automatic refund", types: []string{"alipay"}, mode: "qrcode", refund: true},
		{name: "user refund", types: []string{"alipay"}, mode: "qrcode", userRefund: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &PaymentConfigService{entClient: newPaymentConfigServiceTestClient(t), encryptionKey: []byte("0123456789abcdef0123456789abcdef")}
			_, err := svc.CreateProviderInstance(context.Background(), CreateProviderInstanceRequest{
				ProviderKey: "alipay_bill", Name: "Fixed Alipay", Config: alipayBillDraftConfig(),
				SupportedTypes: tc.types, PaymentMode: tc.mode, RefundEnabled: tc.refund, AllowUserRefund: tc.userRefund,
			})
			require.Equal(t, "VALIDATION_ERROR", infraerrors.Reason(err))
		})
	}
}

func TestAlipayBillProviderProtectsIdentityWithHistoricalOrders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	createPendingProviderConfigOrder(t, ctx, svc.entClient, instance)
	_, err := svc.entClient.PaymentOrder.Update().SetStatus(payment.OrderStatusExpired).Save(ctx)
	require.NoError(t, err)
	for _, field := range []string{"appId", "receiverId"} {
		_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{field: "changed"}})
		require.Equal(t, "PAYMENT_RECONCILIATION_HISTORY", infraerrors.Reason(err))
	}
	require.Equal(t, "PAYMENT_RECONCILIATION_HISTORY", infraerrors.Reason(svc.DeleteProviderInstance(ctx, instance.ID)))
	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"privateKey": "rotated-private-key"}})
	require.NoError(t, err)
	require.NotNil(t, updated)
}

func TestAlipayBillProviderProtectsIdentityWithSharedReceiverReceipts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.entClient.ExecContext(ctx, `CREATE TABLE payment_bill_receipts (provider_instance_id BIGINT NOT NULL, receiver_id TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = svc.entClient.ExecContext(ctx, `INSERT INTO payment_bill_receipts (provider_instance_id, receiver_id) VALUES ($1, $2)`, instance.ID+100, "2088000000000001")
	require.NoError(t, err)
	_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"receiverId": "2088000000000002"}})
	require.Equal(t, "PAYMENT_RECONCILIATION_HISTORY", infraerrors.Reason(err))
	require.Equal(t, "PAYMENT_RECONCILIATION_HISTORY", infraerrors.Reason(svc.DeleteProviderInstance(ctx, instance.ID)))
}

func TestAlipayBillProviderProtectsCredentialsWithPendingOrders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	createPendingProviderConfigOrder(t, ctx, svc.entClient, instance)
	for _, field := range []string{"privateKey", "publicKey", "alipayPublicKey", "appAuthToken", "appId", "receiverId"} {
		_, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{field: "changed"}})
		require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err), field)
	}
}

func TestAlipayBillProviderRejectsRefundPatch(t *testing.T) {
	t.Parallel()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.UpdateProviderInstance(context.Background(), instance.ID, UpdateProviderInstanceRequest{RefundEnabled: boolPtrValue(true)})
	require.Equal(t, "VALIDATION_ERROR", infraerrors.Reason(err))
	_, err = svc.UpdateProviderInstance(context.Background(), instance.ID, UpdateProviderInstanceRequest{AllowUserRefund: boolPtrValue(true)})
	require.Equal(t, "VALIDATION_ERROR", infraerrors.Reason(err))
}

func TestAlipayBillProviderDraftCanChangeIdentityBeforeHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.entClient.ExecContext(ctx, `CREATE TABLE payment_bill_receipts (provider_instance_id BIGINT NOT NULL, receiver_id TEXT NOT NULL)`)
	require.NoError(t, err)
	updated, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{
		"receiverId": "2088000000000002", "appId": "next-app",
	}})
	require.NoError(t, err)
	config, err := svc.decryptConfig(updated.Config)
	require.NoError(t, err)
	require.Equal(t, "2088000000000002", config["receiverId"])
	require.NoError(t, svc.DeleteProviderInstance(ctx, instance.ID))
}

func TestAlipayBillProviderRejectsInvalidRulesInDraft(t *testing.T) {
	t.Parallel()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.UpdateProviderInstance(context.Background(), instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{
		"qrRules": `[{"id":"qr-1","amount":"12.345","qr_url":"https://qr.alipay.com/test","enabled":true,"sort_order":0}]`,
	}})
	require.Equal(t, "ALIPAY_BILL_INVALID_RULES", infraerrors.Reason(err))
}

func TestAlipayBillProviderCannotOverwriteUndecryptableCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	encrypted, err := payment.Encrypt(instance.Config, svc.encryptionKey)
	require.NoError(t, err)
	_, err = svc.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetConfig(encrypted).Save(ctx)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		key    []byte
		reason string
	}{
		{name: "missing key", reason: "PAYMENT_CONFIG_ENCRYPTION_NOT_CONFIGURED"},
		{name: "wrong key", key: []byte("fedcba9876543210fedcba9876543210"), reason: "PAYMENT_CONFIG_DECRYPTION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &PaymentConfigService{entClient: svc.entClient, encryptionKey: tc.key}
			_, err := reader.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"qrRules": "[]"}})
			require.Equal(t, tc.reason, infraerrors.Reason(err))
			_, err = reader.ListProviderInstancesWithConfig(ctx)
			require.Equal(t, tc.reason, infraerrors.Reason(err))
		})
	}
	unchanged, err := svc.entClient.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, encrypted, unchanged.Config)
}

func TestAlipayBillProviderRejectsMalformedPlaintextConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetConfig(`["invalid-config"]`).Save(ctx)
	require.NoError(t, err)
	_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"qrRules": "[]"}})
	require.Equal(t, "PAYMENT_CONFIG_DECRYPTION_FAILED", infraerrors.Reason(err))
}

func TestAlipayBillProviderNeverOffersUserRefundForLegacyFlags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetRefundEnabled(true).SetAllowUserRefund(true).Save(ctx)
	require.NoError(t, err)
	ids, err := svc.GetUserRefundEligibleInstanceIDs(ctx)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestAlipayBillProviderRejectsAmbiguousConfigFieldCasing(t *testing.T) {
	t.Parallel()
	svc, instance := newAlipayBillConfigFixture(t)
	_, err := svc.UpdateProviderInstance(context.Background(), instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{
		"ReceiverId": "2088000000000002",
	}})
	require.Equal(t, "VALIDATION_ERROR", infraerrors.Reason(err))
}

func TestAlipayBillProviderMutationRollsBackAllChangesOnFailure(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			svc, instance := newAlipayBillConfigFixture(t)
			_, err := svc.entClient.ExecContext(ctx, `CREATE TABLE payment_bill_receipts (provider_instance_id BIGINT NOT NULL, receiver_id TEXT NOT NULL)`)
			require.NoError(t, err)
			failed := errors.New("provider mutation failed")
			svc.entClient.PaymentProviderInstance.Use(func(next dbent.Mutator) dbent.Mutator {
				return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
					if mutation.Op().Is(dbent.OpUpdateOne | dbent.OpDeleteOne) {
						m := mutation.(*dbent.PaymentProviderInstanceMutation)
						_, err := m.Client().PaymentProviderInstance.Create().SetProviderKey("alipay_bill").SetName("side-effect").SetConfig("{}").Save(ctx)
						if err != nil {
							return nil, err
						}
						return nil, failed
					}
					return next.Mutate(ctx, mutation)
				})
			})
			if operation == "update" {
				name := "updated-name"
				_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Name: &name})
			} else {
				err = svc.DeleteProviderInstance(ctx, instance.ID)
			}
			require.ErrorIs(t, err, failed)
			instances, err := svc.entClient.PaymentProviderInstance.Query().All(ctx)
			require.NoError(t, err)
			require.Len(t, instances, 1, "failed bill configuration mutations must roll back their transaction")
			require.Equal(t, "Fixed Alipay", instances[0].Name)
		})
	}
}

func newAlipayBillConfigFixture(t *testing.T) (*PaymentConfigService, *dbent.PaymentProviderInstance) {
	t.Helper()
	svc := &PaymentConfigService{entClient: newPaymentConfigServiceTestClient(t), encryptionKey: []byte("0123456789abcdef0123456789abcdef")}
	_, err := svc.entClient.ExecContext(context.Background(), `CREATE TABLE payment_epay_orders (id INTEGER PRIMARY KEY, provider_instance_id BIGINT NOT NULL, status TEXT NOT NULL, expires_at TIMESTAMP NOT NULL, notify_status TEXT NOT NULL)`)
	require.NoError(t, err)
	stored, err := json.Marshal(alipayBillDraftConfig())
	require.NoError(t, err)
	instance, err := svc.entClient.PaymentProviderInstance.Create().SetProviderKey("alipay_bill").SetName("Fixed Alipay").
		SetConfig(string(stored)).SetSupportedTypes("alipay").SetPaymentMode("qrcode").SetEnabled(false).Save(context.Background())
	require.NoError(t, err)
	return svc, instance
}

func alipayBillDraftConfig() map[string]string {
	return map[string]string{
		"appId": "app-fixed-test", "receiverId": "2088000000000001", "privateKey": "private-key-for-test",
		"publicKey": "public-key-for-test", "alipayPublicKey": "alipay-public-key-for-test", "appAuthToken": "app-auth-for-test", "qrRules": "[]",
	}
}
