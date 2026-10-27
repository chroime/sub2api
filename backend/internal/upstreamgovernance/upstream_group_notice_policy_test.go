package upstreamgovernance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type upstreamGroupNoticeStore struct {
	Store
	*changeNotificationMemoryStore
	policy PricingNotificationPolicy
}

func (s *upstreamGroupNoticeStore) LoadChangeNotificationPolicy(context.Context, int64) (PricingNotificationPolicy, error) {
	return s.policy, nil
}

type upstreamGroupNoticeNotifier struct{}

func (upstreamGroupNoticeNotifier) Recipients(_ context.Context, override []string) ([]string, error) {
	if len(override) > 0 {
		return override, nil
	}
	return []string{"admin@example.test"}, nil
}

func (upstreamGroupNoticeNotifier) SendChange(context.Context, string, ChangeNotice) error {
	return nil
}

func newUpstreamGroupNoticeService(t *testing.T, events string, policy PricingNotificationPolicy) (*Service, *upstreamGroupNoticeStore, *memoryStore, *fakeConnector) {
	t.Helper()
	svc, base, connector, _ := setupEngine(t)
	store := &upstreamGroupNoticeStore{Store: base, changeNotificationMemoryStore: &changeNotificationMemoryStore{items: map[int64]ChangeNotification{}}, policy: policy}
	svc.store = store
	svc.SetChangeNotifier(upstreamGroupNoticeNotifier{})
	svc.SetGlobalSettingsStore(&globalSettingsMemoryStore{values: map[string]string{GlobalSettingsKey: `{"notifications":{"enabled":true,"recipients":["global@example.test"],"events":` + events + `}}`}})
	return svc, store, base, connector
}

func TestUpstreamGroupNoticesFilterEachGlobalAndSiteDetailBeforeCombining(t *testing.T) {
	for _, tt := range []struct {
		name   string
		events string
		policy PricingNotificationPolicy
		want   []string
		omit   []string
	}{
		{
			name: "only name enabled globally", events: `{"group_name_change":true,"group_description_change":false,"group_rate_change":false}`,
			want: []string{"分组名称调整：旧名称 更改为 新名称"}, omit: []string{"分组描述调整：", "分组倍率调整："},
		},
		{
			name: "only description enabled globally", events: `{"group_name_change":false,"group_description_change":true,"group_rate_change":false}`,
			want: []string{"分组描述调整：新名称，原描述「旧描述」更改为「新描述」"}, omit: []string{"分组名称调整：", "分组倍率调整："},
		},
		{
			name: "only rate enabled globally", events: `{"group_name_change":false,"group_description_change":false,"group_rate_change":true}`,
			want: []string{"分组倍率调整：新名称，0.8x → 0.9x（上调）"}, omit: []string{"分组名称调整：", "分组描述调整："},
		},
		{
			name: "site disables rate", events: `{"group_name_change":true,"group_description_change":true,"group_rate_change":true}`,
			policy: PricingNotificationPolicy{Enabled: true, GroupChanges: true, Recipients: []string{"site@example.test"}},
			want: []string{"分组名称调整：", "分组描述调整："}, omit: []string{"分组倍率调整："},
		},
		{
			name: "site disables group details", events: `{"group_name_change":true,"group_description_change":true,"group_rate_change":true}`,
			policy: PricingNotificationPolicy{Enabled: true, RateChanges: true},
			want: []string{"分组倍率调整："}, omit: []string{"分组名称调整：", "分组描述调整："},
		},
		{
			name: "no matching globally enabled details", events: `{"group_name_change":false,"group_description_change":false,"group_rate_change":false}`,
		},
		{
			name: "site disables both classes", events: `{"group_name_change":true,"group_description_change":true,"group_rate_change":true}`,
			policy: PricingNotificationPolicy{Enabled: true},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, store, base, connector := newUpstreamGroupNoticeService(t, tt.events, tt.policy)
			beforeRate, afterRate := 0.8, 0.9
			base.snap = &Snapshot{Catalog: Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "旧名称", Description: "旧描述", ResolvedRateMultiplier: &beforeRate}}}}
			connector.catalog = Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "42", Name: "新名称", Description: "新描述", ResolvedRateMultiplier: &afterRate}}}
			svc.now = func() time.Time { return time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC) }

			_, err := svc.syncLocked(t.Context(), base.site)
			require.NoError(t, err)
			if len(tt.want) == 0 {
				require.Empty(t, store.items)
				return
			}
			require.Len(t, store.items, 1)
			for _, item := range store.items {
				for _, text := range tt.want {
					require.Contains(t, item.Body, text)
				}
				for _, text := range tt.omit {
					require.NotContains(t, item.Body, text)
				}
				require.Equal(t, "group_changes", item.Kind)
				require.Equal(t, 1, strings.Count(item.Body, "通知时间："))
				require.True(t, strings.HasSuffix(item.Body, "通知时间：2026-10-23 09:02:03"))
				if len(tt.policy.Recipients) > 0 {
					require.Equal(t, "site@example.test", item.Recipient)
				} else {
					require.Equal(t, "global@example.test", item.Recipient)
				}
			}
		})
	}
}

