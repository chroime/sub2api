//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAdminCreateGroupStreamingACKDefaultsDisabledForEveryPlatform(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "new", Platform: platform, RateMultiplier: 1})
			require.NoError(t, err)
			require.NotNil(t, group.StreamingACKEnabled)
			require.False(t, *group.StreamingACKEnabled)
		})
	}
}

func TestAdminGroupStreamingACKRemainsConfigurableInSimpleMode(t *testing.T) {
	enabled := true
	repo := &groupRepoStubForAdmin{getByID: &Group{ID: 7, Name: "legacy", Platform: PlatformAnthropic}}
	svc := &adminServiceImpl{groupRepo: repo, cfg: &config.Config{RunMode: config.RunModeSimple}}
	created, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "new", Platform: PlatformAnthropic, StreamingACKEnabled: true})
	require.NoError(t, err)
	require.Equal(t, &enabled, created.StreamingACKEnabled)
	updated, err := svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{StreamingACKEnabled: &enabled})
	require.NoError(t, err)
	require.Equal(t, &enabled, updated.StreamingACKEnabled)
}

func TestCloneGroupStreamingACKPreservesPolicyAndOwnsPointer(t *testing.T) {
	for _, value := range []*bool{nil, groupDuplicateTestPointer(false), groupDuplicateTestPointer(true)} {
		source := &Group{Name: "source", StreamingACKEnabled: value}
		duplicate := cloneGroupForDuplicate(source, "operation")
		require.Equal(t, value, duplicate.StreamingACKEnabled)
		if value != nil {
			original := *value
			*duplicate.StreamingACKEnabled = !original
			require.Equal(t, original, *source.StreamingACKEnabled)
		}
	}
}

func TestAdminCreateGroupStreamingACKAcceptsExplicitPolicyForEveryPlatform(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformComposite} {
		for _, enabled := range []bool{false, true} {
			t.Run(platform, func(t *testing.T) {
				repo := &groupRepoStubForAdmin{}
				svc := &adminServiceImpl{groupRepo: repo}
				input := &CreateGroupInput{Name: "new", Platform: platform, RateMultiplier: 1, StreamingACKEnabled: enabled}
				group, err := svc.CreateGroup(context.Background(), input)
				require.NoError(t, err)
				require.Equal(t, &enabled, group.StreamingACKEnabled)
				input.StreamingACKEnabled = !enabled
				require.Equal(t, enabled, *group.StreamingACKEnabled, "saved policy must not alias the request")
			})
		}
	}
}

func TestAdminUpdateGroupStreamingACKPreservesOmissionAndInvalidatesCache(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformComposite} {
		for _, existing := range []*bool{nil, groupDuplicateTestPointer(false), groupDuplicateTestPointer(true)} {
			for _, update := range []*bool{nil, groupDuplicateTestPointer(false), groupDuplicateTestPointer(true)} {
				t.Run(platform, func(t *testing.T) {
					repo := &groupPlatformRepoStub{group: &Group{ID: 7, Name: "group", Platform: platform, StreamingACKEnabled: existing}}
					invalidator := &authCacheInvalidatorStub{}
					svc := &adminServiceImpl{groupRepo: repo, authCacheInvalidator: invalidator}
					group, err := svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{Name: "renamed", StreamingACKEnabled: update})
					require.NoError(t, err)
					want := existing
					if update != nil {
						want = update
					}
					require.Equal(t, want, group.StreamingACKEnabled)
					require.Equal(t, []int64{7}, invalidator.groupIDs)
					if update != nil {
						original := *update
						*update = !original
						require.Equal(t, original, *group.StreamingACKEnabled, "saved policy must not alias the request")
					}
				})
			}
		}
	}
}
