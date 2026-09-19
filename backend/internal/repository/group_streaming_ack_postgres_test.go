//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
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

func TestGroupStreamingACKMigrationPreservesLegacyAndInvalidatesChanges(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		ALTER TABLE groups ADD COLUMN status TEXT DEFAULT 'active',
			ADD COLUMN is_exclusive BOOLEAN DEFAULT FALSE,
			ADD COLUMN allow_image_generation BOOLEAN DEFAULT FALSE,
			ADD COLUMN platform TEXT DEFAULT 'anthropic',
			ADD COLUMN subscription_type TEXT DEFAULT 'standard',
			ADD COLUMN rate_multiplier NUMERIC DEFAULT 1,
			ADD COLUMN peak_rate_enabled BOOLEAN DEFAULT FALSE,
			ADD COLUMN peak_start TEXT DEFAULT '', ADD COLUMN peak_end TEXT DEFAULT '',
			ADD COLUMN peak_rate_multiplier NUMERIC DEFAULT 1,
			ADD COLUMN profit_control_enabled BOOLEAN DEFAULT FALSE,
			ADD COLUMN profit_min_margin NUMERIC DEFAULT 0,
			ADD COLUMN profit_safety_buffer NUMERIC DEFAULT 0,
			ADD COLUMN deleted_at TIMESTAMPTZ;
		ALTER TABLE api_keys ADD COLUMN key TEXT DEFAULT '';
		UPDATE api_keys SET key = 'isolated-ack-test-key' WHERE id = 1;
		CREATE TABLE auth_cache_invalidation_outbox (cache_key TEXT NOT NULL);
	`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("245_group_streaming_ack.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must be idempotent")
	_, err = db.ExecContext(ctx, `CREATE TRIGGER trg_groups_auth_cache_invalidation
		AFTER UPDATE OR DELETE ON groups FOR EACH ROW
		EXECUTE FUNCTION enqueue_group_auth_cache_invalidation()`)
	require.NoError(t, err)

	var legacy sql.NullBool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT streaming_ack_enabled FROM groups WHERE id = 1`).Scan(&legacy))
	require.False(t, legacy.Valid, "existing groups must preserve the legacy account policy")
	_, err = db.ExecContext(ctx, `UPDATE groups SET name = 'renamed' WHERE id = 1`)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
	require.Zero(t, count, "unrelated updates must keep the existing invalidation contract")

	for _, value := range []any{false, true, nil} {
		_, err = db.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE groups SET streaming_ack_enabled = $1 WHERE id = 1`, value)
		require.NoError(t, err)
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
		require.Equal(t, 1, count, "every tri-state transition must invalidate auth snapshots")
		_, err = db.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE groups SET streaming_ack_enabled = $1 WHERE id = 1`, value)
		require.NoError(t, err)
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
		require.Zero(t, count, "an unchanged policy must not enqueue another invalidation")
	}
}

func TestGroupStreamingACKRepositoryRoundTripAndAuthProjection(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TABLE groups, api_keys, users CASCADE;
		CREATE TABLE scheduler_outbox (event_type TEXT, account_id BIGINT, group_id BIGINT, payload JSONB, dedup_key TEXT UNIQUE);`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, entmigrate.Create(ctx, client.Schema,
		[]*schema.Table{entmigrate.GroupsTable, entmigrate.UsersTable, entmigrate.APIKeysTable, entmigrate.UserAllowedGroupsTable},
		entmigrate.WithForeignKeys(false)))
	user, err := client.User.Create().SetEmail("ack-repo@example.com").SetPasswordHash("isolated-placeholder").Save(ctx)
	require.NoError(t, err)
	repo := newGroupRepositoryWithSQL(client, db)
	apiKeyRepo := NewAPIKeyRepository(client, db)
	disabled, enabled := false, true
	for index, policy := range []*bool{nil, &disabled, &enabled} {
		group := &service.Group{
			Name: fmt.Sprintf("ack-policy-%d", index), Platform: service.PlatformAnthropic,
			Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard,
			RateMultiplier: 1, StreamingACKEnabled: policy,
		}
		require.NoError(t, repo.Create(ctx, group))
		got, err := repo.GetByIDLite(ctx, group.ID)
		require.NoError(t, err)
		require.Equal(t, policy, got.StreamingACKEnabled, "repository creation must preserve all three policy states")
		key := fmt.Sprintf("isolated-ack-key-%d", index)
		_, err = client.APIKey.Create().SetUserID(user.ID).SetGroupID(group.ID).SetName("ack").SetKey(key).Save(ctx)
		require.NoError(t, err)
		for _, update := range []*bool{&enabled, &disabled, nil} {
			group.StreamingACKEnabled = update
			require.NoError(t, repo.Update(ctx, group))
			loaded, err := repo.GetByIDLite(ctx, group.ID)
			require.NoError(t, err)
			require.Equal(t, update, loaded.StreamingACKEnabled, "repository updates must set and clear the nullable policy")
			apiKey, err := apiKeyRepo.GetByKeyForAuth(ctx, key)
			require.NoError(t, err)
			require.NotNil(t, apiKey.Group)
			require.Equal(t, update, apiKey.Group.StreamingACKEnabled, "auth projection must select the persisted policy")
		}
	}
}

func TestGroupStreamingACKSimpleModeDefaultsDisableOnlyNewGroups(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TABLE groups CASCADE`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, entmigrate.Create(ctx, client.Schema, []*schema.Table{entmigrate.GroupsTable}))
	legacy, err := client.Group.Create().SetName("anthropic-default").SetPlatform(service.PlatformAnthropic).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, ensureSimpleModeDefaultGroups(ctx, client))
	groups, err := client.Group.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 6)
	for _, group := range groups {
		if group.ID == legacy.ID {
			require.Nil(t, group.StreamingAckEnabled, "an existing default group must retain its legacy policy")
		} else {
			require.NotNil(t, group.StreamingAckEnabled, group.Name)
			require.False(t, *group.StreamingAckEnabled, "new automatic groups must start with ACK disabled")
		}
	}
}
