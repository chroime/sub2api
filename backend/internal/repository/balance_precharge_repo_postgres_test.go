//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// These tests run real SQL without the Docker/Redis integration TestMain. Supply
// SUB2API_TEST_POSTGRES_DSN as a PostgreSQL URL; all writes are confined to a fresh
// schema with no public search-path fallback, and the schema is removed afterward.
func newBalancePrechargeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SUB2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SUB2API_TEST_POSTGRES_DSN to run isolated PostgreSQL balance-precharge tests")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	schema := "precharge_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE SCHEMA "` + schema + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
		require.NoError(t, cleanupErr)
	})
	params := parsed.Query()
	params.Set("search_path", schema)
	params.Set("statement_timeout", "10000")
	parsed.RawQuery = params.Encode()
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(32)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE users (
			id BIGINT PRIMARY KEY, balance NUMERIC(20,8) NOT NULL,
			email TEXT NOT NULL DEFAULT 'wallet-test@example.com',
			frozen_balance NUMERIC(20,8) NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ
		);
		CREATE TABLE api_keys (
			id BIGINT PRIMARY KEY, user_id BIGINT NOT NULL, group_id BIGINT,
			quota NUMERIC(20,8) NOT NULL DEFAULT 0, quota_used NUMERIC(20,8) NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active', expires_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ
		);
		CREATE TABLE groups (id BIGINT PRIMARY KEY, name TEXT NOT NULL DEFAULT 'wallet-test-group');
		INSERT INTO users (id, balance) VALUES (1, 5), (2, 5);
		INSERT INTO groups (id) VALUES (1), (2);
		INSERT INTO api_keys (id,user_id,group_id) VALUES (1,1,1), (2,1,2), (3,2,1);
	`)
	require.NoError(t, err)
	for _, name := range []string{"071_add_usage_billing_dedup.sql", "073_add_usage_billing_dedup_archive.sql", "240_add_balance_precharges.sql", "241_add_balance_precharge_reviews.sql", "242_add_balance_precharge_recovery.sql", "244_add_balance_precharge_archive.sql"} {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, readErr)
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	return db
}

func balancePrechargeRepo(t *testing.T, db *sql.DB) service.BalancePrechargeRepository {
	t.Helper()
	repo, ok := NewUsageBillingRepository(nil, db).(service.BalancePrechargeRepository)
	require.True(t, ok, "usage repository must implement the balance precharge ledger")
	return repo
}

func prechargeCommand(id string) *service.BalancePrechargeCommand {
	return &service.BalancePrechargeCommand{ID: id, UserID: 1, APIKeyID: 1, GroupID: 1, Threshold: 10, Amount: 2}
}

func assertPrechargeWallet(t *testing.T, db *sql.DB, balance, frozen string) {
	t.Helper()
	var gotBalance, gotFrozen string
	require.NoError(t, db.QueryRow(`SELECT balance::text, frozen_balance::text FROM users WHERE id=1`).Scan(&gotBalance, &gotFrozen))
	require.Equal(t, balance, gotBalance)
	require.Equal(t, frozen, gotFrozen)
}

func prechargeUsage(id, hold string, cost float64) *service.UsageBillingCommand {
	return &service.UsageBillingCommand{RequestID: id, APIKeyID: 1, UserID: 1, BalancePrechargeID: hold, BalanceCost: cost}
}

func TestBalancePrechargeRepository_StrictThresholdAndIdempotentAdmission(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	ctx := context.Background()
	cmd := prechargeCommand("equal")
	cmd.Threshold = 5
	result, err := repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Reserved)
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
	_, err = db.Exec(`UPDATE users SET balance=4 WHERE id=1`)
	require.NoError(t, err)
	result, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Reserved, "a previously postpaid admission must not reserve on retry")
	cmd.ID = "below"
	result, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Reserved)
	require.Equal(t, 2.0, result.Amount)
	require.Equal(t, 2.0, result.NewBalance)
	result, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Reserved)
	assertPrechargeWallet(t, db, "2.00000000", "2.00000000")
	cmd.Amount = 1
	_, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
	assertPrechargeWallet(t, db, "2.00000000", "2.00000000")
}

func TestBalancePrechargeRepository_ConcurrentKeysAndGroupsShareAvailableBalance(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	start := make(chan struct{})
	errs := make(chan error, 24)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			cmd := prechargeCommand(fmt.Sprintf("concurrent-%d", i))
			cmd.APIKeyID = int64(i%2 + 1)
			cmd.GroupID = cmd.APIKeyID
			_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	reserved, waiting := 0, 0
	for err := range errs {
		if err == nil {
			reserved++
		} else if errors.Is(err, service.ErrBalancePrechargeWaiting) {
			waiting++
		} else {
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	require.Equal(t, 2, reserved)
	require.Equal(t, 22, waiting)
	assertPrechargeWallet(t, db, "1.00000000", "4.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges`).Scan(&count))
	require.Equal(t, 2, count, "failed admission must not leave a reservation")
}

