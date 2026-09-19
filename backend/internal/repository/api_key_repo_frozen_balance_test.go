package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepository_GetByKeyForAuth_PreservesFrozenBalance_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "auth-frozen-balance@test.com")
	key := &service.APIKey{
		UserID: user.ID, Key: "sk-auth-frozen-balance-regression",
		Name: "Frozen balance auth", Status: service.StatusAPIKeyActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	for _, wallet := range []struct {
		name            string
		balance, frozen float64
	}{
		{"entire wallet frozen", 0, .1},
		{"partial reservation", .01342210, .08},
		{"settled reservation", .09149210, 0},
	} {
		t.Run(wallet.name, func(t *testing.T) {
			_, err := client.User.UpdateOneID(user.ID).
				SetBalance(wallet.balance).
				SetFrozenBalance(wallet.frozen).
				Save(ctx)
			require.NoError(t, err)

			got, err := repo.GetByKeyForAuth(ctx, key.Key)
			require.NoError(t, err)
			require.NotNil(t, got.User)
			require.Equal(t, wallet.balance, got.User.Balance, "auth must retain spendable balance separately")
			require.Equal(t, wallet.frozen, got.User.FrozenBalance, "auth must carry frozen funds so admission can wait instead of rejecting a temporarily empty wallet")
		})
	}
}
