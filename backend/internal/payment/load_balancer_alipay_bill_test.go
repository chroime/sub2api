//go:build unit

package payment

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestAlipayBillIsExcludedFromImplicitLoadBalancing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypted, err := Encrypt(`{"appId":"fixed-app","receiverId":"2088000000000001","privateKey":"test-private","publicKey":"test-public","qrRules":"[]"}`, key)
	require.NoError(t, err)
	bill, err := client.PaymentProviderInstance.Create().SetProviderKey(TypeAlipayBill).SetName("Fixed Alipay").SetConfig(encrypted).
		SetSupportedTypes("alipay").SetPaymentMode("qrcode").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	lb := NewDefaultLoadBalancer(client, key)
	_, err = lb.queryEnabledInstances(ctx, "", TypeAlipay)
	require.Error(t, err, "a fixed QR provider needs explicit source selection")
	instances, err := lb.queryEnabledInstances(ctx, TypeAlipayBill, TypeAlipay)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	require.Equal(t, bill.ID, instances[0].ID)
	legacy, err := client.PaymentProviderInstance.Create().SetProviderKey(TypeEasyPay).SetName("Legacy Alipay").SetConfig("{}").
		SetSupportedTypes("alipay").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	instances, err = lb.queryEnabledInstances(ctx, "", TypeAlipay)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	require.Equal(t, legacy.ID, instances[0].ID)
}
