//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type prechargeReviewFixture struct {
	*prechargeWalletFixture
	reviews                         []*BalancePrechargeReviewEvidence
	markErr, releaseErr, resolveErr error
	resolveResult                   *BalancePrechargeReview
	resolveCommand                  *BalancePrechargeResolutionCommand
	listStatus                      string
	listLimit, listOffset           int
}

func (r *prechargeReviewFixture) MarkBalancePrechargeForReview(ctx context.Context, evidence *BalancePrechargeReviewEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.markErr != nil {
		return r.markErr
	}
	copy := *evidence
	r.reviews = append(r.reviews, &copy)
	return nil
}
func (r *prechargeReviewFixture) ReleaseBalancePrecharge(ctx context.Context, id string, userID int64) (bool, error) {
	if r.releaseErr != nil {
		return false, r.releaseErr
	}
	return r.prechargeWalletFixture.ReleaseBalancePrecharge(ctx, id, userID)
}
func (r *prechargeReviewFixture) ListBalancePrechargeReviews(_ context.Context, status string, limit, offset int) ([]*BalancePrechargeReview, int64, error) {
	r.listStatus, r.listLimit, r.listOffset = status, limit, offset
	return nil, 0, nil
}
func (r *prechargeReviewFixture) ResolveBalancePrechargeReview(_ context.Context, cmd *BalancePrechargeResolutionCommand) (*BalancePrechargeReview, error) {
	copy := *cmd
	r.resolveCommand = &copy
	return r.resolveResult, r.resolveErr
}

func reviewLifecycleFixture(t *testing.T) (context.Context, *prechargeReviewFixture) {
	t.Helper()
	ctx, _, wallet, key := prechargeFixture(.2)
	r := &prechargeReviewFixture{prechargeWalletFixture: wallet}
	requestPrecharge(ctx).repo = r
	require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
	return ctx, r
}

func TestBalancePrechargeReviewPersistsOnlyAfterWorkersFinish(t *testing.T) {
	ctx, r := reviewLifecycleFixture(t)
	StartBalancePrechargeUpstream(ctx)
	ObserveBalancePrechargeUpstream(ctx, 200, nil)
	done := retainBalancePrechargeTask(ctx)
	FinishBalancePrecharge(ctx)
	require.Empty(t, r.reviews)
	BalancePrechargeUsageFinished(ctx, errors.New("billing transaction failed"))
	require.Empty(t, r.reviews, "a failed worker must release lifecycle ownership before review is persisted")
	done()
	require.Len(t, r.reviews, 1)
	require.Equal(t, "billing_failed", r.reviews[0].Reason)
	require.Equal(t, BalancePrechargeBillingID(ctx, 1, 2), r.reviews[0].PrechargeID)
	require.Equal(t, int64(1), r.reviews[0].UserID)
	require.InDelta(t, .1, r.balance, 1e-8)
	require.InDelta(t, .1, r.frozen, 1e-8)
	FinishBalancePrecharge(ctx)
	require.Len(t, r.reviews, 1)
}

func TestBalancePrechargeReviewRecordsReleaseFailure(t *testing.T) {
	ctx, r := reviewLifecycleFixture(t)
	r.releaseErr = errors.New("release failed")
	FinishBalancePrecharge(ctx)
	require.Len(t, r.reviews, 1)
	require.Equal(t, "release_failed", r.reviews[0].Reason)
	require.InDelta(t, .1, r.frozen, 1e-8)
}

