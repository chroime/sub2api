package service

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type balancePrechargeSettingsRepo struct {
	SettingRepository
	mu       sync.Mutex
	values   map[string]string
	readErr  error
	writeErr error
	reads    int
}

func (r *balancePrechargeSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if r.readErr != nil {
		return "", r.readErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *balancePrechargeSettingsRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

type balancePrechargeSettingsGroupReader struct{}

func (balancePrechargeSettingsGroupReader) GetByID(_ context.Context, id int64) (*Group, error) {
	if id == 404 {
		return nil, ErrGroupNotFound
	}
	return &Group{ID: id}, nil
}

func newBalancePrechargeSettingsService(repo *balancePrechargeSettingsRepo) *SettingService {
	svc := NewSettingService(repo, nil)
	svc.SetDefaultSubscriptionGroupReader(balancePrechargeSettingsGroupReader{})
	return svc
}

func TestBalancePrechargeSettingsDefaultsAndGroupInheritance(t *testing.T) {
	ctx := context.Background()
	svc := newBalancePrechargeSettingsService(&balancePrechargeSettingsRepo{})
	settings, err := svc.GetBalancePrechargeSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, BalancePrechargeSettings{}, settings)
	group, err := svc.GetGroupBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, GroupBalancePrechargeSettings{Mode: "inherit"}, group)

	require.NoError(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1.25}))
	effective, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1.25}, effective)
	require.NoError(t, svc.SetGroupBalancePrechargeSettings(ctx, 7, GroupBalancePrechargeSettings{Mode: "custom", Threshold: 8, Amount: 2}))
	effective, err = svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, BalancePrechargeSettings{Enabled: true, Threshold: 8, Amount: 2}, effective)

	require.NoError(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{Threshold: 10, Amount: 1.25}))
	effective, err = svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.False(t, effective.Enabled)
	require.NoError(t, svc.SetGroupBalancePrechargeSettings(ctx, 7, GroupBalancePrechargeSettings{Mode: "inherit"}))
	require.NoError(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1.25}))
	effective, err = svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1.25}, effective)
}

func TestBalancePrechargeSettingsValidatesMonetaryBoundaries(t *testing.T) {
	for _, test := range []struct {
		name              string
		threshold, amount float64
		valid             bool
	}{
		{"zero", 0, 0, false}, {"equal", 1, 1, true}, {"amount exceeds threshold", 1, 2, false},
		{"smallest unit", 0.00000001, 0.00000001, true}, {"eight decimals", 10.12345678, 1.12345678, true},
		{"nine decimals", 1, 0.000000001, false}, {"fractional threshold", 1.000000001, 1, false},
		{"negative", -1, 1, false}, {"nan", 1, math.NaN(), false}, {"infinite", math.Inf(1), 1, false},
		{"maximum", 1000000, 1000000, true}, {"too large", 1000000.00000001, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := newBalancePrechargeSettingsService(&balancePrechargeSettingsRepo{})
			err := svc.SetBalancePrechargeSettings(context.Background(), BalancePrechargeSettings{Enabled: true, Threshold: test.threshold, Amount: test.amount})
			groupErr := svc.SetGroupBalancePrechargeSettings(context.Background(), 7, GroupBalancePrechargeSettings{Mode: "custom", Threshold: test.threshold, Amount: test.amount})
			if test.valid {
				require.NoError(t, err)
				require.NoError(t, groupErr)
			} else {
				require.Error(t, err)
				require.Error(t, groupErr)
			}
		})
	}
	svc := newBalancePrechargeSettingsService(&balancePrechargeSettingsRepo{})
	require.Error(t, svc.SetGroupBalancePrechargeSettings(context.Background(), 7, GroupBalancePrechargeSettings{Mode: "disabled"}))
	require.Error(t, svc.SetBalancePrechargeSettings(context.Background(), BalancePrechargeSettings{Amount: math.NaN()}))
	require.Error(t, svc.SetGroupBalancePrechargeSettings(context.Background(), 0, GroupBalancePrechargeSettings{Mode: "inherit"}))
	_, err := svc.GetGroupBalancePrechargeSettings(context.Background(), 404)
	require.ErrorIs(t, err, ErrGroupNotFound)
	require.ErrorIs(t, svc.SetGroupBalancePrechargeSettings(context.Background(), 404, GroupBalancePrechargeSettings{Mode: "inherit"}), ErrGroupNotFound)
}

