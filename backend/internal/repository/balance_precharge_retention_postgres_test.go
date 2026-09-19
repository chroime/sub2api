//go:build unit

package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBalancePrechargeRetention_PreservesIdempotencyAndReviews(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "244_add_balance_precharge_archive.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	ctx := context.Background()
	cmd := prechargeCommand("archive-captured")
	_, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	bill := prechargeUsage("archived-bill", cmd.ID, .5)
	_, err = repo.Apply(ctx, bill)
	require.NoError(t, err)
	reviewed := prechargeCommand("archive-reviewed")
	_, err = repo.ReserveBalancePrecharge(ctx, reviewed)
	require.NoError(t, err)
	require.NoError(t, repo.MarkBalancePrechargeForReview(ctx, &service.BalancePrechargeReviewEvidence{PrechargeID: reviewed.ID, UserID: 1, Reason: "known_failure"}))
	decision := &service.BalancePrechargeResolutionCommand{PrechargeID: reviewed.ID, ActorID: 1, Action: "release", Note: "confirmed"}
	_, err = repo.ResolveBalancePrechargeReview(ctx, decision)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE balance_precharges SET updated_at=NOW()-INTERVAL '100 days'; UPDATE balance_precharge_reviews SET updated_at=NOW()-INTERVAL '100 days',resolved_at=NOW()-INTERVAL '100 days'`)
	require.NoError(t, err)
	count, err := repo.ArchiveBalancePrecharges(ctx, time.Now().Add(-90*24*time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	_, err = repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	applied, err := repo.Apply(ctx, bill)
	require.NoError(t, err)
	require.False(t, applied.Applied)
	_, err = repo.ResolveBalancePrechargeReview(ctx, decision)
	require.NoError(t, err)
	reviews, total, err := repo.ListBalancePrechargeReviews(ctx, "resolved", 20, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, reviews, 1)
	assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
	count, err = repo.ArchiveBalancePrecharges(ctx, time.Now(), 100)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestBalancePrechargeRetention_NeverMovesPendingHolds(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "244_add_balance_precharge_archive.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	cmd := prechargeCommand("keep-pending")
	_, err = repo.ReserveBalancePrecharge(context.Background(), cmd)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE balance_precharges SET updated_at=NOW()-INTERVAL '365 days'`)
	require.NoError(t, err)
	n, err := repo.ArchiveBalancePrecharges(context.Background(), time.Now(), 100)
	require.NoError(t, err)
	require.Zero(t, n)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
}

func TestBalancePrechargeRetention_ArchivedHoldAcceptsDistinctLateBilling(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := NewUsageBillingRepository(nil, db).(*usageBillingRepository)
	ctx := context.Background()
	cmd := prechargeCommand("archived-multiple-events")
	_, err := repo.ReserveBalancePrecharge(ctx, cmd)
	require.NoError(t, err)
	first := prechargeUsage("event-before-archive", cmd.ID, .5)
	_, err = repo.Apply(ctx, first)
	require.NoError(t, err)
	n, err := repo.ArchiveBalancePrecharges(ctx, time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	second := prechargeUsage("distinct-event-after-archive", cmd.ID, .125)
	result, err := repo.Apply(ctx, second)
	require.NoError(t, err)
	require.True(t, result.Applied)
	for _, bill := range []*service.UsageBillingCommand{first, second} {
		result, err = repo.Apply(ctx, bill)
		require.NoError(t, err)
		require.False(t, result.Applied)
	}
	assertPrechargeWallet(t, db, "4.37500000", "0.00000000")
}