func TestBalancePrechargeReconciliationValidatesBeforeResolving(t *testing.T) {
	valid := BalancePrechargeResolutionCommand{PrechargeID: uuid.NewString(), ActorID: 7, Action: "charge", ActualCost: .01, Note: "upstream invoice checked"}
	for _, mutate := range []func(*BalancePrechargeResolutionCommand){
		func(c *BalancePrechargeResolutionCommand) { c.PrechargeID = "invalid" },
		func(c *BalancePrechargeResolutionCommand) { c.ActorID = 0 },
		func(c *BalancePrechargeResolutionCommand) { c.Action = "unknown" },
		func(c *BalancePrechargeResolutionCommand) { c.Action = "release" },
		func(c *BalancePrechargeResolutionCommand) { c.ActualCost = 0 },
		func(c *BalancePrechargeResolutionCommand) { c.ActualCost = -1 },
		func(c *BalancePrechargeResolutionCommand) { c.ActualCost = math.NaN() },
		func(c *BalancePrechargeResolutionCommand) { c.ActualCost = math.Inf(1) },
		func(c *BalancePrechargeResolutionCommand) { c.ActualCost = 0.000000001 },
		func(c *BalancePrechargeResolutionCommand) { c.ActualCost = 1000001 },
		func(c *BalancePrechargeResolutionCommand) { c.Note = " \n\t" },
		func(c *BalancePrechargeResolutionCommand) { c.Note = strings.Repeat("x", 2001) },
	} {
		r := &prechargeReviewFixture{}
		s := NewBalancePrechargeReconciliationService(r, nil)
		cmd := valid
		mutate(&cmd)
		_, err := s.Resolve(context.Background(), &cmd)
		require.ErrorIs(t, err, ErrBalancePrechargeReviewInvalid)
		require.Nil(t, r.resolveCommand)
	}
}

func TestBalancePrechargeReconciliationCommitInvalidatesAndWakesWaiters(t *testing.T) {
	for _, action := range []string{"release", "charge"} {
		t.Run(action, func(t *testing.T) {
			r := &prechargeReviewFixture{resolveResult: &BalancePrechargeReview{UserID: 1, Status: "resolved"}}
			invalidated := int64(0)
			s := NewBalancePrechargeReconciliationService(r, func(ctx context.Context, userID int64) error {
				require.NoError(t, ctx.Err())
				invalidated = userID
				return errors.New("cache unavailable")
			})
			s.waiter = newBalancePrechargeWaiter()
			_, leave := s.waiter.enqueue(1)
			defer leave()
			changed := s.waiter.changed(1)
			cost := 0.0
			if action == "charge" {
				cost = .02
			}
			result, err := s.Resolve(context.Background(), &BalancePrechargeResolutionCommand{PrechargeID: uuid.NewString(), ActorID: 7, Action: action, ActualCost: cost, Note: " invoice checked "})
			require.NoError(t, err, "cache failure after commit must not misreport accounting failure")
			require.Equal(t, r.resolveResult, result)
			require.Equal(t, int64(1), invalidated)
			require.Equal(t, "invoice checked", r.resolveCommand.Note)
			select {
			case <-changed:
			case <-time.After(time.Second):
				t.Fatal("waiting request not notified")
			}
		})
	}
}

func TestBalancePrechargeReconciliationConflictDoesNotInvalidate(t *testing.T) {
	r := &prechargeReviewFixture{resolveErr: ErrBalancePrechargeReviewConflict}
	s := NewBalancePrechargeReconciliationService(r, func(context.Context, int64) error { t.Fatal("uncommitted resolution invalidated cache"); return nil })
	_, err := s.Resolve(context.Background(), &BalancePrechargeResolutionCommand{PrechargeID: uuid.NewString(), ActorID: 7, Action: "release", Note: "no upstream charge"})
	require.ErrorIs(t, err, ErrBalancePrechargeReviewConflict)
}

func TestBalancePrechargeReconciliationListValidation(t *testing.T) {
	r := &prechargeReviewFixture{}
	s := NewBalancePrechargeReconciliationService(r, nil)
	items, total, err := s.List(context.Background(), "", 20, 20)
	require.NoError(t, err)
	require.NotNil(t, items)
	require.Zero(t, total)
	require.Equal(t, "pending", r.listStatus)
	require.Equal(t, 20, r.listLimit)
	require.Equal(t, 20, r.listOffset)
	for _, status := range []string{"pending", "resolved", "settled", "all"} {
		_, _, err = s.List(context.Background(), status, 20, 0)
		require.NoError(t, err)
	}
	_, _, err = s.List(context.Background(), "bad", 20, 0)
	require.ErrorIs(t, err, ErrBalancePrechargeReviewInvalid)
}
