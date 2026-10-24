package upstreamgovernance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFastObservationChangeNoticesUseDetailedGroupDiffAndFullCollectionDeduplication(t *testing.T) {
	beforeRate, afterRate := 0.8, 0.9
	previous := &CatalogObservation{
		SiteID: 7, Revision: 1, GroupsComplete: true,
		Groups: []RemoteGroup{{ID: "claude", Name: "Claude Max", Platform: "openai", ResolvedRateMultiplier: &beforeRate}},
	}
	observation := GroupObservation{
		GroupsComplete: true,
		Groups:         []RemoteGroup{{ID: "claude", Name: "Claude Max", Platform: "openai", ResolvedRateMultiplier: &afterRate}},
	}
	observedAt := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)

	notices := fastObservationChangeNotices(Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"}, previous, observation, 2, observedAt)
	require.Len(t, notices, 1)
	require.Contains(t, notices[0].Subject, "Claude Max")
	require.Contains(t, notices[0].Body, "上游名称：示例上游")
	require.Contains(t, notices[0].Body, "站点URL：https://up.example")
	require.Contains(t, notices[0].Body, "分组倍率调整：Claude Max，0.8x → 0.9x（上调）")
	require.NotContains(t, notices[0].Body, "{")

	events := DiffCatalog(7, Catalog{GroupsComplete: true, Groups: previous.Groups}, Catalog{GroupsComplete: true, Groups: observation.Groups})
	require.Len(t, events, 1)
	fullNotice := renderCatalogChangeNotice(Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"}, events[0], Catalog{Groups: observation.Groups})
	require.Equal(t, fullNotice.DedupKey, notices[0].DedupKey)
}

func TestFastObservationCombinesSameGroupChangesWithOneFinalTimestamp(t *testing.T) {
	beforeRate, afterRate := 0.8, 0.9
	previous := &CatalogObservation{Revision: 1, GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "旧名称", Description: "旧描述", ResolvedRateMultiplier: &beforeRate}}}
	observation := GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "新名称", Description: "新描述", ResolvedRateMultiplier: &afterRate}}}
	observedAt := time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC)

	notices := fastObservationChangeNotices(Site{ID: 7}, previous, observation, 2, observedAt)

	require.Len(t, notices, 1)
	require.Contains(t, notices[0].Body, "分组名称调整：旧名称 更改为 新名称\n分组描述调整：新名称，原描述「旧描述」更改为「新描述」\n分组倍率调整：新名称，0.8x → 0.9x（上调）\n通知时间：2026-10-23 09:02:03")
}

func TestFastObservationKeepsDifferentGroupsInSeparateNotices(t *testing.T) {
	rate := 0.8
	previous := &CatalogObservation{Revision: 1, GroupsComplete: true, Groups: []RemoteGroup{{ID: "1", Name: "旧一", ResolvedRateMultiplier: &rate}, {ID: "2", Name: "旧二", ResolvedRateMultiplier: &rate}}}
	observation := GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{{ID: "1", Name: "新一", ResolvedRateMultiplier: &rate}, {ID: "2", Name: "新二", ResolvedRateMultiplier: &rate}}}

	notices := fastObservationChangeNotices(Site{ID: 7}, previous, observation, 2, time.Now().UTC())

	require.Len(t, notices, 2)
	require.Contains(t, notices[0].Body, "旧一 更改为 新一")
	require.NotContains(t, notices[0].Body, "旧二")
	require.Contains(t, notices[1].Body, "旧二 更改为 新二")
}

func TestFastObservationSilencesIncompleteCatalogTransitions(t *testing.T) {
	rate := 0.8
	for _, tt := range []struct {
		name string
		beforeComplete, afterComplete bool
	}{
		{name: "previous incomplete", afterComplete: true},
		{name: "current incomplete", beforeComplete: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			notices := fastObservationChangeNotices(Site{ID: 7}, &CatalogObservation{Revision: 1, GroupsComplete: tt.beforeComplete}, GroupObservation{GroupsComplete: tt.afterComplete, Groups: []RemoteGroup{{ID: "42", Name: "新组", ResolvedRateMultiplier: &rate}}}, 2, time.Now().UTC())
			require.Empty(t, notices)
		})
	}
}

func TestFastObservationChangeNoticesSuppressInitialBaseline(t *testing.T) {
	rate := 0.8
	notices := fastObservationChangeNotices(Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"}, nil, GroupObservation{
		GroupsComplete: true,
		Groups:         []RemoteGroup{{ID: "claude", Name: "Claude Max", ResolvedRateMultiplier: &rate}},
	}, 1, time.Now().UTC())
	require.Empty(t, notices)
}