func TestBalancePrechargeRepository_WaitingReservationRetriesAfterSettlement(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", capture), func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			// With one connection, release and retry also prove waiting admissions
			// return their transaction and do not retain the user's row lock.
			db.SetMaxOpenConns(1)
			repo := balancePrechargeRepo(t, db)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, id := range []string{"first", "second"} {
				_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand(id))
				require.NoError(t, err)
			}
			waiting := prechargeCommand("waiting")
			result, err := repo.ReserveBalancePrecharge(ctx, waiting)
			require.ErrorIs(t, err, service.ErrBalancePrechargeWaiting)
			require.Nil(t, result)
			assertPrechargeWallet(t, db, "1.00000000", "4.00000000")
			var count int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM balance_precharges WHERE id='waiting'`).Scan(&count))
			require.Zero(t, count, "waiting must not insert an admission or move money")
			if capture {
				_, err = NewUsageBillingRepository(nil, db).Apply(ctx, prechargeUsage("first-cost", "first", .5))
				require.NoError(t, err)
			} else {
				released, releaseErr := repo.ReleaseBalancePrecharge(ctx, "first", 1)
				require.NoError(t, releaseErr)
				require.True(t, released)
			}
			result, err = repo.ReserveBalancePrecharge(ctx, waiting)
			require.NoError(t, err)
			require.True(t, result.Reserved)
			if capture {
				assertPrechargeWallet(t, db, "0.50000000", "4.00000000")
			} else {
				assertPrechargeWallet(t, db, "1.00000000", "4.00000000")
			}
			again, err := repo.ReserveBalancePrecharge(ctx, waiting)
			require.NoError(t, err)
			require.Equal(t, result, again, "retry must retain the same durable reservation")
		})
	}
}

func TestBalancePrechargeRepository_WaitsOnlyWhenFrozenFundsCanCoverAdmission(t *testing.T) {
	for _, test := range []struct {
		name            string
		balance, frozen float64
		wantErr         error
	}{
		{"no holds", 1, 0, service.ErrInsufficientBalance},
		{"frozen total still insufficient", .5, 1, service.ErrInsufficientBalance},
		{"negative total still insufficient", -2, 3, service.ErrInsufficientBalance},
		{"all available funds frozen", 0, 2, service.ErrBalancePrechargeWaiting},
		{"negative available can recover", -1, 3, service.ErrBalancePrechargeWaiting},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			_, err := db.Exec(`UPDATE users SET balance=$1, frozen_balance=$2 WHERE id=1`, test.balance, test.frozen)
			require.NoError(t, err)
			result, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(context.Background(), prechargeCommand("blocked"))
			require.ErrorIs(t, err, test.wantErr)
			require.Nil(t, result)
			assertPrechargeWallet(t, db, fmt.Sprintf("%.8f", test.balance), fmt.Sprintf("%.8f", test.frozen))
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestBalancePrechargeRepository_WaitingBecomesInsufficientAfterActualCost(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand(id))
		require.NoError(t, err)
	}
	waiting := prechargeCommand("waiting-exhausted")
	_, err := repo.ReserveBalancePrecharge(ctx, waiting)
	require.ErrorIs(t, err, service.ErrBalancePrechargeWaiting)
	_, err = NewUsageBillingRepository(nil, db).Apply(ctx, prechargeUsage("large-actual-cost", "first", 4))
	require.NoError(t, err)
	_, err = repo.ReserveBalancePrecharge(ctx, waiting)
	require.ErrorIs(t, err, service.ErrInsufficientBalance, "outstanding holds cannot cover a request after known charges deplete total funds")
	assertPrechargeWallet(t, db, "-1.00000000", "2.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges WHERE id='waiting-exhausted'`).Scan(&count))
	require.Zero(t, count)
}

