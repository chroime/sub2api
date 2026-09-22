//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func prechargeReviewRepo(t *testing.T, db *sql.DB) service.BalancePrechargeReconciliationRepository {
	t.Helper()
	repo, ok := NewUsageBillingRepository(nil, db).(service.BalancePrechargeReconciliationRepository)
	require.True(t, ok)
	return repo
}

func prechargeReviewEvidence(id string) *service.BalancePrechargeReviewEvidence {
	return &service.BalancePrechargeReviewEvidence{PrechargeID: id, UserID: 1, Reason: "upstream ended without usage", RequestID: "upstream-request", AccountID: 7, Model: "gpt-test"}
}

func prechargeReviewDecision(id string) *service.BalancePrechargeResolutionCommand {
	return &service.BalancePrechargeResolutionCommand{PrechargeID: id, ActorID: 99, Action: "charge", ActualCost: .75, Note: "confirmed by upstream billing"}
}

func createPendingPrechargeReview(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(context.Background(), prechargeCommand(id))
	require.NoError(t, err)
	require.NoError(t, prechargeReviewRepo(t, db).MarkBalancePrechargeForReview(context.Background(), prechargeReviewEvidence(id)))
}

func TestBalancePrechargeReviewRepository_RequiresExplicitEvidenceEvenForOldHolds(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	repo := prechargeReviewRepo(t, db)
	ctx := context.Background()
	_, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(ctx, prechargeCommand("unmarked"))
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE balance_precharges SET created_at=NOW()-INTERVAL '30 days' WHERE id='unmarked'`)
	require.NoError(t, err)
	for _, id := range []string{"missing", "unmarked"} {
		_, err = repo.ResolveBalancePrechargeReview(ctx, prechargeReviewDecision(id))
		require.ErrorIs(t, err, service.ErrBalancePrechargeReviewNotFound)
	}
	reviews, total, err := repo.ListBalancePrechargeReviews(ctx, "all", 20, 0)
	require.NoError(t, err)
	require.Empty(t, reviews)
	require.NotNil(t, reviews)
	require.Zero(t, total)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")

	evidence := prechargeReviewEvidence("unmarked")
	evidence.UserID = 2
	require.ErrorIs(t, repo.MarkBalancePrechargeForReview(ctx, evidence), service.ErrBalancePrechargeConflict)
	evidence.UserID = 1
	evidence.Reason = "  upstream ended without usage  "
	require.NoError(t, repo.MarkBalancePrechargeForReview(ctx, evidence))
	evidence.Reason = "retry must not replace original evidence"
	evidence.RequestID = "other-request"
	require.NoError(t, repo.MarkBalancePrechargeForReview(ctx, evidence))
	reviews, total, err = repo.ListBalancePrechargeReviews(ctx, "", 20, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, reviews, 1)
	require.Equal(t, "pending", reviews[0].Status)
	require.Equal(t, "upstream ended without usage", reviews[0].Reason)
	require.Equal(t, "upstream-request", reviews[0].RequestID)
	require.EqualValues(t, 7, reviews[0].AccountID)
	require.Equal(t, "gpt-test", reviews[0].Model)
	require.Equal(t, "wallet-test@example.com", reviews[0].UserEmail)
	require.Nil(t, reviews[0].ResolvedAt)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
}

func TestBalancePrechargeReviewRepository_ManualResolutionSettlesOnlyWalletAndAudit(t *testing.T) {
	for _, test := range []struct {
		name, action, balance string
		cost                  float64
	}{
		{"release", "release", "5.00000000", 0},
		{"refund difference", "charge", "4.25000000", .75},
		{"charge difference", "charge", "2.50000000", 2.5},
		{"overdraft follows actual cost", "charge", "-1.00000000", 6},
		{"eight decimal places", "charge", "4.87654322", .12345678},
		{"maximum confirmed cost", "charge", "-999995.00000000", 1000000},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			createPendingPrechargeReview(t, db, "manual")
			repo := prechargeReviewRepo(t, db)
			cmd := prechargeReviewDecision("manual")
			cmd.Action, cmd.ActualCost = test.action, test.cost
			cmd.Note = "  已核实上游账单  "
			result, err := repo.ResolveBalancePrechargeReview(context.Background(), cmd)
			require.NoError(t, err)
			require.Equal(t, "resolved", result.Status)
			require.Equal(t, test.action, result.Resolution)
			require.Equal(t, "已核实上游账单", result.Note)
			require.NotNil(t, result.ResolvedAt)
			require.EqualValues(t, 99, *result.ResolvedBy)
			require.Equal(t, test.cost, *result.ActualCost)
			require.InDelta(t, 5-test.cost, *result.NewBalance, 1e-9)
			assertPrechargeWallet(t, db, test.balance, "0.00000000")
			var state, quota string
			var duplicate, captured sql.NullString
			require.NoError(t, db.QueryRow(`SELECT state, duplicate_request_id, captured_request_id FROM balance_precharges WHERE id='manual'`).Scan(&state, &duplicate, &captured))
			require.Equal(t, "released", state)
			require.False(t, duplicate.Valid)
			require.False(t, captured.Valid)
			require.NoError(t, db.QueryRow(`SELECT quota_used::text FROM api_keys WHERE id=1`).Scan(&quota))
			require.Equal(t, "0.00000000", quota, "manual costs must not fabricate quota consumption")
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
			require.Zero(t, count, "manual costs must not fabricate usage claims")
			// A late automatic worker must roll back its newly claimed dedup key.
			_, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), prechargeUsage("late-usage", "manual", .1))
			require.ErrorIs(t, err, service.ErrBalancePrechargeConflict)
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
			require.Zero(t, count)
			assertPrechargeWallet(t, db, test.balance, "0.00000000")
		})
	}
}

func TestBalancePrechargeReviewRepository_IdempotencyIncludesActorCostActionAndNote(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	createPendingPrechargeReview(t, db, "idempotent")
	repo := prechargeReviewRepo(t, db)
	ctx := context.Background()
	cmd := prechargeReviewDecision("idempotent")
	first, err := repo.ResolveBalancePrechargeReview(ctx, cmd)
	require.NoError(t, err)
	cmd.Note = "  " + cmd.Note + "\n"
	again, err := repo.ResolveBalancePrechargeReview(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, first, again, "the original audit result must survive an identical retry")
	for _, mutate := range []func(*service.BalancePrechargeResolutionCommand){
		func(c *service.BalancePrechargeResolutionCommand) { c.ActorID++ },
		func(c *service.BalancePrechargeResolutionCommand) { c.ActualCost = .5 },
		func(c *service.BalancePrechargeResolutionCommand) { c.Note = "different justification" },
		func(c *service.BalancePrechargeResolutionCommand) { c.Action, c.ActualCost = "release", 0 },
	} {
		other := *cmd
		mutate(&other)
		_, err = repo.ResolveBalancePrechargeReview(ctx, &other)
		require.ErrorIs(t, err, service.ErrBalancePrechargeReviewConflict)
	}
	assertPrechargeWallet(t, db, "4.25000000", "0.00000000")
	list, total, err := repo.ListBalancePrechargeReviews(ctx, "resolved", 20, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, first, list[0])
}

func TestBalancePrechargeReviewRepository_ConcurrentIdenticalDecisionsMoveMoneyOnce(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	createPendingPrechargeReview(t, db, "concurrent-admin")
	repo := prechargeReviewRepo(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	type outcome struct {
		result *service.BalancePrechargeReview
		err    error
	}
	outcomes := make(chan outcome, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.ResolveBalancePrechargeReview(ctx, prechargeReviewDecision("concurrent-admin"))
			outcomes <- outcome{result, err}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)
	var first *service.BalancePrechargeReview
	for out := range outcomes {
		require.NoError(t, out.err)
		if first == nil {
			first = out.result
		}
		require.Equal(t, first, out.result)
	}
	assertPrechargeWallet(t, db, "4.25000000", "0.00000000")
}

func TestBalancePrechargeReviewRepository_AutomaticSettlementWinsWithoutManualCharge(t *testing.T) {
	for _, marked := range []bool{false, true} {
		for _, capture := range []bool{false, true} {
			t.Run(fmt.Sprintf("marked=%t/capture=%t", marked, capture), func(t *testing.T) {
				db := newBalancePrechargeTestDB(t)
				ctx := context.Background()
				holds, reviews := balancePrechargeRepo(t, db), prechargeReviewRepo(t, db)
				_, err := holds.ReserveBalancePrecharge(ctx, prechargeCommand("automatic"))
				require.NoError(t, err)
				if marked {
					require.NoError(t, reviews.MarkBalancePrechargeForReview(ctx, prechargeReviewEvidence("automatic")))
				}
				if capture {
					_, err = NewUsageBillingRepository(nil, db).Apply(ctx, prechargeUsage("automatic-usage", "automatic", .5))
				} else {
					_, err = holds.ReleaseBalancePrecharge(ctx, "automatic", 1)
				}
				require.NoError(t, err)
				require.NoError(t, reviews.MarkBalancePrechargeForReview(ctx, prechargeReviewEvidence("automatic")), "late marking cannot resurrect an already settled request")
				pending, count, err := reviews.ListBalancePrechargeReviews(ctx, "pending", 20, 0)
				require.NoError(t, err)
				require.Empty(t, pending)
				require.Zero(t, count)
				settled, count, err := reviews.ListBalancePrechargeReviews(ctx, "settled", 20, 0)
				require.NoError(t, err)
				_, resolveErr := reviews.ResolveBalancePrechargeReview(ctx, prechargeReviewDecision("automatic"))
				if marked {
					require.EqualValues(t, 1, count)
					require.Len(t, settled, 1)
					require.Equal(t, "settled", settled[0].Status)
					require.Nil(t, settled[0].ResolvedAt)
					require.ErrorIs(t, resolveErr, service.ErrBalancePrechargeReviewConflict)
				} else {
					require.Zero(t, count)
					require.Empty(t, settled)
					require.ErrorIs(t, resolveErr, service.ErrBalancePrechargeReviewNotFound)
				}
				if capture {
					assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
				} else {
					assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
				}
			})
		}
	}
}

func TestBalancePrechargeReviewRepository_AutomaticAndManualSettlementHaveOneWinner(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", capture), func(t *testing.T) {
			db := newBalancePrechargeTestDB(t)
			reviews, holds := prechargeReviewRepo(t, db), balancePrechargeRepo(t, db)
			billing := NewUsageBillingRepository(nil, db)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for i := 0; i < 12; i++ {
				_, err := db.Exec(`UPDATE users SET balance=5, frozen_balance=0 WHERE id=1`)
				require.NoError(t, err)
				id := fmt.Sprintf("settlement-race-%d", i)
				createPendingPrechargeReview(t, db, id)
				start := make(chan struct{})
				var manualErr, automaticErr error
				var manuallySettled *service.BalancePrechargeReview
				var automaticallySettled bool
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					manuallySettled, manualErr = reviews.ResolveBalancePrechargeReview(ctx, prechargeReviewDecision(id))
				}()
				go func() {
					defer wg.Done()
					<-start
					if capture {
						var result *service.UsageBillingApplyResult
						result, automaticErr = billing.Apply(ctx, prechargeUsage(id, id, .5))
						automaticallySettled = automaticErr == nil && result.Applied
					} else {
						automaticallySettled, automaticErr = holds.ReleaseBalancePrecharge(ctx, id, 1)
					}
				}()
				close(start)
				wg.Wait()
				if manualErr == nil {
					require.NotNil(t, manuallySettled)
					require.False(t, automaticallySettled)
					if capture {
						require.ErrorIs(t, automaticErr, service.ErrBalancePrechargeConflict)
						var count int
						require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1`, id).Scan(&count))
						require.Zero(t, count)
					} else {
						require.NoError(t, automaticErr)
					}
					assertPrechargeWallet(t, db, "4.25000000", "0.00000000")
				} else {
					require.ErrorIs(t, manualErr, service.ErrBalancePrechargeReviewConflict)
					require.NoError(t, automaticErr)
					require.True(t, automaticallySettled)
					if capture {
						assertPrechargeWallet(t, db, "4.50000000", "0.00000000")
					} else {
						assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
					}
				}
			}
		})
	}
}

