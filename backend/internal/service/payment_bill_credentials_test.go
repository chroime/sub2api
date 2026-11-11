//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBillEditCredentialsRestoresExactSavedKeysOnly(t *testing.T) {
	ctx := context.Background()
	configSvc, instance := newAlipayBillConfigFixture(t)
	privateKey := "-----BEGIN PRIVATE KEY-----\nfixture-application-private-key\n-----END PRIVATE KEY-----"
	publicKey := "-----BEGIN PUBLIC KEY-----\nfixture-alipay-public-key\n-----END PUBLIC KEY-----"
	saved, err := configSvc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{
		"privateKey": privateKey, "publicKey": publicKey, "epayKey": "separate-epay-key-that-must-stay-masked",
	}})
	require.NoError(t, err)
	s := &PaymentService{entClient: configSvc.entClient, configService: configSvc}
	credentials, err := s.GetAlipayBillEditCredentials(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, privateKey, credentials.PrivateKey)
	require.Equal(t, publicKey, credentials.PublicKey)
	encoded, err := json.Marshal(credentials)
	require.NoError(t, err)
	var fields map[string]string
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.Len(t, fields, 2)
	require.NotContains(t, string(encoded), "separate-epay-key")
	require.NotContains(t, string(encoded), "app-auth-for-test")

	// Viewing the editor does not change encrypted storage or the list response.
	after, err := configSvc.entClient.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, saved.Config, after.Config)
	require.NotContains(t, after.Config, privateKey)
	require.False(t, json.Valid([]byte(after.Config)))
	list, err := configSvc.ListProviderInstancesWithConfig(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotContains(t, list[0].Config, "privateKey")
	require.NotContains(t, list[0].Config, "publicKey")
	require.NotContains(t, list[0].Config, "epayKey")
}

func TestBillEditCredentialsRejectsMissingAndOtherProviders(t *testing.T) {
	ctx := context.Background()
	configSvc, instance := newAlipayBillConfigFixture(t)
	s := &PaymentService{entClient: configSvc.entClient, configService: configSvc}
	credentials, err := s.GetAlipayBillEditCredentials(ctx, instance.ID+100)
	require.Nil(t, credentials)
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(err))
	_, err = configSvc.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).SetProviderKey(payment.TypeEasyPay).Save(ctx)
	require.NoError(t, err)
	credentials, err = s.GetAlipayBillEditCredentials(ctx, instance.ID)
	require.Nil(t, credentials)
	require.Equal(t, "ALIPAY_BILL_PROVIDER_REQUIRED", infraerrors.Reason(err))
}

func TestBillEditCredentialsSupportsCredentialOnlyDraft(t *testing.T) {
	ctx := context.Background()
	configSvc, instance := newAlipayBillConfigFixture(t)
	_, err := configSvc.entClient.PaymentProviderInstance.UpdateOneID(instance.ID).
		SetConfig(`{"appId":"draft-app","privateKey":"saved-private-key","publicKey":"saved-public-key"}`).Save(ctx)
	require.NoError(t, err)
	s := &PaymentService{entClient: configSvc.entClient, configService: configSvc}
	credentials, err := s.GetAlipayBillEditCredentials(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, "saved-private-key", credentials.PrivateKey)
	require.Equal(t, "saved-public-key", credentials.PublicKey)
}