func TestBalancePrechargeRepository_RechecksWaitingAPIKeyEligibility(t *testing.T) {
	for _, test := range []struct {
		name, update string
		wantErr      error
	}{
		{"disabled", `UPDATE api_keys SET status='disabled' WHERE id=1`, infraerrors.Forbidden("API_KEY_DISABLED", "api key is disabled")},
		{"unknown status", `UPDATE api_keys SET status='unknown' WHERE id=1`, infraerrors.Forbidden("API_KEY_DISABLED", "api key is disabled")},
		{"expired status", `UPDATE api_keys SET status='expired' WHERE id=1`, service.ErrAPIKeyExpired},
		{"expiry timestamp", `UPDATE api_keys SET expires_at=NOW()-INTERVAL '1 second' WHERE id=1`, service.ErrAPIKeyExpired},
		{"quota status", `UPDATE api_keys SET status='quota_exhausted' WHERE id=1`, service.ErrAPIKeyQuotaExhausted},
		{"quota amount", `UPDATE api_keys SET quota=1, quota_used=1 WHERE id=1`, service.ErrAPIKeyQuotaExhausted},
		{"deleted", `UPDATE api_keys SET deleted_at=NOW() WHERE id=1`, service.ErrAPIKeyNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			repo := balancePrechargeRepo(t, db)
			ctx := context.Background()
			for _, id := range []string{"first", "second"} {
				_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand(id))
				require.NoError(t, err)
			}
			waiting := prechargeCommand("waiting-key")
			_, err := repo.ReserveBalancePrecharge(ctx, waiting)
			require.ErrorIs(t, err, service.ErrBalancePrechargeWaiting)
			_, err = db.Exec(test.update)
			require.NoError(t, err)
			_, err = repo.ReserveBalancePrecharge(ctx, waiting)
			require.ErrorIs(t, err, test.wantErr, "revoked keys should leave the queue before frozen funds are released")
			released, err := repo.ReleaseBalancePrecharge(ctx, "first", 1)
			require.NoError(t, err)
			require.True(t, released)
			_, err = repo.ReserveBalancePrecharge(ctx, waiting)
			require.ErrorIs(t, err, test.wantErr, "new available funds must not bypass changed key eligibility")
			assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges WHERE id='waiting-key'`).Scan(&count))
			require.Zero(t, count)
			owned, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand("second"))
			require.NoError(t, err, "existing owned holds remain idempotent after key eligibility changes")
			require.True(t, owned.Reserved)
			released, err = repo.ReleaseBalancePrecharge(ctx, "second", 1)
			require.NoError(t, err)
			require.True(t, released)
			assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
		})
	}
}

func TestBalancePrechargeRepository_RechecksWaitingUserStatus(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand(id))
		require.NoError(t, err)
	}
	waiting := prechargeCommand("waiting-user")
	_, err := repo.ReserveBalancePrecharge(ctx, waiting)
	require.ErrorIs(t, err, service.ErrBalancePrechargeWaiting)
	_, err = db.Exec(`UPDATE users SET status='disabled' WHERE id=1`)
	require.NoError(t, err)
	_, err = repo.ReserveBalancePrecharge(ctx, waiting)
	require.Equal(t, "USER_INACTIVE", infraerrors.Reason(err))
	released, err := repo.ReleaseBalancePrecharge(ctx, "first", 1)
	require.NoError(t, err)
	require.True(t, released, "inactive users must still get existing holds refunded")
	_, err = repo.ReserveBalancePrecharge(ctx, waiting)
	require.Equal(t, "USER_INACTIVE", infraerrors.Reason(err))
	owned, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand("second"))
	require.NoError(t, err)
	require.True(t, owned.Reserved, "an existing owned hold remains idempotent after user is disabled")
	_, err = NewUsageBillingRepository(nil, db).Apply(ctx, prechargeUsage("inactive-user-cost", "second", .5))
	require.NoError(t, err, "already incurred charges must still settle for inactive users")
	assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges WHERE id='waiting-user'`).Scan(&count))
	require.Zero(t, count)
}

