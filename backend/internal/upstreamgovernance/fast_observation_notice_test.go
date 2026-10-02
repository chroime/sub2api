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
	require.Contains(t, notices[0].Body, "分组名称：Claude Max")
	require.Contains(t, notices[0].Body, "倍率：0.8 -> 0.9")
	require.Contains(t, notices[0].Body, "上调幅度为12.5%")
	require.NotContains(t, notices[0].Body, "{")

	events := DiffCatalog(7, Catalog{GroupsComplete: true, Groups: previous.Groups}, Catalog{GroupsComplete: true, Groups: observation.Groups})
	require.Len(t, events, 1)
	require.Equal(t, catalogChangeNoticeDedupKey(7, events[0]), notices[0].DedupKey)
}

func TestFastObservationChangeNoticesSuppressInitialBaseline(t *testing.T) {
	rate := 0.8
	notices := fastObservationChangeNotices(Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"}, nil, GroupObservation{
		GroupsComplete: true,
		Groups:         []RemoteGroup{{ID: "claude", Name: "Claude Max", ResolvedRateMultiplier: &rate}},
	}, 1, time.Now().UTC())
	require.Empty(t, notices)
}
