//go:build unit

package payment

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestEligibleProviderInstancesRestrictSelectionWithoutFallback(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	first, err := client.PaymentProviderInstance.Create().SetProviderKey(TypeAlipayBill).SetName("first").SetConfig("{}").SetSupportedTypes(TypeAlipay).SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	second, err := client.PaymentProviderInstance.Create().SetProviderKey(TypeAlipayBill).SetName("second").SetConfig("{}").SetSupportedTypes(TypeAlipay).SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	lb := NewDefaultLoadBalancer(client, nil)
	ids := []int64{first.ID}
	filteredCtx := WithEligibleProviderInstances(ctx, ids)
	ids[0] = second.ID // A caller changing its slice cannot change selection permissions.
	for _, strategy := range []Strategy{StrategyRoundRobin, StrategyLeastAmount} {
		for range 3 {
			selection, err := lb.SelectInstance(filteredCtx, TypeAlipayBill, TypeAlipay, strategy, 10)
			require.NoError(t, err)
			require.Equal(t, strconv.FormatInt(first.ID, 10), selection.InstanceID)
		}
	}
	for _, ids := range [][]int64{nil, {}, {-1}} {
		selection, err := lb.SelectInstance(WithEligibleProviderInstances(ctx, ids), TypeAlipayBill, TypeAlipay, StrategyRoundRobin, 10)
		require.Error(t, err)
		require.Nil(t, selection, "an empty candidate set must never fall back to other enabled providers")
	}
	selection, err := lb.SelectInstance(filteredCtx, "", TypeAlipay, StrategyRoundRobin, 10)
	require.Error(t, err)
	require.Nil(t, selection, "eligible IDs do not opt fixed QR into implicit source selection")
}