func TestBalancePrechargeRepository_MinimumBalanceUsesAvailableBeforeReservation(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	ctx := context.Background()
	_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand("occupying"))
	require.NoError(t, err)
	cmd := prechargeCommand("minimum")
	cmd.MinimumBalance = 4
	_, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.ErrorIs(t, err, service.ErrBalancePrechargeWaiting, "enough for the hold but not the minimum must wait")
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	released, err := repo.ReleaseBalancePrecharge(ctx, "occupying", 1)
	require.NoError(t, err)
	require.True(t, released)
	result, err := repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Reserved)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	cmd.MinimumBalance = 10
	result, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err, "an existing reservation must survive minimum policy changes and its own frozen funds")
	require.True(t, result.Reserved)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	cmd.ID = "minimum-depleted"
	cmd.MinimumBalance = 6
	_, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.ErrorIs(t, err, service.ErrInsufficientBalance, "even release of all holds would not satisfy the minimum")
}

func TestBalancePrechargeRepository_MinimumBalanceAlsoAppliesAbovePrechargeThreshold(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	cmd := prechargeCommand("minimum-postpaid")
	cmd.Threshold = 3
	cmd.MinimumBalance = 6
	_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	cmd.MinimumBalance = 5
	result, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Reserved)
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
}

func TestBalancePrechargeRepository_ConcurrentSameIDReservesOnce(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	start := make(chan struct{})
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			repo := NewUsageBillingRepository(nil, db).(service.BalancePrechargeRepository)
			_, err := repo.ReserveBalancePrecharge(context.Background(), prechargeCommand("same-id"))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
}

func TestBalancePrechargeRepository_DoesNotSubtractExistingFrozenBalance(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	_, err := db.Exec(`UPDATE users SET balance=2, frozen_balance=20 WHERE id=1`)
	require.NoError(t, err)
	result, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(context.Background(), prechargeCommand("batch-coexist"))
	require.NoError(t, err)
	require.True(t, result.Reserved)
	assertPrechargeWallet(t, db, "0.00000000", "22.00000000")
}