func TestBalancePrechargeSettingsIndependentWritesAndPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	repo := &balancePrechargeSettingsRepo{}
	svc := newBalancePrechargeSettingsService(repo)
	other := newBalancePrechargeSettingsService(repo)
	require.NoError(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1}))
	require.NoError(t, svc.SetGroupBalancePrechargeSettings(ctx, 7, GroupBalancePrechargeSettings{Mode: "custom", Threshold: 7, Amount: 2}))
	require.NoError(t, other.SetGroupBalancePrechargeSettings(ctx, 8, GroupBalancePrechargeSettings{Mode: "custom", Threshold: 8, Amount: 3}))
	group, err := other.GetGroupBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, GroupBalancePrechargeSettings{Mode: "custom", Threshold: 7, Amount: 2}, group)
	require.Len(t, repo.values, 3)
	repo.writeErr = errors.New("database offline")
	require.Error(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{}))
	require.Error(t, svc.SetGroupBalancePrechargeSettings(ctx, 7, GroupBalancePrechargeSettings{Mode: "inherit"}))
	effective, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, BalancePrechargeSettings{Enabled: true, Threshold: 7, Amount: 2}, effective)
}

func TestBalancePrechargeSettingsRuntimeRefreshAndErrors(t *testing.T) {
	ctx := context.Background()
	repo := &balancePrechargeSettingsRepo{}
	svc := newBalancePrechargeSettingsService(repo)
	other := newBalancePrechargeSettingsService(repo)
	require.NoError(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1}))
	_, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.NoError(t, other.SetGroupBalancePrechargeSettings(ctx, 7, GroupBalancePrechargeSettings{Mode: "custom", Threshold: 6, Amount: 2}))
	before, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, float64(1), before.Amount)
	svc.balancePrechargeMu.Lock()
	svc.balancePrechargeGroups[7] = cachedGroupBalancePrechargeSettings{expiresAt: time.Now().Add(-time.Second)}
	svc.balancePrechargeMu.Unlock()
	after, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, float64(2), after.Amount)
	require.LessOrEqual(t, time.Until(svc.balancePrechargeGroups[7].expiresAt), 5*time.Second)

	repo.readErr = errors.New("database offline")
	svc.balancePrechargeMu.Lock()
	svc.balancePrechargeGlobal.expiresAt = time.Now().Add(-time.Second)
	svc.balancePrechargeMu.Unlock()
	_, err = svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.ErrorContains(t, err, "database offline")
	_, err = svc.GetBalancePrechargeSettings(ctx)
	require.Error(t, err)
	_, err = svc.GetGroupBalancePrechargeSettings(ctx, 7)
	require.Error(t, err)

	repo.readErr = nil
	require.NoError(t, other.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{}))
	disabled, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
}

func TestBalancePrechargeSettingsRejectsCorruptPersistedPolicy(t *testing.T) {
	for _, raw := range []string{"", "null", `{}`, `{"enabled":true}`, `{"enabled":false,"threshold":null,"amount":0}`, `{"enabled":true,"threshold":1,"amount":2}`} {
		t.Run(raw, func(t *testing.T) {
			svc := newBalancePrechargeSettingsService(&balancePrechargeSettingsRepo{values: map[string]string{SettingKeyBalancePrecharge: raw}})
			_, err := svc.GetEffectiveBalancePrechargeSettings(context.Background(), 7)
			require.Error(t, err)
		})
	}
}

func TestBalancePrechargeSettingsCorruptGroupDoesNotFallBackToGlobal(t *testing.T) {
	ctx := context.Background()
	repo := &balancePrechargeSettingsRepo{values: map[string]string{
		"balance_precharge":         `{"enabled":true,"threshold":10,"amount":1}`,
		"balance_precharge_group_7": `{"mode":"custom","threshold":5,"amount":null}`,
	}}
	svc := newBalancePrechargeSettingsService(repo)
	_, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.Error(t, err)
	otherGroup, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 8)
	require.NoError(t, err)
	require.Equal(t, BalancePrechargeSettings{Enabled: true, Threshold: 10, Amount: 1}, otherGroup)
	require.NoError(t, svc.SetBalancePrechargeSettings(ctx, BalancePrechargeSettings{}))
	disabled, err := svc.GetEffectiveBalancePrechargeSettings(ctx, 7)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
}
