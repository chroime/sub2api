//go:build unit

package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	entmigrate "github.com/Wei-Shaw/sub2api/ent/migrate"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestGroupIdentityMigrationLeavesLegacyGroupsUnconfigured(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	migration, err := dbmigrations.FS.ReadFile("289_group_identity.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration may be replayed without changing identities")
	var name, identity string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT name,identity::text FROM groups WHERE id=1`).Scan(&name, &identity))
	require.Equal(t, "wallet-test-group", name)
	require.JSONEq(t, `{}`, identity)
	_, err = db.ExecContext(ctx, `INSERT INTO groups(id,name) VALUES(3,'官方分组')`)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT identity::text FROM groups WHERE id=3`).Scan(&identity))
	require.JSONEq(t, `{}`, identity, "new rows with no identity also stay unconfigured")
	_, err = db.ExecContext(ctx, `UPDATE groups SET identity=NULL WHERE id=3`)
	require.Error(t, err, "the display identity must remain non-null")
}

func TestGroupIdentityRepositoryRoundTripAndNestedProjections(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TABLE groups,api_keys,users CASCADE;
		CREATE TABLE scheduler_outbox(event_type TEXT,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT UNIQUE);`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, entmigrate.Create(ctx, client.Schema,
		[]*schema.Table{entmigrate.GroupsTable, entmigrate.UsersTable, entmigrate.APIKeysTable,
			entmigrate.UserAllowedGroupsTable, entmigrate.ProxiesTable, entmigrate.AccountsTable, entmigrate.AccountGroupsTable},
		entmigrate.WithForeignKeys(false)))
	user, err := client.User.Create().SetEmail("identity-payer@example.test").SetPasswordHash("isolated-placeholder").Save(ctx)
	require.NoError(t, err)
	groupRepo := newGroupRepositoryWithSQL(client, db)
	group := &service.Group{Name: "原分组", Platform: service.PlatformGrok, RateMultiplier: 1,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, IsExclusive: true,
		Identity: service.GroupIdentity{Label: "官方", Color: "teal"}}
	require.NoError(t, groupRepo.Create(ctx, group))
	account, err := client.Account.Create().SetName("identity-account").SetPlatform(service.PlatformGrok).SetType("api_key").Save(ctx)
	require.NoError(t, err)
	_, err = client.AccountGroup.Create().SetAccountID(account.ID).SetGroupID(group.ID).SetPriority(3).Save(ctx)
	require.NoError(t, err)
	key, err := client.APIKey.Create().SetUserID(user.ID).SetGroupID(group.ID).SetName("identity-key").SetKey("isolated-identity-key").Save(ctx)
	require.NoError(t, err)
	apiKeyRepo := NewAPIKeyRepository(client, db)
	accountRepo := newAccountRepositoryWithSQL(client, db, nil)
	for _, identity := range []service.GroupIdentity{{Label: "官方", Color: "teal"}, {Label: "自定义", Color: "#a1b2c3"}, {}} {
		group.Identity = identity
		require.NoError(t, groupRepo.Update(ctx, group))
		full, err := groupRepo.GetByID(ctx, group.ID)
		require.NoError(t, err)
		require.Equal(t, identity, full.Identity)
		lite, err := groupRepo.GetByIDLite(ctx, group.ID)
		require.NoError(t, err)
		require.Equal(t, identity, lite.Identity)
		require.Equal(t, "原分组", lite.Name)
		require.Equal(t, service.PlatformGrok, lite.Platform)
		require.True(t, lite.IsExclusive)
		auth, err := apiKeyRepo.GetByKeyForAuth(ctx, "isolated-identity-key")
		require.NoError(t, err)
		require.NotNil(t, auth.Group)
		require.Equal(t, identity, auth.Group.Identity)
		keyDetails, err := apiKeyRepo.GetByID(ctx, key.ID)
		require.NoError(t, err)
		require.Equal(t, identity, keyDetails.Group.Identity)
		loadedAccount, err := accountRepo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Len(t, loadedAccount.Groups, 1)
		require.Equal(t, identity, loadedAccount.Groups[0].Identity)
		require.Len(t, loadedAccount.AccountGroups, 1)
		require.Equal(t, identity, loadedAccount.AccountGroups[0].Group.Identity)
	}
	var persisted string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT identity::text FROM groups WHERE id=$1`, group.ID).Scan(&persisted))
	require.JSONEq(t, `{}`, persisted, "clearing identity persists an empty object rather than JSON null")
}