func TestBalancePrechargeRepository_SettlesActualAndDeduplicates(t *testing.T) {
	for _, test := range []struct {
		name, balance string
		cost          float64
		overdraft     bool
	}{
		{"refund", "4.25000000", .75, false},
		{"supplement", "-2.00000000", 7, true},
		{"zero", "5.00000000", 0, false},
		{"money8", "4.99992187", .000078125, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			holds := balancePrechargeRepo(t, db)
			billing := NewUsageBillingRepository(nil, db)
			ctx := context.Background()
			_, err := holds.ReserveBalancePrecharge(ctx, prechargeCommand("held"))
			require.NoError(t, err)
			cmd := prechargeUsage("actual", "held", test.cost)
			result, err := billing.Apply(ctx, cmd)
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.Equal(t, test.overdraft, result.BalanceOverdrafted)
			require.NotNil(t, result.NewBalance)
			assertPrechargeWallet(t, db, test.balance, "0.00000000")
			result, err = billing.Apply(ctx, cmd)
			require.NoError(t, err)
			require.False(t, result.Applied)
			released, err := holds.ReleaseBalancePrecharge(ctx, "held", 1)
			require.NoError(t, err)
			require.False(t, released)
			assertPrechargeWallet(t, db, test.balance, "0.00000000")
			var state string
			require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='held'`).Scan(&state))
			require.Equal(t, "captured", state)
		})
	}
}

func TestBalancePrechargeRepository_DuplicateUsageReleasesRedundantHold(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(fmt.Sprintf("archive=%t", archived), func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			holds := balancePrechargeRepo(t, db)
			billing := NewUsageBillingRepository(nil, db)
			ctx := context.Background()
			_, err := holds.ReserveBalancePrecharge(ctx, prechargeCommand("first"))
			require.NoError(t, err)
			_, err = billing.Apply(ctx, prechargeUsage("same-usage", "first", .5))
			require.NoError(t, err)
			if archived {
				_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive (request_id, api_key_id, request_fingerprint, created_at) SELECT request_id, api_key_id, request_fingerprint, created_at FROM usage_billing_dedup; DELETE FROM usage_billing_dedup`)
				require.NoError(t, err)
			}
			_, err = holds.ReserveBalancePrecharge(ctx, prechargeCommand("redundant"))
			require.NoError(t, err)
			result, err := billing.Apply(ctx, prechargeUsage("same-usage", "redundant", .5))
			require.NoError(t, err)
			require.False(t, result.Applied)
			assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
			var state string
			require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='redundant'`).Scan(&state))
			require.Equal(t, "released", state)
		})
	}
}

func TestBalancePrechargeRepository_LaterUsageEventsChargeWithoutRecapturing(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	holds := balancePrechargeRepo(t, db)
	billing := NewUsageBillingRepository(nil, db)
	ctx := context.Background()
	_, err := holds.ReserveBalancePrecharge(ctx, prechargeCommand("turns"))
	require.NoError(t, err)
	for i, cost := range []float64{.5, 1.25, 4} {
		cmd := prechargeUsage(fmt.Sprintf("turn-%d", i), "turns", cost)
		result, err := billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
		result, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.False(t, result.Applied)
	}
	assertPrechargeWallet(t, db, "-0.75000000", "0.00000000")
}

func TestBalancePrechargeRepository_LaterUsageAfterDuplicateRefundStillCharges(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	holds := balancePrechargeRepo(t, db)
	billing := NewUsageBillingRepository(nil, db)
	ctx := context.Background()
	_, err := billing.Apply(ctx, prechargeUsage("existing-turn", "", .5))
	require.NoError(t, err)
	_, err = holds.ReserveBalancePrecharge(ctx, prechargeCommand("duplicate-then-new"))
	require.NoError(t, err)
	duplicate, err := billing.Apply(ctx, prechargeUsage("existing-turn", "duplicate-then-new", .5))
	require.NoError(t, err)
	require.False(t, duplicate.Applied)
	assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
	next, err := billing.Apply(ctx, prechargeUsage("new-turn", "duplicate-then-new", 1.25))
	require.NoError(t, err)
	require.True(t, next.Applied)
	assertPrechargeWallet(t, db, "3.25000000", "0.00000000")
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='duplicate-then-new'`).Scan(&state))
	require.Equal(t, "released", state, "the reservation must remain in its single terminal state")
}

func TestBalancePrechargeRepository_RejectsMissingAndMismatchedHold(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	holds := balancePrechargeRepo(t, db)
	billing := NewUsageBillingRepository(nil, db)
	ctx := context.Background()
	_, err := billing.Apply(ctx, prechargeUsage("missing-usage", "missing-hold", 1))
	require.ErrorIs(t, err, service.ErrBalancePrechargeNotFound)
	_, err = holds.ReserveBalancePrecharge(ctx, prechargeCommand("owned"))
	require.NoError(t, err)
	cmd := prechargeUsage("wrong-key", "owned", 1)
	cmd.APIKeyID = 2
	_, err = billing.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
	_, err = holds.ReleaseBalancePrecharge(ctx, "owned", 2)
	require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
	foreignReservation := prechargeCommand("foreign-key")
	foreignReservation.APIKeyID = 3
	_, err = holds.ReserveBalancePrecharge(ctx, foreignReservation)
	require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
	cmd = prechargeUsage("wrong-user", "owned", 1)
	cmd.UserID = 2
	_, err = billing.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Zero(t, count, "failed capture must roll back its dedup claim")
}

