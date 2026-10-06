package upstreamgovernance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type rechargeMemoryStore struct {
	*balanceTestStore
	record *RechargeRecord
	writes int
}

func cloneRecharge(record *RechargeRecord) *RechargeRecord {
	if record == nil {
		return nil
	}
	raw, _ := json.Marshal(record)
	var result RechargeRecord
	_ = json.Unmarshal(raw, &result)
	return &result
}

func (m *rechargeMemoryStore) GetRechargeRecord(context.Context, int64) (*RechargeRecord, error) {
	return cloneRecharge(m.record), nil
}

func (m *rechargeMemoryStore) SaveRechargePolicy(_ context.Context, record *RechargeRecord, version int64) error {
	current := int64(0)
	if m.record != nil {
		current = m.record.Version
	}
	if current != version {
		return ErrConflict
	}
	record.Version = current + 1
	m.record = cloneRecharge(record)
	m.writes++
	return nil
}

func (m *rechargeMemoryStore) SaveRechargeEvaluation(_ context.Context, id, version int64, state RechargeState) error {
	if m.record == nil || m.record.SiteID != id || m.record.Version != version {
		return ErrConflict
	}
	m.record.State = state
	m.record = cloneRecharge(m.record)
	m.writes++
	return nil
}

func rechargeEngine(t *testing.T) (*Service, *rechargeMemoryStore, *fakeConnector, *balanceTestNotifier, *time.Time) {
	t.Helper()
	svc, balanceStore, connector, notifier, now := balanceEngine(t)
	balanceStore.site.BalanceMonitor.Enabled = false
	store := &rechargeMemoryStore{balanceTestStore: balanceStore}
	svc.store = store
	return svc, store, connector, notifier, now
}

func TestRechargePlanOnlyPersistsAndEvaluatesWithoutPaymentsOrMail(t *testing.T) {
	svc, store, _, notifier, _ := rechargeEngine(t)
	initial, err := svc.RechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, initial.Version)
	require.Equal(t, "disabled", initial.Policy.Mode)
	policy := initial.Policy
	policy.Mode = "plan_only"
	siteVersion := store.site.Version
	saved, err := svc.ConfigureRechargePlan(t.Context(), 1, 0, policy)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Version)
	require.Equal(t, siteVersion, store.site.Version)
	require.Nil(t, saved.Evaluation)
	require.Equal(t, "blocked", saved.Status)
	require.False(t, saved.Capability.Available)
	require.Equal(t, "provider_unavailable", saved.Capability.Reason)
	_, err = svc.ConfigureRechargePlan(t.Context(), 1, 0, policy)
	require.ErrorIs(t, err, ErrConflict)
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.NotNil(t, store.record.State.Evaluation, "successful collection automatically evaluates plan_only")
	first, err := svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, first.Evaluation.PolicyMatched)
	require.Contains(t, first.Evaluation.Reasons, "provider_unavailable")
	second, err := svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, first.Evaluation.ID, second.Evaluation.ID)
	require.Equal(t, first.Evaluation.EpisodeID, second.Evaluation.EpisodeID)
	require.Empty(t, notifier.sent)
	writes := store.writes
	_, err = svc.RechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, writes, store.writes)
}

func TestRechargeRejectsExecutionAndBlocksStaleVersionBudgetAndChangedWallet(t *testing.T) {
	svc, store, connector, _, now := rechargeEngine(t)
	policy := defaultRechargePolicy("sub2api")
	policy.Mode = "execute"
	_, err := svc.ConfigureRechargePlan(t.Context(), 1, 0, policy)
	require.ErrorIs(t, err, ErrInvalid)
	policy.Mode = "plan_only"
	policy.DailyBudgetMinor = policy.AmountMinor - 1
	_, err = svc.ConfigureRechargePlan(t.Context(), 1, 0, policy)
	require.NoError(t, err)
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	result, err := svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, result.Evaluation.PolicyMatched)
	require.Contains(t, result.Evaluation.Reasons, "daily_budget_exceeded")
	episode := result.Evaluation.EpisodeID
	*now = now.Add(31 * time.Minute)
	result, err = svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Contains(t, result.Evaluation.Reasons, "balance_stale")
	require.Equal(t, episode, result.Evaluation.EpisodeID)
	store.site.Version++
	result, err = svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Contains(t, result.Evaluation.Reasons, "snapshot_outdated")
	store.site.BaseURL = "https://changed.example"
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	result, err = svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.NotEqual(t, episode, result.Evaluation.EpisodeID)
	first := result.Evaluation.EpisodeID
	above := 20.0
	connector.catalog.Account.Balance = &above
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	below := 5.0
	connector.catalog.Account.Balance = &below
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.NotEqual(t, first, store.record.State.Evaluation.EpisodeID)
}

func TestRechargePolicyAndAuthenticatedWalletChangesIsolateEpisodes(t *testing.T) {
	svc, store, connector, _, _ := rechargeEngine(t)
	policy := defaultRechargePolicy("sub2api")
	policy.Mode = "plan_only"
	_, err := svc.ConfigureRechargePlan(t.Context(), 1, 0, policy)
	require.NoError(t, err)
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	first := store.record.State.EpisodeID
	policy.Threshold = 11
	_, err = svc.ConfigureRechargePlan(t.Context(), 1, 1, policy)
	require.NoError(t, err)
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	second := store.record.State.EpisodeID
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second)
	store.site.SessionCipher, err = svc.cipher.Encrypt(`{"access_token":"other-fixture-session","user_id":6}`)
	require.NoError(t, err)
	store.site.Version++
	writes := store.writes
	read, err := svc.RechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Nil(t, read.Evaluation, "a new wallet must not expose the old wallet's simulation")
	require.Equal(t, writes, store.writes)
	blocked, err := svc.EvaluateRechargePlan(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, blocked.Evaluation.EpisodeID)
	require.Contains(t, blocked.Evaluation.Reasons, "snapshot_outdated")
	connector.catalog.Account.UserID = 6
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.NotEqual(t, second, store.record.State.EpisodeID)
	third := store.record.State.EpisodeID
	connector.catalog.Account.Unit = "quota"
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, store.record.State.EpisodeID)
	require.Contains(t, store.record.State.Evaluation.Reasons, "balance_unit_changed")
	connector.catalog.Account.Unit = "usd"
	_, err = svc.Sync(t.Context(), 1)
	require.NoError(t, err)
	require.NotEqual(t, third, store.record.State.EpisodeID)
}

func TestRechargePolicyMoneyAndModeValidation(t *testing.T) {
	for name, change := range map[string]func(*RechargePolicy){
		"execution":            func(p *RechargePolicy) { p.Mode = "execute" },
		"zero-amount":          func(p *RechargePolicy) { p.AmountMinor = 0 },
		"excessive-amount":     func(p *RechargePolicy) { p.AmountMinor = maxRechargeMinor + 1 },
		"negative-budget":      func(p *RechargePolicy) { p.DailyBudgetMinor = -1 },
		"unsupported-currency": func(p *RechargePolicy) { p.Currency = "BTC" },
		"foreign-unit":         func(p *RechargePolicy) { p.Unit = "quota" },
		"zero-cooldown":        func(p *RechargePolicy) { p.CooldownMinutes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			policy := defaultRechargePolicy("sub2api")
			change(&policy)
			_, err := normalizeRechargePolicy(policy, "sub2api")
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}
