package upstreamgovernance

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSQLSecondsMigrationBackfillsAndPreservesSchedules(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOVERNANCE_STORE_TEST_DSN to an isolated loopback PostgreSQL fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer base.Close()
	schema := fmt.Sprintf("governance_seconds_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY); CREATE TABLE groups(id BIGSERIAL PRIMARY KEY); CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,extra JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ); INSERT INTO groups(id) VALUES(1)`)
	require.NoError(t, err)
	apply := func(name string) {
		raw, readErr := os.ReadFile("../../migrations/" + name)
		require.NoError(t, readErr)
		_, execErr := db.Exec(string(raw))
		require.NoError(t, execErr)
	}
	apply("247_upstream_governance.sql")
	apply("249_upstream_governance_balance_monitor.sql")
	apply("251_upstream_governance_login_credentials.sql")
	apply("261_upstream_governance_seconds_observation.sql")
	var siteID int64
	next := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	require.NoError(t, db.QueryRow(`INSERT INTO upstream_governance_sites(name,platform,base_url,interval_minutes,next_sync_at,session_cipher) VALUES('Seconds','sub2api','https://fixture.example',$1,$2,'fixture') RETURNING id`, maxIntervalMinutes, next).Scan(&siteID))
	var fast, full int64
	var due time.Time
	require.NoError(t, db.QueryRow(`SELECT fast_interval_seconds,full_interval_seconds,next_fast_observe_at FROM upstream_governance_sites WHERE id=$1`, siteID).Scan(&fast, &full, &due))
	require.Equal(t, int64(maxIntervalMinutes)*60, fast)
	require.Equal(t, int64(maxIntervalMinutes)*60, full)
	require.Equal(t, next, due)
	got, err := NewSQLStore(db).GetSite(t.Context(), siteID)
	require.NoError(t, err)
	require.Equal(t, fast, got.FastIntervalSeconds)
	require.Equal(t, full, got.FullIntervalSeconds)
	require.Equal(t, next, got.NextFastObserveAt)
}
