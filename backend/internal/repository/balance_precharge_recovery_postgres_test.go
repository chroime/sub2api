//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBalancePrechargeRecovery_ExpiredOwnerEntersReviewWithoutRefund(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("lost-owner")
	cmd.LeaseOwner = "owner-a"
	_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE balance_precharges SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, cmd.ID)
	require.NoError(t, err)
	recovery := any(repo).(service.BalancePrechargeRecoveryRepository)
	count, err := recovery.RecoverBalancePrecharges(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	var reason string
	require.NoError(t, db.QueryRow(`SELECT reason FROM balance_precharge_reviews WHERE precharge_id=$1`, cmd.ID).Scan(&reason))
	require.Equal(t, "owner_lease_expired", reason)
	count, err = recovery.RecoverBalancePrecharges(context.Background(), 20)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestBalancePrechargeRecovery_LiveOwnerAndLateSettlement(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("live-owner")
	cmd.LeaseOwner = "owner-b"
	_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	recovery := any(repo).(service.BalancePrechargeRecoveryRepository)
	renewed, err := recovery.RenewBalancePrechargeLease(context.Background(), cmd.ID, cmd.LeaseOwner)
	require.NoError(t, err)
	require.True(t, renewed)
	renewed, err = recovery.RenewBalancePrechargeLease(context.Background(), cmd.ID, "wrong-owner")
	require.NoError(t, err)
	require.False(t, renewed)
	count, err := recovery.RecoverBalancePrecharges(context.Background(), 20)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = db.Exec(`UPDATE balance_precharges SET lease_expires_at=$2 WHERE id=$1`, cmd.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	count, err = recovery.RecoverBalancePrecharges(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	_, err = repo.Apply(context.Background(), prechargeUsage("late-valid-usage", cmd.ID, 0.5))
	require.NoError(t, err)
	assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
	reviews, total, err := repo.ListBalancePrechargeReviews(context.Background(), "settled", 20, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, reviews, 1)
}

func TestBalancePrechargeRecovery_ActiveLeaseCannotBeManuallyRefunded(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("manual-live")
	cmd.LeaseOwner = "owner-live"
	_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	require.NoError(t, repo.MarkBalancePrechargeForReview(context.Background(), &service.BalancePrechargeReviewEvidence{PrechargeID: cmd.ID, UserID: 1, Reason: "test-review"}))
	resolution := &service.BalancePrechargeResolutionCommand{PrechargeID: cmd.ID, ActorID: 1, Action: "release", Note: "confirmed no usage"}
	_, err = repo.ResolveBalancePrechargeReview(context.Background(), resolution)
	require.ErrorIs(t, err, service.ErrBalancePrechargeReviewConflict)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	require.NoError(t, repo.EndBalancePrechargeLease(context.Background(), cmd.ID, cmd.LeaseOwner))
	_, err = repo.ResolveBalancePrechargeReview(context.Background(), resolution)
	require.NoError(t, err)
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
}

func TestBalancePrechargeRecovery_LiveOwnerCanResumeAfterStorageOutage(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	ctx := context.Background()
	cmd := prechargeCommand("owner-resumes")
	cmd.LeaseOwner = "still-running-owner"
	_, err := repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE balance_precharges SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, cmd.ID)
	require.NoError(t, err)
	n, err := repo.RecoverBalancePrecharges(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	// A review is evidence of uncertain ownership, not revocation. A live owner
	// can resume after storage recovers even if the maintenance scan ran first.
	renewed, err := repo.RenewBalancePrechargeLease(ctx, cmd.ID, cmd.LeaseOwner)
	require.NoError(t, err)
	require.True(t, renewed)
	decision := &service.BalancePrechargeResolutionCommand{PrechargeID: cmd.ID, ActorID: 1, Action: "release", Note: "still in flight"}
	_, err = repo.ResolveBalancePrechargeReview(ctx, decision)
	require.ErrorIs(t, err, service.ErrBalancePrechargeReviewConflict)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	require.NoError(t, repo.EndBalancePrechargeLease(ctx, cmd.ID, cmd.LeaseOwner))
	renewed, err = repo.RenewBalancePrechargeLease(ctx, cmd.ID, cmd.LeaseOwner)
	require.NoError(t, err)
	require.False(t, renewed, "a completed owner must never resurrect its lease")
	_, err = repo.ResolveBalancePrechargeReview(ctx, decision)
	require.NoError(t, err)
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
}

func TestBalancePrechargeRecovery_DurableRetryAfterDatabaseWriteFailure(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("recover-db-failed")
	cmd.LeaseOwner = "old-owner"
	_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	require.NoError(t, repo.EndBalancePrechargeLease(context.Background(), cmd.ID, cmd.LeaseOwner))
	_, err = db.Exec(`CREATE FUNCTION reject_review() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected storage failure'; END $$;
  CREATE TRIGGER fail_review BEFORE INSERT ON balance_precharge_reviews FOR EACH ROW EXECUTE FUNCTION reject_review()`)
	require.NoError(t, err)
	_, err = repo.RecoverBalancePrecharges(context.Background(), 20)
	require.Error(t, err)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	_, err = db.Exec(`DROP TRIGGER fail_review ON balance_precharge_reviews`)
	require.NoError(t, err)
	// A newly created repository models loss of all process-local retry state.
	fresh := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	count, err := fresh.RecoverBalancePrecharges(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
}

func TestBalancePrechargeRecovery_LegacyGracePeriodAndBatchLimit(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("legacy-owner")
	_, err := repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	count, err := repo.RecoverBalancePrecharges(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = db.Exec(`UPDATE balance_precharges SET recovery_after=NOW()-INTERVAL '1 second' WHERE id=$1`, cmd.ID)
	require.NoError(t, err)
	count, err = repo.RecoverBalancePrecharges(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var reason string
	require.NoError(t, db.QueryRow(`SELECT reason FROM balance_precharge_reviews WHERE precharge_id=$1`, cmd.ID).Scan(&reason))
	require.Equal(t, "legacy_owner_unknown", reason)
	_, err = repo.RecoverBalancePrecharges(context.Background(), 0)
	require.Error(t, err)
}

func TestBalancePrechargeRecovery_ProcessExitLeavesRecoverableHold(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	var schema string
	require.NoError(t, db.QueryRow(`SELECT current_schema()`).Scan(&schema))
	require.True(t, strings.HasPrefix(schema, "precharge_test_"))
	parsed, err := url.Parse(os.Getenv("SUB2API_TEST_POSTGRES_DSN"))
	require.NoError(t, err)
	params := parsed.Query()
	params.Set("search_path", schema)
	params.Set("statement_timeout", "10000")
	parsed.RawQuery = params.Encode()
	child := exec.Command(os.Args[0], "-test.run=^TestBalancePrechargeRecovery_ProcessExitHelper$")
	child.Env = append(os.Environ(), "SUB2API_RECOVERY_CRASH_DSN="+parsed.String())
	// The child exits without running deferred cleanup after its committed hold.
	err = child.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, 23, exitErr.ExitCode())
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	_, err = db.Exec(`UPDATE balance_precharges SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id='process-crash'`)
	require.NoError(t, err)
	restarted := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	n, err := restarted.RecoverBalancePrecharges(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	reviews, _, err := restarted.ListBalancePrechargeReviews(context.Background(), "pending", 20, 0)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
}

func TestBalancePrechargeRecovery_ProcessExitHelper(t *testing.T) {
	dsn := os.Getenv("SUB2API_RECOVERY_CRASH_DSN")
	if dsn == "" {
		t.Skip("isolated child only")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		os.Exit(21)
	}
	var schema string
	if db.QueryRow(`SELECT current_schema()`).Scan(&schema) != nil || !strings.HasPrefix(schema, "precharge_test_") {
		os.Exit(22)
	}
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("process-crash")
	cmd.LeaseOwner = "terminated-child"
	if _, err = repo.ReserveBalancePrecharge(context.Background(), cmd); err != nil {
		os.Exit(24)
	}
	os.Exit(23)
}