func TestBalancePrechargeReviewRepository_FailedTransitionRollsBackWalletLedgerAndAudit(t *testing.T) {
	for _, table := range []string{"balance_precharges", "balance_precharge_reviews"} {
		for _, suppress := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/suppress=%t", table, suppress), func(t *testing.T) {
				db := newBalancePrechargeTestDB(t)
				createPendingPrechargeReview(t, db, "rollback")
				body := "RAISE EXCEPTION 'injected settlement failure';"
				if suppress {
					body = "RETURN NULL;"
				}
				_, err := db.Exec(`CREATE FUNCTION reject_review_transition() RETURNS TRIGGER AS $$ BEGIN ` + body + ` END $$ LANGUAGE plpgsql;
					CREATE TRIGGER reject_review_transition BEFORE UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION reject_review_transition()`)
				require.NoError(t, err)
				_, err = prechargeReviewRepo(t, db).ResolveBalancePrechargeReview(context.Background(), prechargeReviewDecision("rollback"))
				require.Error(t, err)
				assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
				var state string
				var resolvedAt sql.NullTime
				require.NoError(t, db.QueryRow(`SELECT hold.state, review.resolved_at FROM balance_precharges hold JOIN balance_precharge_reviews review ON review.precharge_id=hold.id WHERE hold.id='rollback'`).Scan(&state, &resolvedAt))
				require.Equal(t, "reserved", state)
				require.False(t, resolvedAt.Valid)
			})
		}
	}
}

