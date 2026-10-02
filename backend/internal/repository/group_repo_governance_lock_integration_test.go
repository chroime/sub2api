package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func governanceGroupLockFixture(t *testing.T) (*sql.DB, *dbent.Client, *groupRepository, *service.Account, int64) {
	t.Helper()
	dsn := os.Getenv("GOVERNANCE_STORE_TEST_DSN")
	if dsn == "" {
		t.Skip("set isolated loopback governance fixture DSN")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.NotNil(t, u.User)
	require.Equal(t, "governance_fixture", u.User.Username())
	require.Equal(t, "/postgres", u.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("governance_group_lock_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = db.Close()
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	fixture, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fixture.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, fixture)))
	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx))
	_, err = fixture.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT NOT NULL,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT now()); CREATE UNIQUE INDEX fixture_group_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	g, err := client.Group.Create().SetName("fixture-group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	account := &service.Account{Name: "import", Platform: "openai", Type: "apikey", Status: "active", Credentials: map[string]any{"api_key": "invented-key"}, Extra: map[string]any{"upstream_governance_marker": "marker"}, Concurrency: 1}
	accountRepo := newAccountRepositoryWithSQL(client, fixture, nil)
	require.NoError(t, accountRepo.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: g.ID, Priority: 7}}))
	account, err = accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	return fixture, client, newGroupRepositoryWithSQL(client, fixture), account, g.ID
}

func governanceWaitForBlockedWriter(t *testing.T, ctx context.Context, db *sql.DB, blocker int, done <-chan error) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("membership writer completed before account lock released: %v", err)
		case <-timer.C:
			t.Fatal("membership writer did not wait for the account lock")
		case <-ticker.C:
			var waiting bool
			err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting)
			if err == nil && waiting {
				return
			}
		}
	}
}

func TestGovernanceGroupWritersLockAccountsBeforeGroups(t *testing.T) {
	for _, operation := range []string{"clear", "cascade", "bind", "duplicate"} {
		t.Run(operation, func(t *testing.T) {
			db, client, repo, account, groupID := governanceGroupLockFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := client.Tx(ctx)
			require.NoError(t, err)
			defer tx.Rollback()
			require.NoError(t, checkGovernanceAccountCAS(ctx, tx.Client(), account.ID, service.GovernanceAccountFingerprint(account)))
			var blocker int
			require.NoError(t, scanSingleRow(ctx, tx.Client(), `SELECT pg_backend_pid()`, nil, &blocker))
			done := make(chan error, 1)
			copy := &service.Group{Name: "copy", Platform: "openai", Status: "active", SubscriptionType: "standard"}
			go func() {
				var err error
				switch operation {
				case "clear":
					_, err = repo.DeleteAccountGroupsByGroupID(ctx, groupID)
				case "cascade":
					_, err = repo.DeleteCascade(ctx, groupID)
				case "bind":
					err = repo.BindAccountsToGroup(ctx, groupID, []int64{account.ID})
				case "duplicate":
					err = repo.CreateFromSource(ctx, copy, groupID)
				}
				done <- err
			}()
			governanceWaitForBlockedWriter(t, ctx, db, blocker, done)
			// A writer waiting for the account must not already own a group lock.
			probe, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			var id int64
			require.NoError(t, probe.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=$1 FOR UPDATE NOWAIT`, groupID).Scan(&id))
			require.NoError(t, probe.Rollback())
			require.NoError(t, replaceAccountGroupsInTransaction(ctx, tx.Client(), account.ID, []int64{groupID}))
			require.NoError(t, tx.Commit())
			require.NoError(t, <-done)
			var count int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, account.ID, groupID).Scan(&count))
			if operation == "clear" || operation == "cascade" {
				require.Zero(t, count, "group edit after the governance write must remain applied")
			} else {
				require.Equal(t, 1, count)
			}
			if operation == "duplicate" {
				var priority int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT priority FROM account_groups WHERE account_id=$1 AND group_id=$2`, account.ID, copy.ID).Scan(&priority))
				require.Equal(t, 1, priority, "copy must use the source priority committed by the preceding account writer")
			}
		})
	}
}

func TestGovernanceGroupClearMakesWaitingPreviewStale(t *testing.T) {
	db, client, _, account, groupID := governanceGroupLockFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	groupTx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer groupTx.Rollback()
	_, err = newGroupRepositoryWithSQL(groupTx.Client(), groupTx).DeleteAccountGroupsByGroupID(ctx, groupID)
	require.NoError(t, err)
	var blocker int
	require.NoError(t, scanSingleRow(ctx, groupTx.Client(), `SELECT pg_backend_pid()`, nil, &blocker))
	done := make(chan error, 1)
	go func() {
		tx, err := client.Tx(ctx)
		if err == nil {
			defer tx.Rollback()
			err = checkGovernanceAccountCAS(ctx, tx.Client(), account.ID, service.GovernanceAccountFingerprint(account))
		}
		done <- err
	}()
	governanceWaitForBlockedWriter(t, ctx, db, blocker, done)
	require.NoError(t, groupTx.Commit())
	require.ErrorIs(t, <-done, gov.ErrConflict)
}

func TestGovernanceGroupMembershipGrowthReturnsConflict(t *testing.T) {
	db, client, repo, account, groupID := governanceGroupLockFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, lockAccountForGroupBind(ctx, tx.Client(), account.ID))
	var blocker int
	require.NoError(t, scanSingleRow(ctx, tx.Client(), `SELECT pg_backend_pid()`, nil, &blocker))
	done := make(chan error, 1)
	go func() {
		_, err := repo.DeleteAccountGroupsByGroupID(ctx, groupID)
		done <- err
	}()
	governanceWaitForBlockedWriter(t, ctx, db, blocker, done)
	newAccount, err := client.Account.Create().SetName("new-member").SetPlatform("openai").SetType("apikey").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.BindAccountsToGroup(ctx, groupID, []int64{newAccount.ID}))
	require.NoError(t, tx.Commit())
	err = <-done
	require.Equal(t, http.StatusConflict, infraerrors.Code(err))
	require.Equal(t, "GROUP_MEMBERSHIP_CHANGED", infraerrors.Reason(err))
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM account_groups WHERE group_id=$1`, groupID).Scan(&count))
	require.Equal(t, 2, count, "conflicting clear must leave both memberships intact")
}
