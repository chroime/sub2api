package repository

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestGovernancePostgresConcurrentAdminEdit(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set isolated loopback governance fixture DSN")
	}
	u, e := url.Parse(dsn)
	require.NoError(t, e)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	db, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	defer db.Close()
	schema := fmt.Sprintf("governance_cas_%d", time.Now().UnixNano())
	_, e = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, e)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	fixture, e := sql.Open("postgres", u.String())
	require.NoError(t, e)
	defer fixture.Close()
	_, e = fixture.Exec(`CREATE TABLE accounts(id BIGINT PRIMARY KEY,name TEXT,platform TEXT,type TEXT,status TEXT,credentials JSONB,extra JSONB,proxy_id BIGINT,rate_multiplier DOUBLE PRECISION,parent_account_id BIGINT,deleted_at TIMESTAMPTZ);CREATE TABLE account_groups(account_id BIGINT,group_id BIGINT);INSERT INTO accounts VALUES(9,'import','openai','apikey','active','{"api_key":"old"}','{"upstream_governance_marker":"marker","unrelated":"preserved"}',NULL,2,NULL,NULL);INSERT INTO account_groups VALUES(9,3)`)
	require.NoError(t, e)
	rate := 2.0
	a := &service.Account{ID: 9, Name: "import", Platform: "openai", Type: "apikey", Status: "active", Credentials: map[string]any{"api_key": "old"}, Extra: map[string]any{"upstream_governance_marker": "marker", "unrelated": "preserved"}, GroupIDs: []int64{3}, RateMultiplier: &rate}
	expected := service.GovernanceAccountFingerprint(a)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, kind := range []string{"credentials", "groups"} {
		t.Run(kind, func(t *testing.T) {
			_, e = fixture.Exec(`UPDATE accounts SET credentials='{"api_key":"old"}';UPDATE account_groups SET group_id=3`)
			require.NoError(t, e)
			admin, e := fixture.BeginTx(ctx, nil)
			require.NoError(t, e)
			defer admin.Rollback()
			require.NoError(t, lockAccountForGroupBind(ctx, admin, 9))
			if kind == "credentials" {
				_, e = admin.ExecContext(ctx, `UPDATE accounts SET credentials='{"api_key":"admin-won"}' WHERE id=9`)
			} else {
				_, e = admin.ExecContext(ctx, `UPDATE account_groups SET group_id=5 WHERE account_id=9`)
			}
			require.NoError(t, e)
			done := make(chan error, 1)
			go func() {
				tx, err := fixture.BeginTx(ctx, nil)
				if err != nil {
					done <- err
					return
				}
				defer tx.Rollback()
				done <- checkGovernanceAccountCAS(ctx, tx, 9, expected)
			}()
			// The competing CAS must wait for the already-held row lock.
			select {
			case err := <-done:
				t.Fatalf("CAS bypassed active admin transaction: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			require.NoError(t, admin.Commit())
			require.ErrorIs(t, <-done, gov.ErrConflict)
			var key, extra string
			var group int64
			require.NoError(t, fixture.QueryRow(`SELECT credentials->>'api_key',extra->>'unrelated' FROM accounts WHERE id=9`).Scan(&key, &extra))
			require.NoError(t, fixture.QueryRow(`SELECT group_id FROM account_groups WHERE account_id=9`).Scan(&group))
			require.Equal(t, "preserved", extra)
			if kind == "credentials" {
				require.Equal(t, "admin-won", key)
				require.EqualValues(t, 3, group)
			} else {
				require.Equal(t, "old", key)
				require.EqualValues(t, 5, group)
			}
		})
	}
}

func TestGovernancePostgresAtomicCreateRollback(t *testing.T) {
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set isolated loopback governance fixture DSN")
	}
	u, e := url.Parse(dsn)
	require.NoError(t, e)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	db, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	defer db.Close()
	schema := fmt.Sprintf("governance_atomic_%d", time.Now().UnixNano())
	_, e = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, e)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	fixture, e := sql.Open("postgres", u.String())
	require.NoError(t, e)
	defer fixture.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, fixture)))
	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx))
	// scheduler_outbox is intentionally SQL-managed, outside Ent's schema.
	_, e = fixture.Exec(`CREATE TABLE scheduled_test_plans(account_id BIGINT); CREATE TABLE IF NOT EXISTS scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT NOT NULL,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT now()); CREATE UNIQUE INDEX fixture_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, e)
	g, e := client.Group.Create().SetName("fixture-group").SetPlatform("openai").Save(ctx)
	require.NoError(t, e)
	repo := newAccountRepositoryWithSQL(client, fixture, nil)
	a := &service.Account{Name: "import", Platform: "openai", Type: "apikey", Status: "active", Credentials: map[string]any{"api_key": "invented-key"}, Extra: map[string]any{"upstream_governance_marker": "marker"}, Concurrency: 1}
	// Force failure after account insertion, in the binding phase.
	_, e = fixture.Exec(`ALTER TABLE account_groups ADD CONSTRAINT fixture_reject_binding CHECK (priority <> 777)`)
	require.NoError(t, e)
	e = repo.CreateWithAccountGroups(ctx, a, []service.AccountGroup{{GroupID: g.ID, Priority: 777}})
	require.Error(t, e)
	var count int
	require.NoError(t, fixture.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, fixture.QueryRow(`SELECT count(*) FROM account_groups`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, fixture.QueryRow(`SELECT count(*) FROM scheduler_outbox`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, repo.CreateWithAccountGroups(ctx, a, []service.AccountGroup{{GroupID: g.ID, Priority: 1}}))
	require.NoError(t, fixture.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, fixture.QueryRow(`SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, a.ID, g.ID).Scan(&count))
	require.Equal(t, 1, count)
	// Deletion must wait on the parent before locking join rows. The old join-first
	// order deadlocked with a governance transaction holding the parent row.
	held, err := fixture.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer held.Rollback()
	require.NoError(t, lockAccountForGroupBind(ctx, held, a.ID))
	deleteCtx, cancelDelete := context.WithTimeout(ctx, 3*time.Second)
	defer cancelDelete()
	done := make(chan error, 1)
	go func() { done <- repo.Delete(deleteCtx, a.ID) }()
	select {
	case err := <-done:
		t.Fatalf("delete bypassed parent lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	rows, err := held.QueryContext(ctx, `SELECT account_id FROM account_groups WHERE account_id=$1 FOR UPDATE NOWAIT`, a.ID)
	require.NoError(t, err)
	require.NoError(t, rows.Close())
	require.NoError(t, held.Rollback())
	require.NoError(t, <-done)
}