func TestBalancePrechargeReviewRepository_MissingFrozenFundsPreservesPendingEvidence(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	createPendingPrechargeReview(t, db, "missing-frozen")
	_, err := db.Exec(`UPDATE users SET frozen_balance=1 WHERE id=1`)
	require.NoError(t, err)
	repo := prechargeReviewRepo(t, db)
	_, err = repo.ResolveBalancePrechargeReview(context.Background(), prechargeReviewDecision("missing-frozen"))
	require.ErrorContains(t, err, "frozen funds are insufficient")
	assertPrechargeWallet(t, db, "3.00000000", "1.00000000")
	list, count, err := repo.ListBalancePrechargeReviews(context.Background(), "pending", 20, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, list, 1)
	require.Nil(t, list[0].ResolvedAt)
}

func TestBalancePrechargeReviewRepository_HistoryPaginationSurvivesOwnerAndGroupDeletion(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	repo := prechargeReviewRepo(t, db)
	createPendingPrechargeReview(t, db, "resolved")
	_, err := repo.ResolveBalancePrechargeReview(ctx, prechargeReviewDecision("resolved"))
	require.NoError(t, err)
	createPendingPrechargeReview(t, db, "settled")
	_, err = balancePrechargeRepo(t, db).ReleaseBalancePrecharge(ctx, "settled", 1)
	require.NoError(t, err)
	createPendingPrechargeReview(t, db, "pending")
	_, err = db.Exec(`UPDATE balance_precharge_reviews SET created_at=TIMESTAMPTZ '2026-01-01 00:00:00+00'`)
	require.NoError(t, err)
	for _, status := range []string{"pending", "resolved", "settled"} {
		list, total, err := repo.ListBalancePrechargeReviews(ctx, status, 20, 0)
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		require.Len(t, list, 1)
		require.Equal(t, status, list[0].ID)
		require.Equal(t, status, list[0].Status)
	}
	for offset, id := range []string{"pending", "resolved", "settled"} {
		list, total, err := repo.ListBalancePrechargeReviews(ctx, "all", 1, offset)
		require.NoError(t, err)
		require.EqualValues(t, 3, total)
		require.Len(t, list, 1)
		require.Equal(t, id, list[0].ID)
	}
	list, total, err := repo.ListBalancePrechargeReviews(ctx, "all", 1, 3)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Empty(t, list)
	require.NotNil(t, list)
	_, err = db.Exec(`DELETE FROM users WHERE id=1; DELETE FROM groups WHERE id=1`)
	require.NoError(t, err)
	list, total, err = repo.ListBalancePrechargeReviews(ctx, "all", 20, 0)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Len(t, list, 3)
	for _, review := range list {
		require.EqualValues(t, 1, review.UserID)
		require.EqualValues(t, 1, review.APIKeyID)
		require.EqualValues(t, 1, review.GroupID)
		require.Empty(t, review.UserEmail)
		require.Empty(t, review.GroupName)
	}
}