func TestBalancePrechargeRepository_ReservationFailureRollsBackLedger(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	_, err := db.Exec(`CREATE FUNCTION reject_precharge_freeze() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'injected freeze failure'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_precharge_freeze BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION reject_precharge_freeze()`)
	require.NoError(t, err)
	_, err = balancePrechargeRepo(t, db).ReserveBalancePrecharge(context.Background(), prechargeCommand("freeze-rollback"))
	require.ErrorContains(t, err, "injected freeze failure")
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges`).Scan(&count))
	require.Zero(t, count)
}

func TestBalancePrechargeRepository_ReleaseFailureRollsBackRefund(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := balancePrechargeRepo(t, db)
	_, err := repo.ReserveBalancePrecharge(context.Background(), prechargeCommand("release-rollback"))
	require.NoError(t, err)
	_, err = db.Exec(`CREATE FUNCTION reject_precharge_release() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'injected release failure'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_precharge_release BEFORE UPDATE ON balance_precharges FOR EACH ROW EXECUTE FUNCTION reject_precharge_release()`)
	require.NoError(t, err)
	_, err = repo.ReleaseBalancePrecharge(context.Background(), "release-rollback", 1)
	require.ErrorContains(t, err, "injected release failure")
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='release-rollback'`).Scan(&state))
	require.Equal(t, "reserved", state)
}

func TestBalancePrechargeRepository_RejectedTransitionRollsBackMoney(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", capture), func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			repo := balancePrechargeRepo(t, db)
			ctx := context.Background()
			_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand("transition-rollback"))
			require.NoError(t, err)
			_, err = db.Exec(`CREATE FUNCTION suppress_precharge_transition() RETURNS TRIGGER AS $$ BEGIN RETURN NULL; END $$ LANGUAGE plpgsql;
				CREATE TRIGGER suppress_precharge_transition BEFORE UPDATE ON balance_precharges FOR EACH ROW EXECUTE FUNCTION suppress_precharge_transition()`)
			require.NoError(t, err)
			if capture {
				_, err = NewUsageBillingRepository(nil, db).Apply(ctx, prechargeUsage("transition-cost", "transition-rollback", .5))
			} else {
				_, err = repo.ReleaseBalancePrecharge(ctx, "transition-rollback", 1)
			}
			require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
			assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
		})
	}
}

func TestBalancePrechargeRepository_MissingFrozenFundsRetainsEvidence(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", capture), func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			repo := balancePrechargeRepo(t, db)
			ctx := context.Background()
			_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand("frozen-invariant"))
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE users SET frozen_balance=1 WHERE id=1`)
			require.NoError(t, err)
			if capture {
				_, err = NewUsageBillingRepository(nil, db).Apply(ctx, prechargeUsage("frozen-cost", "frozen-invariant", .5))
			} else {
				_, err = repo.ReleaseBalancePrecharge(ctx, "frozen-invariant", 1)
			}
			require.Error(t, err)
			assertPrechargeWallet(t, db, "3.00000000", "1.00000000")
			var state string
			require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='frozen-invariant'`).Scan(&state))
			require.Equal(t, "reserved", state)
		})
	}
}

func TestBalancePrechargeRepository_PreservesPostpaidBilling(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db)
	cmd := prechargeUsage("legacy-overdraft", "", 7)
	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.BalanceOverdrafted)
	result, err = repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	assertPrechargeWallet(t, db, "-2.00000000", "0.00000000")
}

