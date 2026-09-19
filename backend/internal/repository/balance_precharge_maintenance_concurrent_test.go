//go:build unit

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBalancePrechargeMaintenance_ConcurrentRecoverySettlementAndArchive(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("maintenance-race-%d", i)
		cmd := prechargeCommand(id)
		cmd.LeaseOwner = "owner-ended"
		cmd.Amount = .1
		_, err := repo.ReserveBalancePrecharge(ctx, cmd)
		require.NoError(t, err)
		require.NoError(t, repo.EndBalancePrechargeLease(ctx, id, cmd.LeaseOwner))
		bill := prechargeUsage("bill-"+id, id, .01)
		start := make(chan struct{})
		errors := make(chan error, 4)
		var wg sync.WaitGroup
		for n := 0; n < 2; n++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; _, err := repo.RecoverBalancePrecharges(ctx, 100); errors <- err }()
		}
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, err := repo.Apply(ctx, bill); errors <- err }()
		go func() {
			defer wg.Done()
			<-start
			_, err := repo.ArchiveBalancePrecharges(ctx, time.Now().Add(time.Hour), 100)
			errors <- err
		}()
		close(start)
		wg.Wait()
		close(errors)
		for err := range errors {
			require.NoError(t, err)
		}
		_, err = repo.ArchiveBalancePrecharges(ctx, time.Now().Add(time.Hour), 100)
		require.NoError(t, err)
		result, err := repo.Apply(ctx, bill)
		require.NoError(t, err)
		require.False(t, result.Applied)
		var copies, pending int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges_history WHERE id=$1`, id).Scan(&copies))
		require.Equal(t, 1, copies, "archival must preserve exactly one authoritative hold")
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges_history WHERE id=$1 AND state='reserved'`, id).Scan(&pending))
		require.Zero(t, pending)
	}
	assertPrechargeWallet(t, db, "4.88000000", "0.00000000")
	_, pending, err := repo.ListBalancePrechargeReviews(ctx, "pending", 100, 0)
	require.NoError(t, err)
	require.Zero(t, pending)
}

func TestBalancePrechargeRetention_ArchiveFailureRollsBackWholeBatch(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	ctx := context.Background()
	for _, id := range []string{"archive-first", "archive-second"} {
		_, err := repo.ReserveBalancePrecharge(ctx, prechargeCommand(id))
		require.NoError(t, err)
		require.NoError(t, repo.MarkBalancePrechargeForReview(ctx, prechargeReviewEvidence(id)))
		_, err = repo.ResolveBalancePrechargeReview(ctx, prechargeReviewDecision(id))
		require.NoError(t, err)
	}
	_, err := db.Exec(`CREATE FUNCTION reject_archive() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected archive storage failure'; END $$;
CREATE TRIGGER fail_archive BEFORE INSERT ON balance_precharge_reviews_archive FOR EACH ROW EXECUTE FUNCTION reject_archive()`)
	require.NoError(t, err)
	_, err = repo.ArchiveBalancePrecharges(ctx, time.Now().Add(time.Hour), 100)
	require.Error(t, err)
	var hot, cold, reviews int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges`).Scan(&hot))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharges_archive`).Scan(&cold))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharge_reviews`).Scan(&reviews))
	require.Equal(t, 2, hot)
	require.Equal(t, 2, reviews)
	require.Zero(t, cold)
	assertPrechargeWallet(t, db, "3.50000000", "0.00000000")
	_, err = db.Exec(`DROP TRIGGER fail_archive ON balance_precharge_reviews_archive`)
	require.NoError(t, err)
	n, err := repo.ArchiveBalancePrecharges(ctx, time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	assertPrechargeWallet(t, db, "3.50000000", "0.00000000")
}
