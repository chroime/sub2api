//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceCreateGroupUltraFastTakesPrecedenceOverFast(t *testing.T) {
	input := CreateGroupInput{Name: "ultrafast", Platform: PlatformOpenAI, RateMultiplier: 1, ForceOpenAIFast: true, ForceOpenAIUltrafast: true}
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	group, err := svc.CreateGroup(context.Background(), &input)
	require.NoError(t, err)
	require.True(t, group.ForceOpenAIUltrafast)
	require.False(t, group.ForceOpenAIFast, "creating an UltraFast group must clear Fast even when both flags are supplied")
}

func TestAdminServiceUpdateGroupUltraFastClearsExistingFast(t *testing.T) {
	existing := &Group{ID: 1, Name: "fast", Platform: PlatformOpenAI, Status: StatusActive, ForceOpenAIFast: true}
	repo := &groupRepoStubForAdmin{getByID: existing}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{groupRepo: repo, authCacheInvalidator: invalidator}
	enabled := true
	input := UpdateGroupInput{ForceOpenAIUltrafast: &enabled}
	group, err := svc.UpdateGroup(context.Background(), 1, &input)
	require.NoError(t, err)
	require.True(t, group.ForceOpenAIUltrafast)
	require.False(t, group.ForceOpenAIFast, "enabling UltraFast must clear previously saved Fast")
	require.Equal(t, []int64{1}, invalidator.groupIDs)
}

func TestAdminServiceCreateGroupUltraFastOnlySupportsOpenAI(t *testing.T) {
	for _, tc := range []struct {
		platform  string
		wantUltra bool
		wantFast  bool
		wantFree  bool
	}{
		{PlatformOpenAI, true, false, true},
		{PlatformComposite, false, true, true},
		{PlatformAnthropic, false, false, false},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			svc := &adminServiceImpl{groupRepo: &groupRepoStubForAdmin{}}
			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name: "ultrafast", Platform: tc.platform, RateMultiplier: 1,
				ForceOpenAIFast: true, ForceOpenAIUltrafast: true, FreeOpenAIFast: true,
			})
			require.NoError(t, err)
			require.Equal(t, tc.wantUltra, group.ForceOpenAIUltrafast)
			require.Equal(t, tc.wantFast, group.ForceOpenAIFast)
			require.Equal(t, tc.wantFree, group.FreeOpenAIFast)
		})
	}
}

func TestAdminServiceUpdateGroupUltraFastTransitions(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name      string
		ultra     bool
		fast      bool
		input     UpdateGroupInput
		wantUltra bool
		wantFast  bool
		wantFree  bool
	}{
		{"fast alone replaces ultrafast", true, false, UpdateGroupInput{ForceOpenAIFast: &on}, false, true, true},
		{"both enabled prefer ultrafast", false, false, UpdateGroupInput{ForceOpenAIFast: &on, ForceOpenAIUltrafast: &on}, true, false, true},
		{"disabling fast preserves ultrafast", true, false, UpdateGroupInput{ForceOpenAIFast: &off}, true, false, true},
		{"explicit ultra off preserves fast", false, true, UpdateGroupInput{ForceOpenAIUltrafast: &off}, false, true, true},
		{"omission preserves ultrafast", true, false, UpdateGroupInput{Name: "renamed"}, true, false, true},
		{"disable ultrafast", true, false, UpdateGroupInput{ForceOpenAIUltrafast: &off}, false, false, true},
		{"composite clears ultra but preserves fast", true, true, UpdateGroupInput{Platform: PlatformComposite}, false, true, true},
		{"anthropic clears all policies", true, true, UpdateGroupInput{Platform: PlatformAnthropic}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByID: &Group{
				ID: 1, Name: "policy", Platform: PlatformOpenAI, Status: StatusActive,
				ForceOpenAIUltrafast: tc.ultra, ForceOpenAIFast: tc.fast, FreeOpenAIFast: true,
			}}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.UpdateGroup(context.Background(), 1, &tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.wantUltra, group.ForceOpenAIUltrafast)
			require.Equal(t, tc.wantFast, group.ForceOpenAIFast)
			require.Equal(t, tc.wantFree, group.FreeOpenAIFast)
		})
	}
}

func TestDuplicateGroupPreservesUltraFastPolicy(t *testing.T) {
	source := &Group{ID: 41, Name: "ultrafast", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1, ForceOpenAIUltrafast: true}
	repo := newDuplicateGroupRepoStub(source)
	svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}
	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:7", "ultrafast-copy")
	require.NoError(t, err)
	require.True(t, duplicate.ForceOpenAIUltrafast)
	require.False(t, duplicate.ForceOpenAIFast)
	require.True(t, source.ForceOpenAIUltrafast)
	require.Equal(t, duplicateGroupInactiveStatus, duplicate.Status)
}