func TestBalancePrechargeRepository_ExistingBatchHoldsSettleIndependently(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db)
	ctx := context.Background()
	batch := &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID("batch"), BatchID: "batch", APIKeyID: 1, UserID: 1, HoldAmount: 1}
	_, err := repo.ReserveBatchImageBalance(ctx, batch)
	require.NoError(t, err)
	_, err = balancePrechargeRepo(t, db).ReserveBalancePrecharge(ctx, prechargeCommand("alongside-batch"))
	require.NoError(t, err)
	assertPrechargeWallet(t, db, "2.00000000", "3.00000000")
	_, err = repo.Apply(ctx, prechargeUsage("precharge-cost", "alongside-batch", .5))
	require.NoError(t, err)
	assertPrechargeWallet(t, db, "3.50000000", "1.00000000")
	batch.RequestID = "batch-capture"
	batch.RequestFingerprint = ""
	batch.ActualAmount = .25
	_, err = repo.CaptureBatchImageBalance(ctx, batch)
	require.NoError(t, err)
	assertPrechargeWallet(t, db, "4.25000000", "0.00000000")
}

func TestBalancePrechargeRepository_ReleaseAndCaptureHaveExclusiveTerminalStates(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	holds := balancePrechargeRepo(t, db)
	billing := NewUsageBillingRepository(nil, db)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		_, err := db.Exec(`UPDATE users SET balance=5, frozen_balance=0 WHERE id=1`)
		require.NoError(t, err)
		id := fmt.Sprintf("race-%d", i)
		_, err = holds.ReserveBalancePrecharge(ctx, prechargeCommand(id))
		require.NoError(t, err)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var releaseErr, captureErr error
		var released bool
		wg.Add(2)
		go func() { defer wg.Done(); <-start; released, releaseErr = holds.ReleaseBalancePrecharge(ctx, id, 1) }()
		go func() { defer wg.Done(); <-start; _, captureErr = billing.Apply(ctx, prechargeUsage(id, id, 1)) }()
		close(start)
		wg.Wait()
		require.NoError(t, releaseErr)
		if released {
			require.ErrorIs(t, captureErr, service.ErrBalancePrechargeConflict)
			assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
		} else {
			require.NoError(t, captureErr)
			assertPrechargeWallet(t, db, "4.00000000", "0.00000000")
		}
		again, err := holds.ReleaseBalancePrecharge(ctx, id, 1)
		require.NoError(t, err)
		require.False(t, again)
	}
}

func TestBalancePrechargeRepository_QuotaFailureRollsBackCaptureAndDedup(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	holds := balancePrechargeRepo(t, db)
	billing := NewUsageBillingRepository(nil, db)
	ctx := context.Background()
	_, err := holds.ReserveBalancePrecharge(ctx, prechargeCommand("rollback"))
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE api_keys SET deleted_at=NOW() WHERE id=1`)
	require.NoError(t, err)
	cmd := prechargeUsage("rollback-usage", "rollback", .5)
	cmd.APIKeyQuotaCost = .5
	_, err = billing.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='rollback'`).Scan(&state))
	require.Equal(t, "reserved", state)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Zero(t, count)
	_, err = db.Exec(`UPDATE api_keys SET deleted_at=NULL WHERE id=1`)
	require.NoError(t, err)
	result, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
}

func TestBalancePrechargeRepository_RejectsInvalidMoneyBeforeSQL(t *testing.T) {
	repo := balancePrechargeRepo(t, &sql.DB{})
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1), .000000001, 1e12} {
		cmd := prechargeCommand("invalid")
		cmd.Amount = bad
		_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
		require.ErrorIs(t, err, service.ErrBalancePrechargeInvalid)
		cmd = prechargeCommand("invalid")
		cmd.Threshold = bad
		_, err = repo.ReserveBalancePrecharge(context.Background(), cmd)
		require.ErrorIs(t, err, service.ErrBalancePrechargeInvalid)
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), 1e12} {
		cmd := prechargeCommand("invalid-minimum")
		cmd.MinimumBalance = bad
		_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
		require.ErrorIs(t, err, service.ErrBalancePrechargeInvalid)
	}
}

func TestBalancePrechargeRepository_CanceledReservationDoesNotFreeze(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	_, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(ctx, prechargeCommand("canceled"))
	require.Error(t, err)
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
}