func TestUpstreamGroupOnlineAndOfflineSwitchesAreIndependent(t *testing.T) {
	for _, tt := range []struct {
		name, events, want, omit string
	}{
		{name: "online only", events: `{"group_online":true,"group_offline":false}`, want: "分组上线：新组", omit: "分组下线："},
		{name: "offline only", events: `{"group_online":false,"group_offline":true}`, want: "分组下线：旧组", omit: "分组上线："},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, store, base, connector := newUpstreamGroupNoticeService(t, tt.events, PricingNotificationPolicy{})
			rate := 0.8
			base.snap = &Snapshot{Catalog: Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "1", Name: "旧组", ResolvedRateMultiplier: &rate}}}}
			connector.catalog = Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "2", Name: "新组", ResolvedRateMultiplier: &rate}}}

			_, err := svc.syncLocked(t.Context(), base.site)
			require.NoError(t, err)
			require.Len(t, store.items, 1)
			for _, item := range store.items {
				require.Contains(t, item.Body, tt.want)
				require.NotContains(t, item.Body, tt.omit)
			}
		})
	}
}

func TestUpstreamGroupFullCollectionSilencesInitialAndIncompleteBaselines(t *testing.T) {
	for _, tt := range []struct {
		name string
		before *Snapshot
		afterComplete bool
	}{
		{name: "initial", afterComplete: true},
		{name: "previous incomplete", before: &Snapshot{Catalog: Catalog{}}, afterComplete: true},
		{name: "current incomplete", before: &Snapshot{Catalog: Catalog{GroupsComplete: true}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, store, base, connector := newUpstreamGroupNoticeService(t, `{"group_online":true,"group_offline":true}`, PricingNotificationPolicy{})
			base.snap = tt.before
			rate := 0.8
			connector.catalog = Catalog{GroupsComplete: tt.afterComplete, Groups: []RemoteGroup{{ID: "42", Name: "新组", ResolvedRateMultiplier: &rate}}}

			_, err := svc.syncLocked(t.Context(), base.site)
			require.NoError(t, err)
			require.Empty(t, store.items)
		})
	}
}

func TestUpstreamGroupFullAndFastDedupIgnoreFullOnlyMetadata(t *testing.T) {
	for _, change := range []string{"online", "offline", "rate", "mixed"} {
		t.Run(change, func(t *testing.T) {
			svc, store, base, connector := newUpstreamGroupNoticeService(t, `{"group_online":true,"group_offline":true,"group_name_change":true,"group_description_change":true,"group_rate_change":true}`, PricingNotificationPolicy{})
			beforeRate, afterRate := 0.8, 0.9
			beforeGroup := RemoteGroup{ID: "42", Name: "旧组", Description: "旧描述", UserRateMultiplier: &beforeRate, ResolvedRateMultiplier: &beforeRate, Models: []string{"old-model"}}
			afterGroup := RemoteGroup{ID: "42", Name: "旧组", Description: "旧描述", UserRateMultiplier: &afterRate, ResolvedRateMultiplier: &afterRate, Models: []string{"new-model"}, Prices: []RemotePrice{{Model: "new-model", Unit: "token"}}}
			if change == "mixed" {
				afterGroup.Name, afterGroup.Description = "新组", "新描述"
			}
			before, after := Catalog{GroupsComplete: true, Groups: []RemoteGroup{beforeGroup}}, Catalog{GroupsComplete: true, Groups: []RemoteGroup{afterGroup}}
			if change == "online" {
				before.Groups = []RemoteGroup{}
			}
			if change == "offline" {
				after.Groups = []RemoteGroup{}
			}
			base.snap, connector.catalog = &Snapshot{Catalog: before}, after
			_, err := svc.syncLocked(t.Context(), base.site)
			require.NoError(t, err)
			require.Len(t, store.items, 1)

			beforeGroup.Models, beforeGroup.Prices, beforeGroup.RateMultiplier = nil, nil, &beforeRate
			afterGroup.Models, afterGroup.Prices, afterGroup.RateMultiplier = nil, nil, &afterRate
			before.Groups, after.Groups = []RemoteGroup{beforeGroup}, []RemoteGroup{afterGroup}
			if change == "online" {
				before.Groups = []RemoteGroup{}
			}
			if change == "offline" {
				after.Groups = []RemoteGroup{}
			}
			for _, notice := range fastObservationChangeNotices(base.site, &CatalogObservation{Revision: 1, GroupsComplete: true, Groups: before.Groups}, GroupObservation{GroupsComplete: true, Groups: after.Groups}, 2, time.Now().UTC()) {
				require.NoError(t, svc.EnqueueChangeNotice(t.Context(), notice))
			}
			require.Len(t, store.items, 1)
		})
	}
}

func TestUpstreamSitePricingAndProtectionRemainIndependentOfGlobalGroupSwitches(t *testing.T) {
	for _, kind := range []string{"pricing_change", "protection_change"} {
		t.Run(kind, func(t *testing.T) {
			svc, store, _, _ := newUpstreamGroupNoticeService(t, `{"group_online":false,"group_offline":false,"group_name_change":false,"group_description_change":false,"group_rate_change":false,"pricing_change":false,"protection_change":false}`, PricingNotificationPolicy{Enabled: true, PricingChanges: true, ProtectionChanges: true, Recipients: []string{"site@example.test"}})
			require.NoError(t, svc.EnqueueChangeNotice(t.Context(), ChangeNotice{SiteID: 1, Kind: kind, DedupKey: kind, Subject: "独立站点通知", Body: "自动调价结果"}))
			require.Len(t, store.items, 1)
			for _, item := range store.items {
				require.Equal(t, "site@example.test", item.Recipient)
			}
		})
	}
}
