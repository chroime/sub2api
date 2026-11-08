//go:build unit

package repository

import (
	"context"
	"encoding/json"
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

func TestGroupInputRoutingPostgresMigrationRepositoryAndAuthProjection(t *testing.T) {
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
	legacy, err := client.Group.Create().SetName("legacy-input-routing").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().SetEmail("routing-repo@example.com").SetPasswordHash("isolated-placeholder").Save(ctx)
	require.NoError(t, err)
	_, err = client.APIKey.Create().SetUserID(user.ID).SetGroupID(legacy.ID).SetName("routing").SetKey("isolated-input-routing-key").Save(ctx)
	require.NoError(t, err)
	// Simulate an existing database before the new column, then apply twice.
	_, err = db.ExecContext(ctx, `ALTER TABLE groups DROP COLUMN input_routing`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("278_group_input_routing.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `CREATE TRIGGER trg_groups_auth_cache_invalidation
		AFTER UPDATE OR DELETE ON groups FOR EACH ROW EXECUTE FUNCTION enqueue_group_auth_cache_invalidation()`)
	require.NoError(t, err)
	var raw []byte
	require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id = $1`, legacy.ID).Scan(&raw))
	require.JSONEq(t, `{"enabled":false,"rules":[]}`, string(raw))

	repo := newGroupRepositoryWithSQL(client, db)
	keyRepo := NewAPIKeyRepository(client, db)
	group, err := repo.GetByIDLite(ctx, legacy.ID)
	require.NoError(t, err)
	config := service.GroupInputRoutingConfig{Enabled: true, Rules: []service.GroupInputRoutingRule{{ID: "small", Enabled: true, RequestType: "all", InputTokensMode: "range", MinInputTokens: 1000, MaxInputTokens: 2000, AccountIDs: []int64{11}, GroupIDs: []int64{21}, FallbackToOriginal: true}}}
	allInputs := service.CloneGroupInputRouting(config)
	allInputs.Rules[0].InputTokensMode = "all"
	for _, update := range []service.GroupInputRoutingConfig{config, allInputs, config, {Rules: []service.GroupInputRoutingRule{}}} {
		group.InputRouting = update
		require.NoError(t, repo.Update(ctx, group))
		loaded, err := repo.GetByIDLite(ctx, group.ID)
		require.NoError(t, err)
		require.Equal(t, update, loaded.InputRouting)
		key, err := keyRepo.GetByKeyForAuth(ctx, "isolated-input-routing-key")
		require.NoError(t, err)
		require.Equal(t, update, key.Group.InputRouting, "auth SELECT must project routing config")
		var invalidations int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&invalidations))
		require.Equal(t, 1, invalidations)
		_, err = db.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)
		require.NoError(t, repo.Update(ctx, group))
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&invalidations))
		require.Zero(t, invalidations, "unchanged routing must not invalidate again")
	}
	created := &service.Group{Name: "new-input-routing", Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1, InputRouting: config}
	require.NoError(t, repo.Create(ctx, created))
	loaded, err := repo.GetByIDLite(ctx, created.ID)
	require.NoError(t, err)
	expected, err := json.Marshal(config)
	require.NoError(t, err)
	raw, err = json.Marshal(loaded.InputRouting)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(raw))

	t.Run("inclusive ranges preserve legacy boundaries and invalidate auth once", func(t *testing.T) {
		legacyJSON := `{
			"enabled":true,"extension":{"keep":"source"},"rules":[
				{"id":"legacy","name":"Small","enabled":true,"request_type":"all","max_input_tokens":2000,"account_ids":[11,9],"group_ids":[21],"fallback_to_original":true,"extension":{"keep":true}},
				{"id":"bounded","enabled":true,"request_type":"sync","min_input_tokens":2000,"max_input_tokens":5000,"account_ids":[12]},
				{"id":"zero","enabled":false,"request_type":"stream","max_input_tokens":1,"group_ids":[22]},
				{"id":"explicit-zero","enabled":true,"request_type":"all","min_input_tokens":0,"max_input_tokens":2000,"account_ids":[13]}
			]}`
		migratedJSON := `{
			"enabled":true,"extension":{"keep":"source"},"rules":[
				{"id":"legacy","name":"Small","enabled":true,"request_type":"all","min_input_tokens":0,"max_input_tokens":1999,"account_ids":[11,9],"group_ids":[21],"fallback_to_original":true,"extension":{"keep":true}},
				{"id":"bounded","enabled":true,"request_type":"sync","min_input_tokens":2000,"max_input_tokens":5000,"account_ids":[12]},
				{"id":"zero","enabled":false,"request_type":"stream","min_input_tokens":0,"max_input_tokens":0,"group_ids":[22]},
				{"id":"explicit-zero","enabled":true,"request_type":"all","min_input_tokens":0,"max_input_tokens":2000,"account_ids":[13]}
			]}`
		_, err := db.ExecContext(ctx, `UPDATE groups SET input_routing=$1 WHERE id=$2`, legacyJSON, legacy.ID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE groups SET input_routing='{"enabled":false,"rules":[
			{"id":"disabled","enabled":false,"max_input_tokens":500,"account_ids":[14]},
			{"id":"invalid-zero","enabled":true,"max_input_tokens":0,"account_ids":[15]},
			{"id":"invalid-negative","enabled":true,"max_input_tokens":-1,"account_ids":[16]},
			{"id":"invalid-too-large","enabled":true,"max_input_tokens":10000001,"account_ids":[17]}
		]}'::jsonb WHERE id=$1`, created.ID)
		require.NoError(t, err)
		empty, err := client.Group.Create().SetName("empty-input-routing").SetPlatform(service.PlatformOpenAI).Save(ctx)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)

		var originalTrigger string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_functiondef('enqueue_group_auth_cache_invalidation()'::regprocedure)`).Scan(&originalTrigger))
		rangesMigration, err := dbmigrations.FS.ReadFile("279_group_input_routing_ranges.sql")
		require.NoError(t, err)
		for attempt := range 2 {
			result, err := db.ExecContext(ctx, string(rangesMigration))
			require.NoError(t, err)
			affected, err := result.RowsAffected()
			require.NoError(t, err)
			if attempt == 0 {
				require.EqualValues(t, 2, affected, "only groups containing legacy rules should be rewritten")
			} else {
				require.Zero(t, affected, "reapplying the migration must not update rows")
			}
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, legacy.ID).Scan(&raw))
			require.JSONEq(t, migratedJSON, string(raw), "rule order and all unrelated fields must survive migration")
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, created.ID).Scan(&raw))
			require.JSONEq(t, `{"enabled":false,"rules":[
				{"id":"disabled","enabled":false,"min_input_tokens":0,"max_input_tokens":499,"account_ids":[14]},
				{"id":"invalid-zero","enabled":true,"min_input_tokens":0,"max_input_tokens":-1,"account_ids":[15]},
				{"id":"invalid-negative","enabled":true,"min_input_tokens":0,"max_input_tokens":-1,"account_ids":[16]},
				{"id":"invalid-too-large","enabled":true,"min_input_tokens":0,"max_input_tokens":10000001,"account_ids":[17]}
			]}`, string(raw), "invalid legacy thresholds must remain invalid, matching the JSON compatibility decoder")
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, empty.ID).Scan(&raw))
			require.JSONEq(t, `{"enabled":false,"rules":[]}`, string(raw))
			key, err := keyRepo.GetByKeyForAuth(ctx, "isolated-input-routing-key")
			require.NoError(t, err)
			require.Equal(t, 0, key.Group.InputRouting.Rules[0].MinInputTokens)
			require.Equal(t, 1999, key.Group.InputRouting.Rules[0].MaxInputTokens, "auth projection must see the converted inclusive boundary")
			require.Equal(t, 2000, key.Group.InputRouting.Rules[1].MinInputTokens)
			require.Equal(t, 5000, key.Group.InputRouting.Rules[1].MaxInputTokens)
			var invalidations int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&invalidations))
			require.Equal(t, 1, invalidations, "the existing trigger should enqueue exactly one invalidation for the legacy key")
		}
		var currentTrigger string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_functiondef('enqueue_group_auth_cache_invalidation()'::regprocedure)`).Scan(&currentTrigger))
		require.Equal(t, originalTrigger, currentTrigger, "the range migration must reuse the existing auth invalidation trigger")
	})

	t.Run("token modes preserve JSON and invalidate auth only for changed groups", func(t *testing.T) {
		originalJSON := `{
			"enabled":true,"extension":{"keep":"source"},"rules":[
				{"id":"missing-mode","name":"Small","enabled":true,"request_type":"sync","min_input_tokens":20,"max_input_tokens":300,"account_ids":[11,9],"group_ids":[21],"fallback_to_original":true,"extension":{"keep":[1,2]}},
				{"id":"all","enabled":true,"request_type":"stream","input_tokens_mode":"all","group_ids":[22]},
				{"id":"range","enabled":false,"request_type":"all","input_tokens_mode":"range","min_input_tokens":50,"max_input_tokens":100,"account_ids":[12]},
				{"id":"legacy-boundary","enabled":false,"request_type":"all","max_input_tokens":2000,"account_ids":[13]}
			]}`
		migratedJSON := `{
			"enabled":true,"extension":{"keep":"source"},"rules":[
				{"id":"missing-mode","name":"Small","enabled":true,"request_type":"sync","input_tokens_mode":"range","min_input_tokens":20,"max_input_tokens":300,"account_ids":[11,9],"group_ids":[21],"fallback_to_original":true,"extension":{"keep":[1,2]}},
				{"id":"all","enabled":true,"request_type":"stream","input_tokens_mode":"all","group_ids":[22]},
				{"id":"range","enabled":false,"request_type":"all","input_tokens_mode":"range","min_input_tokens":50,"max_input_tokens":100,"account_ids":[12]},
				{"id":"legacy-boundary","enabled":false,"request_type":"all","input_tokens_mode":"range","max_input_tokens":2000,"account_ids":[13]}
			]}`
		_, err := db.ExecContext(ctx, `UPDATE groups SET input_routing=$1 WHERE id=$2`, originalJSON, legacy.ID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE groups SET input_routing='{"enabled":false,"rules":[
			{"id":"disabled","enabled":false,"min_input_tokens":0,"max_input_tokens":0,"account_ids":[14]},
			{"id":"existing-null","input_tokens_mode":null,"extension":"unchanged"},
			{"id":"existing-unknown","input_tokens_mode":"future-mode","extension":"unchanged"}
		]}'::jsonb WHERE id=$1`, created.ID)
		require.NoError(t, err)
		unchanged, err := client.Group.Create().SetName("existing-input-mode").SetPlatform(service.PlatformOpenAI).Save(ctx)
		require.NoError(t, err)
		unchangedJSON := `{"enabled":true,"rules":[{"id":"already-all","input_tokens_mode":"all","min_input_tokens":800,"max_input_tokens":100,"account_ids":[15]}]}`
		_, err = db.ExecContext(ctx, `UPDATE groups SET input_routing=$1 WHERE id=$2`, unchangedJSON, unchanged.ID)
		require.NoError(t, err)
		_, err = client.APIKey.Create().SetUserID(user.ID).SetGroupID(unchanged.ID).SetName("unchanged-mode").SetKey("isolated-existing-mode-key").Save(ctx)
		require.NoError(t, err)
		empty, err := client.Group.Create().SetName("empty-mode-routing").SetPlatform(service.PlatformOpenAI).Save(ctx)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
		require.NoError(t, err)

		var originalTrigger string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_functiondef('enqueue_group_auth_cache_invalidation()'::regprocedure)`).Scan(&originalTrigger))
		modeMigration, err := dbmigrations.FS.ReadFile("280_group_input_routing_tokens_mode.sql")
		require.NoError(t, err)
		for attempt := range 2 {
			result, err := db.ExecContext(ctx, string(modeMigration))
			require.NoError(t, err)
			affected, err := result.RowsAffected()
			require.NoError(t, err)
			if attempt == 0 {
				require.EqualValues(t, 2, affected, "only groups with missing modes should be rewritten")
			} else {
				require.Zero(t, affected, "reapplying the migration must not update rows")
			}
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, legacy.ID).Scan(&raw))
			require.JSONEq(t, migratedJSON, string(raw), "migration must preserve rule order, boundaries, and every existing field")
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, created.ID).Scan(&raw))
			require.JSONEq(t, `{"enabled":false,"rules":[
				{"id":"disabled","enabled":false,"input_tokens_mode":"range","min_input_tokens":0,"max_input_tokens":0,"account_ids":[14]},
				{"id":"existing-null","input_tokens_mode":null,"extension":"unchanged"},
				{"id":"existing-unknown","input_tokens_mode":"future-mode","extension":"unchanged"}
			]}`, string(raw), "only an absent field receives the default, including disabled rules")
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, unchanged.ID).Scan(&raw))
			require.JSONEq(t, unchangedJSON, string(raw))
			require.NoError(t, db.QueryRowContext(ctx, `SELECT input_routing FROM groups WHERE id=$1`, empty.ID).Scan(&raw))
			require.JSONEq(t, `{"enabled":false,"rules":[]}`, string(raw))
			key, err := keyRepo.GetByKeyForAuth(ctx, "isolated-input-routing-key")
			require.NoError(t, err)
			require.Equal(t, "range", key.Group.InputRouting.Rules[0].InputTokensMode)
			require.Equal(t, 20, key.Group.InputRouting.Rules[0].MinInputTokens)
			require.Equal(t, 300, key.Group.InputRouting.Rules[0].MaxInputTokens)
			require.Equal(t, "all", key.Group.InputRouting.Rules[1].InputTokensMode)
			require.Equal(t, 0, key.Group.InputRouting.Rules[1].MinInputTokens)
			require.Equal(t, 0, key.Group.InputRouting.Rules[1].MaxInputTokens)
			require.Equal(t, "range", key.Group.InputRouting.Rules[3].InputTokensMode)
			require.Equal(t, 1999, key.Group.InputRouting.Rules[3].MaxInputTokens, "JSON compatibility still converts a legacy max-only threshold")
			var invalidations int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&invalidations))
			require.Equal(t, 1, invalidations, "the changed group invalidates its key once; unchanged groups and reruns do not")
		}
		var currentTrigger string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_get_functiondef('enqueue_group_auth_cache_invalidation()'::regprocedure)`).Scan(&currentTrigger))
		require.Equal(t, originalTrigger, currentTrigger, "the mode migration must reuse the existing auth invalidation trigger")
	})
}
