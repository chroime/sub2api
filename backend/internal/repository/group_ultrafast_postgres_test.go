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

func TestGroupUltraFastPostgresMigrationRepositoryAndAuthProjection(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TABLE groups, api_keys, users CASCADE;
		CREATE TABLE scheduler_outbox (event_type TEXT, account_id BIGINT, group_id BIGINT, payload JSONB, dedup_key TEXT UNIQUE);
		CREATE TABLE auth_cache_invalidation_outbox (cache_key TEXT NOT NULL);`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, entmigrate.Create(ctx, client.Schema,
		[]*schema.Table{entmigrate.GroupsTable, entmigrate.UsersTable, entmigrate.APIKeysTable, entmigrate.UserAllowedGroupsTable},
		entmigrate.WithForeignKeys(false)))
	legacy, err := client.Group.Create().SetName("legacy-ultrafast").SetPlatform(service.PlatformOpenAI).SetForceOpenaiFast(true).SetFreeOpenaiFast(true).Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().SetEmail("ultrafast-repo@example.com").SetPasswordHash("isolated-placeholder").Save(ctx)
	require.NoError(t, err)
	_, err = client.APIKey.Create().SetUserID(user.ID).SetGroupID(legacy.ID).SetName("ultrafast").SetKey("isolated-ultrafast-key").Save(ctx)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE groups DROP COLUMN force_openai_ultrafast`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("281_group_force_openai_ultrafast.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, "migration must preserve existing data when reapplied")
	}
	_, err = db.ExecContext(ctx, `CREATE TRIGGER trg_groups_auth_cache_invalidation
		AFTER UPDATE OR DELETE ON groups FOR EACH ROW EXECUTE FUNCTION enqueue_group_auth_cache_invalidation()`)
	require.NoError(t, err)
	var ultra, fast, free bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT force_openai_ultrafast, force_openai_fast, free_openai_fast FROM groups WHERE id=$1`, legacy.ID).Scan(&ultra, &fast, &free))
	require.False(t, ultra, "legacy groups must default to UltraFast off")
	require.True(t, fast, "migration must retain the existing Fast policy")
	require.True(t, free, "migration must retain the existing Fast billing policy")
	_, err = db.ExecContext(ctx, `UPDATE groups SET force_openai_ultrafast=NULL WHERE id=$1`, legacy.ID)
	require.Error(t, err, "the new column is not nullable")

	repo := newGroupRepositoryWithSQL(client, db)
	keyRepo := NewAPIKeyRepository(client, db)
	group, err := repo.GetByIDLite(ctx, legacy.ID)
	require.NoError(t, err)
	require.False(t, group.ForceOpenAIUltrafast)
	assertInvalidations := func(want int) {
		t.Helper()
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
		require.Equal(t, want, count)
		_, err := db.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)
	}
	for _, enabled := range []bool{true, false, true} {
		group.ForceOpenAIUltrafast = enabled
		require.NoError(t, repo.Update(ctx, group))
		loaded, err := repo.GetByIDLite(ctx, group.ID)
		require.NoError(t, err)
		require.Equal(t, enabled, loaded.ForceOpenAIUltrafast)
		key, err := keyRepo.GetByKeyForAuth(ctx, "isolated-ultrafast-key")
		require.NoError(t, err)
		require.Equal(t, enabled, key.Group.ForceOpenAIUltrafast, "auth SELECT must project the persisted UltraFast flag")
		assertInvalidations(1)
		require.NoError(t, repo.Update(ctx, group))
		assertInvalidations(0)
	}
	_, err = db.ExecContext(ctx, `UPDATE groups SET force_openai_fast=false WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	assertInvalidations(1)
	_, err = db.ExecContext(ctx, `UPDATE groups SET free_openai_fast=false WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	assertInvalidations(1)
	_, err = db.ExecContext(ctx, `UPDATE groups SET name='renamed-ultrafast' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	assertInvalidations(0)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT force_openai_ultrafast FROM groups WHERE id=$1`, legacy.ID).Scan(&ultra))
	require.True(t, ultra, "reapplying migration must not reset an enabled policy")
	assertInvalidations(0)

	for _, enabled := range []bool{false, true} {
		created := &service.Group{
			Name: "created-ultrafast", Platform: service.PlatformOpenAI, Status: service.StatusActive,
			SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1, ForceOpenAIUltrafast: enabled,
		}
		if enabled {
			created.Name = "created-ultrafast-enabled"
		}
		require.NoError(t, repo.Create(ctx, created))
		loaded, err := repo.GetByIDLite(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, enabled, loaded.ForceOpenAIUltrafast, "repository creation must persist the requested state")
	}
}
