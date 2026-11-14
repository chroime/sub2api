//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func readGroupIdentity(t *testing.T, value any) map[string]string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var data struct{ Identity map[string]string }
	require.NoError(t, json.Unmarshal(raw, &data))
	return data.Identity
}

func groupIdentityFixture(t *testing.T) *Group {
	t.Helper()
	group := &Group{ID: 7, Name: "group", Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 1}
	require.NoError(t, json.Unmarshal([]byte(`{"identity":{"label":"自营","color":"teal"}}`), group))
	return group
}

func TestAdminGroupIdentityCreateNormalizeAndSimpleMode(t *testing.T) {
	for _, simple := range []bool{false, true} {
		repo := &groupRepoStubForAdmin{}
		svc := &adminServiceImpl{groupRepo: repo}
		if simple {
			svc.cfg = &config.Config{RunMode: config.RunModeSimple}
		}
		var input CreateGroupInput
		require.NoError(t, json.Unmarshal([]byte(`{"name":"group","platform":"anthropic","RateMultiplier":1,"identity":{"label":" 自营 ","color":"teal"}}`), &input))
		group, err := svc.CreateGroup(t.Context(), &input)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"label": "自营", "color": "teal"}, readGroupIdentity(t, group))
	}
}

func TestAdminGroupIdentityUpdateOmitClearReplace(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		want          map[string]string
	}{
		{"omitted", `{"name":"new"}`, map[string]string{"label": "自营", "color": "teal"}},
		{"clear", `{"identity":{}}`, map[string]string{}},
		{"replace", `{"identity":{"label":" 严选 ","color":"blue"}}`, map[string]string{"label": "严选", "color": "blue"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByID: groupIdentityFixture(t)}
			var input UpdateGroupInput
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &input))
			group, err := (&adminServiceImpl{groupRepo: repo}).UpdateGroup(t.Context(), 7, &input)
			require.NoError(t, err)
			require.Equal(t, tc.want, readGroupIdentity(t, group))
		})
	}
}

func TestAdminGroupIdentityInvalidBeforeAnyMutation(t *testing.T) {
	for _, identity := range []string{`{"label":"自营","color":"#fff"}`, `{"label":"` + strings.Repeat("甲", 13) + `","color":"teal"}`} {
		repo := &groupRepoStubForAdmin{getByID: groupIdentityFixture(t)}
		var create CreateGroupInput
		require.NoError(t, json.Unmarshal([]byte(`{"name":"new","RateMultiplier":1,"identity":`+identity+`}`), &create))
		_, err := (&adminServiceImpl{groupRepo: repo}).CreateGroup(t.Context(), &create)
		require.Equal(t, "INVALID_GROUP_IDENTITY", infraerrors.Reason(err))
		require.Nil(t, repo.created)
		var update UpdateGroupInput
		require.NoError(t, json.Unmarshal([]byte(`{"name":"should-not-change","identity":`+identity+`}`), &update))
		_, err = (&adminServiceImpl{groupRepo: repo}).UpdateGroup(t.Context(), 7, &update)
		require.Equal(t, "INVALID_GROUP_IDENTITY", infraerrors.Reason(err))
		require.Nil(t, repo.updated)
		require.Equal(t, "group", repo.getByID.Name)
	}
}

func TestGroupIdentityDuplicateAndPublicProjections(t *testing.T) {
	group := groupIdentityFixture(t)
	want := map[string]string{"label": "自营", "color": "teal"}
	require.Equal(t, want, readGroupIdentity(t, cloneGroupForDuplicate(group, "operation")))
	plaza, err := newPlazaService(nil, []Group{*group}, nil).ListGroupsWithAccountModels(t.Context(), staticPlazaAccountModels(nil))
	require.NoError(t, err)
	require.Len(t, plaza, 1)
	require.Equal(t, want, readGroupIdentity(t, plaza[0]))
	channels, err := newAvailableChannelService([]Channel{{ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{7}}}, &stubGroupRepoForAvailable{activeGroups: []Group{*group}}).ListAvailable(t.Context())
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Len(t, channels[0].Groups, 1)
	require.Equal(t, want, readGroupIdentity(t, channels[0].Groups[0]))
}

type groupIdentityServiceRepoStub struct{ groupRepoStubForAdmin }

func (*groupIdentityServiceRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}

func TestGroupServiceIdentityCreateUpdateAndValidate(t *testing.T) {
	repo := &groupIdentityServiceRepoStub{}
	svc := NewGroupService(repo, nil)
	var create CreateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"basic","rate_multiplier":1,"identity":{"label":" 自营 ","color":"teal"}}`), &create))
	group, err := svc.Create(t.Context(), create)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"label": "自营", "color": "teal"}, readGroupIdentity(t, group))
	repo.getByID = group
	var omitted UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"description":"updated"}`), &omitted))
	updated, err := svc.Update(t.Context(), 7, omitted)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"label": "自营", "color": "teal"}, readGroupIdentity(t, updated))
	var clear UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"identity":{}}`), &clear))
	updated, err = svc.Update(t.Context(), 7, clear)
	require.NoError(t, err)
	require.Empty(t, readGroupIdentity(t, updated))
	repo.created, repo.updated = nil, nil
	require.NoError(t, json.Unmarshal([]byte(`{"name":"should-not-change","identity":{"label":"tag","color":"invalid"}}`), &clear))
	_, err = svc.Update(t.Context(), 7, clear)
	require.Equal(t, "INVALID_GROUP_IDENTITY", infraerrors.Reason(err))
	require.Nil(t, repo.updated)
	require.Equal(t, "basic", group.Name)
	require.NoError(t, json.Unmarshal([]byte(`{"identity":{"label":"tag","color":"invalid"}}`), &create))
	_, err = svc.Create(t.Context(), create)
	require.Equal(t, "INVALID_GROUP_IDENTITY", infraerrors.Reason(err))
	require.Nil(t, repo.created)
}
