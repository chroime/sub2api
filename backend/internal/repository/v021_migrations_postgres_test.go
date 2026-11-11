//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The external runner restores the previous complete schema/data into a fresh
// disposable database. These tests must never upgrade a developer/live database.
func TestV021MigrationsUpgradePreviousCompleteDatabase(t *testing.T) {
	dsn := os.Getenv("SUB2API_MERGE_TEST_DSN")
	if dsn == "" {
		t.Skip("requires a restored disposable SUB2API_MERGE_TEST_DSN")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("a PostgreSQL URL for an isolated fixture is required; connection details withheld")
	}
	if u.Hostname() != "localhost" {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			t.Fatal("only a loopback PostgreSQL fixture is allowed")
		}
	}
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "sub2api_mergetest_") {
		t.Fatal("the fixture database must use the sub2api_mergetest_ prefix")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("could not open the fixture; connection details withheld")
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	var database, serverAddress, schema string
	if err := db.QueryRowContext(ctx, `SELECT current_database(),host(inet_server_addr()),current_schema()`).Scan(&database, &serverAddress, &schema); err != nil {
		t.Fatal("could not verify the isolated fixture identity; connection details withheld")
	}
	require.True(t, strings.HasPrefix(database, "sub2api_mergetest_"), "refusing a non-fixture database")
	serverIP := net.ParseIP(serverAddress)
	require.NotNil(t, serverIP, "the fixture must use a loopback TCP connection")
	require.True(t, serverIP.IsLoopback(), "refusing a non-loopback database server")
	require.Equal(t, "public", schema, "the complete fixture must use the public schema")

	beforeMigrations := v021MigrationChecksums(t, ctx, db)
	require.Contains(t, beforeMigrations, "287_user_notification_record_event_type.sql", "restore the current complete pre-upgrade database first")
	require.NotContains(t, beforeMigrations, "249_add_usage_logs_api_key_cost_index_notx.sql", "fixture was already upgraded")
	require.NotContains(t, beforeMigrations, "250_openai_cache_write_ttl.sql", "fixture was already upgraded")
	beforeData := v021PublicDataFingerprints(t, ctx, db)
	for _, table := range []string{"settings", "users", "usage_logs", "payment_orders", "payment_bill_receipts", "payment_bill_claims", "payment_bill_amount_reservations", "user_balance_notification_records"} {
		require.Contains(t, beforeData, table, "the restored fixture must include local payment and email features")
	}
	for _, column := range []string{"cache_creation_30m_tokens", "cache_creation_12h_tokens"} {
		require.False(t, v021ColumnExists(t, ctx, db, "usage_logs", column), "old-version fixture must not already have the new TTL fields")
	}

	// New filenames numbered 249/250 must run even though this database already
	// records the local extensions through migration 287. Reapplication must be a
	// no-op, including the nontransactional concurrent-index migration.
	require.NoError(t, ApplyMigrations(ctx, db))
	firstMigrations := v021MigrationChecksums(t, ctx, db)
	require.Len(t, firstMigrations, len(beforeMigrations)+2)
	for name, checksum := range beforeMigrations {
		require.Equal(t, checksum, firstMigrations[name], "an existing migration checksum changed: %s", name)
	}
	require.Contains(t, firstMigrations, "249_add_usage_logs_api_key_cost_index_notx.sql")
	require.Contains(t, firstMigrations, "250_openai_cache_write_ttl.sql")
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, firstMigrations, v021MigrationChecksums(t, ctx, db), "second upgrade run must not change migration state")

	var valid, ready bool
	var indexDefinition string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid)
		FROM pg_index i JOIN pg_class idx ON idx.oid=i.indexrelid
		JOIN pg_namespace ns ON ns.oid=idx.relnamespace
		WHERE ns.nspname='public' AND idx.relname='idx_usage_logs_api_key_created_cost'`).Scan(&valid, &ready, &indexDefinition))
	require.True(t, valid)
	require.True(t, ready)
	require.Contains(t, indexDefinition, "(api_key_id, created_at)")
	require.Contains(t, indexDefinition, "INCLUDE (actual_cost)")

	var nonzeroOldTokens int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM usage_logs WHERE cache_creation_30m_tokens<>0 OR cache_creation_12h_tokens<>0`).Scan(&nonzeroOldTokens))
	require.Zero(t, nonzeroOldTokens, "old usage must not be retroactively attributed to either cache TTL")
	for _, column := range []string{"cache_creation_30m_tokens", "cache_creation_12h_tokens"} {
		var nullable, dataType, defaultValue string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT is_nullable,data_type,column_default FROM information_schema.columns
			WHERE table_schema='public' AND table_name='usage_logs' AND column_name=$1`, column).Scan(&nullable, &dataType, &defaultValue))
		require.Equal(t, "NO", nullable)
		require.Equal(t, "integer", dataType)
		require.Contains(t, defaultValue, "0")
	}
	for _, table := range v021PricingTables {
		var nullable, dataType string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT is_nullable,data_type FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name='cache_write_12h_price'`, table).Scan(&nullable, &dataType))
		require.Equal(t, "YES", nullable)
		require.Equal(t, "numeric", dataType)
		var newPrices int64
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM public.`+pq.QuoteIdentifier(table)+` WHERE cache_write_12h_price IS NOT NULL`).Scan(&newPrices))
		require.Zero(t, newPrices, "existing pricing must retain its generic cache-write price: %s", table)
	}
	require.Equal(t, beforeData, v021PublicDataFingerprints(t, ctx, db), "migration changed existing row data/counts; only fingerprints are compared to protect restored private values")
	v021CacheUsageRoundTrip(t, ctx, db)
}

func v021CacheUsageRoundTrip(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var userID, keyID, accountID int64
	err := db.QueryRowContext(ctx, `SELECT u.user_id,u.api_key_id,u.account_id FROM usage_logs u
		JOIN users usr ON usr.id=u.user_id JOIN api_keys k ON k.id=u.api_key_id
		JOIN accounts a ON a.id=u.account_id LIMIT 1`).Scan(&userID, &keyID, &accountID)
	if err == sql.ErrNoRows {
		t.Log("restored fixture has no complete usage references for the optional insert round trip")
		return
	}
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	log := &service.UsageLog{
		UserID: userID, APIKeyID: keyID, AccountID: accountID,
		RequestID: uuid.NewString(), Model: "v021-isolated-usage-fixture", InputTokens: 3, OutputTokens: 5,
		CacheCreationTokens: 30, CacheCreation30mTokens: 10, CacheCreation12hTokens: 20,
		UpstreamInputTokens: 103, UpstreamOutputTokens: 105,
		UpstreamCacheReadTokens: 11, UpstreamCacheCreationTokens: 31,
		UpstreamCacheCreation5mTokens: 13, UpstreamCacheCreation1hTokens: 17,
		CreatedAt: time.Now().UTC(),
	}
	repo := &usageLogRepository{}
	inserted, err := repo.createSingle(ctx, tx, log)
	require.NoError(t, err)
	require.True(t, inserted)
	stored, err := scanUsageLog(tx.QueryRowContext(ctx, `SELECT `+usageLogSelectColumns+` FROM usage_logs WHERE id=$1`, log.ID))
	require.NoError(t, err)
	require.Equal(t, 10, stored.CacheCreation30mTokens)
	require.Equal(t, 20, stored.CacheCreation12hTokens)
	require.Equal(t, 11, stored.UpstreamCacheReadTokens)
	require.Equal(t, 31, stored.UpstreamCacheCreationTokens)
	require.Equal(t, 13, stored.UpstreamCacheCreation5mTokens)
	require.Equal(t, 17, stored.UpstreamCacheCreation1hTokens)
	require.Equal(t, 103, stored.UpstreamInputTokens)
	require.Equal(t, 105, stored.UpstreamOutputTokens)
	require.NoError(t, tx.Rollback())
}

var v021PricingTables = []string{
	"channel_model_pricing", "channel_pricing_intervals", "channel_account_stats_model_pricing", "channel_account_stats_pricing_intervals",
}

type v021TableFingerprint struct {
	Rows   int64
	Digest string
}

// Compute hashes in PostgreSQL, so test failure output never contains restored
// credentials, email bodies, accounts, balances or other private row contents.
func v021PublicDataFingerprints(t *testing.T, ctx context.Context, db *sql.DB) map[string]v021TableFingerprint {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	require.NoError(t, err)
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'schema_migrations' ORDER BY tablename`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	result := make(map[string]v021TableFingerprint, len(tables))
	for _, table := range tables {
		jsonExpression := `to_jsonb(t)`
		if table == "usage_logs" {
			jsonExpression += `-'cache_creation_30m_tokens'-'cache_creation_12h_tokens'`
		}
		for _, pricing := range v021PricingTables {
			if table == pricing {
				jsonExpression += `-'cache_write_12h_price'`
			}
		}
		query := `SELECT count(*),md5(COALESCE(string_agg(row_hash,'' ORDER BY row_hash),'')) FROM
			(SELECT md5((` + jsonExpression + `)::text) AS row_hash FROM public.` + pq.QuoteIdentifier(table) + ` t) hashed`
		var fingerprint v021TableFingerprint
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&fingerprint.Rows, &fingerprint.Digest), "fingerprint table %s", table)
		result[table] = fingerprint
	}
	require.NoError(t, tx.Commit())
	return result
}

func v021MigrationChecksums(t *testing.T, ctx context.Context, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT filename,checksum FROM public.schema_migrations ORDER BY filename`)
	require.NoError(t, err)
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var name, checksum string
		require.NoError(t, rows.Scan(&name, &checksum))
		result[name] = checksum
	}
	require.NoError(t, rows.Err())
	return result
}

func v021ColumnExists(t *testing.T, ctx context.Context, db *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, table, column).Scan(&exists))
	return exists
}