func TestBalancePrechargeReviewRepository_InvalidResolutionRejectedBeforeSQL(t *testing.T) {
	repo := prechargeReviewRepo(t, &sql.DB{})
	ctx := context.Background()
	_, err := repo.ResolveBalancePrechargeReview(ctx, nil)
	require.ErrorIs(t, err, service.ErrBalancePrechargeReviewInvalid)
	for _, cost := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, 0, .000000001, 1000000.00000001} {
		cmd := prechargeReviewDecision("invalid")
		cmd.ActualCost = cost
		_, err := repo.ResolveBalancePrechargeReview(ctx, cmd)
		require.ErrorIs(t, err, service.ErrBalancePrechargeReviewInvalid)
	}
	for _, mutate := range []func(*service.BalancePrechargeResolutionCommand){
		func(c *service.BalancePrechargeResolutionCommand) { c.PrechargeID = " " },
		func(c *service.BalancePrechargeResolutionCommand) { c.PrechargeID = strings.Repeat("a", 129) },
		func(c *service.BalancePrechargeResolutionCommand) { c.ActorID = 0 },
		func(c *service.BalancePrechargeResolutionCommand) { c.ActorID = -1 },
		func(c *service.BalancePrechargeResolutionCommand) { c.Action = "refund" },
		func(c *service.BalancePrechargeResolutionCommand) { c.Action = "release" },
		func(c *service.BalancePrechargeResolutionCommand) { c.Note = " \n\t" },
		func(c *service.BalancePrechargeResolutionCommand) { c.Note = strings.Repeat("字", 2001) },
	} {
		cmd := prechargeReviewDecision("invalid")
		mutate(cmd)
		_, err := repo.ResolveBalancePrechargeReview(ctx, cmd)
		require.ErrorIs(t, err, service.ErrBalancePrechargeReviewInvalid)
	}
}

