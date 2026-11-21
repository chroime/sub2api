//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lanePrechargeTestRepo struct {
	*laneTestRepo
	*prechargeWalletFixture
	recoveryCalls []string
	reviewCalls   []string
}

func (r *lanePrechargeTestRepo) RenewBalancePrechargeLease(context.Context, string, string) (bool, error) {
	r.recoveryCalls = append(r.recoveryCalls, "renew")
	return true, nil
}

func (r *lanePrechargeTestRepo) EndBalancePrechargeLease(context.Context, string, string) error {
	r.recoveryCalls = append(r.recoveryCalls, "end")
	return nil
}

func (r *lanePrechargeTestRepo) RecoverBalancePrecharges(context.Context, int) (int, error) {
	r.recoveryCalls = append(r.recoveryCalls, "recover")
	return 2, nil
}

func (r *lanePrechargeTestRepo) ArchiveBalancePrecharges(context.Context, time.Time, int) (int, error) {
	r.recoveryCalls = append(r.recoveryCalls, "archive")
	return 3, nil
}

func (r *lanePrechargeTestRepo) MarkBalancePrechargeForReview(context.Context, *BalancePrechargeReviewEvidence) error {
	r.reviewCalls = append(r.reviewCalls, "mark")
	return nil
}

func (r *lanePrechargeTestRepo) ListBalancePrechargeReviews(context.Context, string, int, int) ([]*BalancePrechargeReview, int64, error) {
	r.reviewCalls = append(r.reviewCalls, "list")
	return []*BalancePrechargeReview{{ID: "review"}}, 1, nil
}

func (r *lanePrechargeTestRepo) ResolveBalancePrechargeReview(context.Context, *BalancePrechargeResolutionCommand) (*BalancePrechargeReview, error) {
	r.reviewCalls = append(r.reviewCalls, "resolve")
	return &BalancePrechargeReview{ID: "review"}, nil
}

func TestUsageBillingProviderPreservesBalancePrechargeAdmission(t *testing.T) {
	for _, gateway := range []string{"native", "openai"} {
		t.Run(gateway, func(t *testing.T) {
			repo := &lanePrechargeTestRepo{
				laneTestRepo: newLaneTestRepo(),
				prechargeWalletFixture: &prechargeWalletFixture{
					balance: .2,
					holds:   make(map[string]float64),
				},
			}
			lanes := NewUsageBillingLaneCombiner(repo)
			t.Cleanup(lanes.Stop)
			provided := ProvideUsageBillingRepository(lanes)
			settings := newBalancePrechargeSettingsService(&balancePrechargeSettingsRepo{})
			require.NoError(t, settings.SetBalancePrechargeSettings(context.Background(), BalancePrechargeSettings{Enabled: true, Threshold: 1, Amount: .1}))
			ctx := context.Background()
			if gateway == "native" {
				service := &GatewayService{usageBillingRepo: provided, settingService: settings}
				ctx = service.WithBalancePrecharge(ctx)
			} else {
				service := &OpenAIGatewayService{usageBillingRepo: provided, settingService: settings}
				ctx = service.WithBalancePrecharge(ctx)
			}
			key := &APIKey{ID: 2, UserID: 1, User: &User{ID: 1}}
			require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
			require.True(t, HasBalancePrecharge(ctx))
			require.InDelta(t, .1, repo.balance, 1e-8)
			require.InDelta(t, .1, repo.frozen, 1e-8)
			FinishBalancePrecharge(ctx)
			require.InDelta(t, .2, repo.balance, 1e-8)
			require.Zero(t, repo.frozen)
		})
	}
}

func TestUsageBillingProviderPreservesPrechargeMaintenanceAndReviews(t *testing.T) {
	repo := &lanePrechargeTestRepo{laneTestRepo: newLaneTestRepo(), prechargeWalletFixture: &prechargeWalletFixture{}}
	lanes := NewUsageBillingLaneCombiner(repo)
	t.Cleanup(lanes.Stop)
	provided := ProvideUsageBillingRepository(lanes)
	recovery, ok := provided.(BalancePrechargeRecoveryRepository)
	require.True(t, ok, "billing provider must expose durable precharge recovery")
	alive, err := recovery.RenewBalancePrechargeLease(context.Background(), "hold", "owner")
	require.NoError(t, err)
	require.True(t, alive)
	require.NoError(t, recovery.EndBalancePrechargeLease(context.Background(), "hold", "owner"))
	maintenance := NewBillingMaintenanceService(provided, nil)
	require.NoError(t, maintenance.RunOnce(context.Background()))
	require.Equal(t, []string{"renew", "end", "recover", "archive"}, repo.recoveryCalls)

	reviews, ok := provided.(BalancePrechargeReconciliationRepository)
	require.True(t, ok, "billing provider must expose durable precharge reconciliation")
	require.NoError(t, reviews.MarkBalancePrechargeForReview(context.Background(), &BalancePrechargeReviewEvidence{}))
	items, total, err := reviews.ListBalancePrechargeReviews(context.Background(), "pending", 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "review", items[0].ID)
	resolved, err := reviews.ResolveBalancePrechargeReview(context.Background(), &BalancePrechargeResolutionCommand{})
	require.NoError(t, err)
	require.Equal(t, "review", resolved.ID)
	require.Equal(t, []string{"mark", "list", "resolve"}, repo.reviewCalls)
	_, err = provided.Apply(context.Background(), laneTestCommand(1, "normal-usage", .01))
	require.NoError(t, err)
	require.EqualValues(t, 1, lanes.Stats().SingleApplies)
}

func TestUsageBillingProviderRetainsRepositoryWithoutPrecharge(t *testing.T) {
	lanes := NewUsageBillingLaneCombiner(newLaneTestRepo())
	t.Cleanup(lanes.Stop)
	provided := ProvideUsageBillingRepository(lanes)
	_, supported := provided.(BalancePrechargeRepository)
	require.False(t, supported)
	_, err := provided.Apply(context.Background(), laneTestCommand(1, "normal-usage", .01))
	require.NoError(t, err)
}
