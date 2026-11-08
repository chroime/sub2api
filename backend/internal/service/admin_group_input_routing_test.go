//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type inputRoutingAccountRepoStub struct {
	AccountRepository
	accounts []*Account
}

func (r *inputRoutingAccountRepoStub) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return r.accounts, nil
}

func inputRoutingTestConfig() GroupInputRoutingConfig {
	return GroupInputRoutingConfig{Enabled: true, Rules: []GroupInputRoutingRule{{
		ID: "small", Name: "Small inputs", Enabled: true, RequestType: "all", InputTokensMode: "range", MaxInputTokens: 2000,
		AccountIDs: []int64{11}, GroupIDs: []int64{21}, FallbackToOriginal: true,
	}}}
}

func TestAdminGroupInputRoutingCreateUpdateClear(t *testing.T) {
	ctx := context.Background()
	config := inputRoutingTestConfig()
	repo := &groupRepoStubForAdmin{createID: 7, getByIDByID: map[int64]*Group{21: {ID: 21, Platform: PlatformAnthropic}}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: &inputRoutingAccountRepoStub{accounts: []*Account{{ID: 11, Platform: PlatformGemini}, {ID: 12, Platform: PlatformGrok}}}, authCacheInvalidator: invalidator}
	created, err := svc.CreateGroup(ctx, &CreateGroupInput{Name: "source", Platform: PlatformOpenAI, RateMultiplier: 1, InputRouting: config})
	require.NoError(t, err)
	require.Equal(t, config, created.InputRouting)
	config.Rules[0].AccountIDs[0] = 98
	require.Equal(t, int64(11), created.InputRouting.Rules[0].AccountIDs[0])
	repo.getByIDByID[7] = created
	preserved, err := svc.UpdateGroup(ctx, 7, &UpdateGroupInput{Name: "renamed"})
	require.NoError(t, err)
	require.True(t, preserved.InputRouting.Enabled)
	require.Equal(t, int64(11), preserved.InputRouting.Rules[0].AccountIDs[0])
	updatedConfig := inputRoutingTestConfig()
	updatedConfig.Rules[0].AccountIDs = []int64{12}
	updated, err := svc.UpdateGroup(ctx, 7, &UpdateGroupInput{InputRouting: &updatedConfig})
	require.NoError(t, err)
	require.Equal(t, updatedConfig, updated.InputRouting)
	require.Equal(t, PlatformOpenAI, updated.Platform, "saving targets must not change the source platform")
	cleared, err := svc.UpdateGroup(ctx, 7, &UpdateGroupInput{InputRouting: &GroupInputRoutingConfig{}})
	require.NoError(t, err)
	require.False(t, cleared.InputRouting.Enabled)
	require.Empty(t, cleared.InputRouting.Rules)
	require.NotNil(t, cleared.InputRouting.Rules)
	require.Equal(t, []int64{7, 7, 7}, invalidator.groupIDs)
}

func TestAdminGroupInputRoutingRejectsTargetsBeforePersistence(t *testing.T) {
	for name, targetAccountPlatform := range map[string]string{"missing account": "", "unknown account platform": "unknown", "composite account platform": PlatformComposite} {
		t.Run(name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{21: {ID: 21, Platform: PlatformOpenAI}}}
			accounts := &inputRoutingAccountRepoStub{}
			if targetAccountPlatform != "" {
				accounts.accounts = []*Account{{ID: 11, Platform: targetAccountPlatform}}
			}
			svc := &adminServiceImpl{groupRepo: repo, accountRepo: accounts}
			_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "source", Platform: PlatformOpenAI, RateMultiplier: 1, InputRouting: inputRoutingTestConfig()})
			require.Error(t, err)
			require.Nil(t, repo.created)
		})
	}
	for _, tc := range []struct {
		name, source, target string
		sourceID             int64
		valid                bool
	}{
		{"same platform", PlatformOpenAI, PlatformOpenAI, 7, true},
		{"self group", PlatformOpenAI, PlatformOpenAI, 21, false},
		{"missing group", PlatformOpenAI, "", 7, false},
		{"different platform", PlatformOpenAI, PlatformAnthropic, 7, true},
		{"concrete to composite", PlatformOpenAI, PlatformComposite, 7, true},
		{"composite to concrete", PlatformComposite, PlatformAnthropic, 7, true},
		{"composite to composite", PlatformComposite, PlatformComposite, 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByIDByID: make(map[int64]*Group)}
			if tc.target != "" {
				repo.getByIDByID[21] = &Group{ID: 21, Platform: tc.target}
			}
			config := inputRoutingTestConfig()
			config.Rules[0].AccountIDs = nil
			svc := &adminServiceImpl{groupRepo: repo}
			_, err := svc.normalizeAndValidateGroupInputRouting(context.Background(), tc.sourceID, tc.source, config)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestAdminGroupInputRoutingAcceptsEveryRegisteredTargetPlatform(t *testing.T) {
	platforms := append(domain.ConcretePlatformIDs(), PlatformComposite)
	for _, source := range platforms {
		for _, target := range platforms {
			t.Run(source+"/"+target, func(t *testing.T) {
				config := inputRoutingTestConfig()
				accounts := &inputRoutingAccountRepoStub{}
				if target == PlatformComposite {
					config.Rules[0].AccountIDs = nil
				} else {
					accounts.accounts = []*Account{{ID: 11, Platform: target}}
				}
				repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{21: {ID: 21, Platform: target}}}
				svc := &adminServiceImpl{groupRepo: repo, accountRepo: accounts}
				got, err := svc.normalizeAndValidateGroupInputRouting(context.Background(), 7, source, config)
				require.NoError(t, err)
				require.Equal(t, CloneGroupInputRouting(config), got)
			})
		}
	}
}

func TestAdminGroupInputRoutingPlatformChangePreservesCrossPlatformTargets(t *testing.T) {
	config := inputRoutingTestConfig()
	config.Rules[0].AccountIDs = nil
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{
		7:  {ID: 7, Platform: PlatformOpenAI, InputRouting: config},
		21: {ID: 21, Platform: PlatformOpenAI},
	}}
	svc := &adminServiceImpl{groupRepo: repo}
	updated, err := svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{Platform: PlatformAnthropic})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, PlatformAnthropic, updated.Platform)
	require.Equal(t, CloneGroupInputRouting(config), updated.InputRouting)
}

func TestCloneGroupInputRoutingForDuplicateOwnsTargets(t *testing.T) {
	source := &Group{ID: 7, Platform: PlatformOpenAI, InputRouting: inputRoutingTestConfig()}
	source.InputRouting.Rules[0].MinInputTokens = 50
	copy := cloneGroupForDuplicate(source, "duplicate")
	require.Equal(t, source.InputRouting, copy.InputRouting)
	require.Equal(t, 50, copy.InputRouting.Rules[0].MinInputTokens)
	copy.InputRouting.Rules[0].MinInputTokens = 75
	copy.InputRouting.Rules[0].Name = "Copy only"
	copy.InputRouting.Rules[0].AccountIDs[0] = 99
	copy.InputRouting.Rules[0].GroupIDs[0] = 99
	require.Equal(t, 50, source.InputRouting.Rules[0].MinInputTokens)
	require.Equal(t, "Small inputs", source.InputRouting.Rules[0].Name)
	require.Equal(t, []int64{11}, source.InputRouting.Rules[0].AccountIDs)
	require.Equal(t, []int64{21}, source.InputRouting.Rules[0].GroupIDs)
}