func TestBalancePrechargeReviewRepository_InvalidEvidenceAndListRejectedBeforeSQL(t *testing.T) {
	repo := prechargeReviewRepo(t, &sql.DB{})
	ctx := context.Background()
	require.ErrorIs(t, repo.MarkBalancePrechargeForReview(ctx, nil), service.ErrBalancePrechargeReviewInvalid)
	for _, mutate := range []func(*service.BalancePrechargeReviewEvidence){
		func(e *service.BalancePrechargeReviewEvidence) { e.PrechargeID = " " },
		func(e *service.BalancePrechargeReviewEvidence) { e.PrechargeID = strings.Repeat("a", 129) },
		func(e *service.BalancePrechargeReviewEvidence) { e.UserID = 0 },
		func(e *service.BalancePrechargeReviewEvidence) { e.AccountID = -1 },
		func(e *service.BalancePrechargeReviewEvidence) { e.Reason = " \n" },
		func(e *service.BalancePrechargeReviewEvidence) { e.Reason = strings.Repeat("字", 513) },
		func(e *service.BalancePrechargeReviewEvidence) { e.RequestID = strings.Repeat("字", 256) },
		func(e *service.BalancePrechargeReviewEvidence) { e.Model = strings.Repeat("字", 256) },
	} {
		evidence := prechargeReviewEvidence("invalid")
		mutate(evidence)
		require.ErrorIs(t, repo.MarkBalancePrechargeForReview(ctx, evidence), service.ErrBalancePrechargeReviewInvalid)
	}
	for _, test := range []struct {
		status        string
		limit, offset int
	}{{"unknown", 20, 0}, {"all", 0, 0}, {"all", 201, 0}, {"pending", 20, -1}} {
		_, _, err := repo.ListBalancePrechargeReviews(ctx, test.status, test.limit, test.offset)
		require.ErrorIs(t, err, service.ErrBalancePrechargeReviewInvalid)
	}
}

func TestBalancePrechargeReviewRepository_UnicodeAuditBoundariesAreAccepted(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	ctx := context.Background()
	_, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(ctx, prechargeCommand("unicode"))
	require.NoError(t, err)
	repo := prechargeReviewRepo(t, db)
	evidence := prechargeReviewEvidence("unicode")
	evidence.Reason = strings.Repeat("因", 512)
	evidence.RequestID = strings.Repeat("求", 255)
	evidence.Model = strings.Repeat("型", 255)
	require.NoError(t, repo.MarkBalancePrechargeForReview(ctx, evidence))
	cmd := prechargeReviewDecision("unicode")
	cmd.Note = strings.Repeat("已", 2000)
	result, err := repo.ResolveBalancePrechargeReview(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, cmd.Note, result.Note)
	require.Equal(t, evidence.Reason, result.Reason)
}

func TestBalancePrechargeReviewRepository_MigrationRejectsIncompleteResolutionAudit(t *testing.T) {
	db := newBalancePrechargeTestDB(t)
	createPendingPrechargeReview(t, db, "audit-check")
	for _, update := range []string{
		`resolved_at=NOW()`,
		`actual_cost=0`,
		`resolved_at=NOW(), resolved_by=99, resolution='release', actual_cost=0, note='verified'`,
		`resolved_at=NOW(), resolved_by=99, resolution='release', actual_cost=1, note='verified', new_balance=5`,
		`resolved_at=NOW(), resolved_by=99, resolution='charge', actual_cost=0, note='verified', new_balance=5`,
		`resolved_at=NOW(), resolved_by=99, resolution='charge', actual_cost=1000001, note='verified', new_balance=-1`,
		`resolved_at=NOW(), resolved_by=0, resolution='release', actual_cost=0, note='verified', new_balance=5`,
		`resolved_at=NOW(), resolved_by=99, resolution='release', actual_cost=0, note=' ', new_balance=5`,
	} {
		_, err := db.Exec(`UPDATE balance_precharge_reviews SET ` + update + ` WHERE precharge_id='audit-check'`)
		var pgErr *pq.Error
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, pq.ErrorCode("23514"), pgErr.Code, "invalid audit must fail the database CHECK constraint")
	}
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM balance_precharge_reviews WHERE resolved_at IS NULL AND actual_cost IS NULL`).Scan(&count))
	require.Equal(t, 1, count)
}
