package upstreamgovernance

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCatalogCollectorsPreserveExclusiveGroupType(t *testing.T) {
	for _, platform := range []string{"sub2api", "newapi"} {
		for _, fast := range []bool{false, true} {
			t.Run(platform+"/"+map[bool]string{false: "full", true: "fast"}[fast], func(t *testing.T) {
				connector := fixtureConnector(t, func(r *http.Request) (int, string) {
					switch r.URL.Path {
					case "/api/v1/user/profile":
						return 200, `{"code":0,"data":{"id":42}}`
					case "/api/user/self":
						return 200, `{"success":true,"data":{"id":42}}`
					case "/api/user/self/groups":
						return 200, `{"success":true,"data":{"7":{"desc":"Private","ratio":1,"is_exclusive":true},"8":{"desc":"Public","ratio":1,"is_exclusive":false}}}`
					case "/api/user/models":
						return 200, `{"success":true,"data":[]}`
					case "/api/v1/groups/available":
						return 200, `{"code":0,"data":[{"id":7,"name":"Private","platform":"openai","rate_multiplier":1,"is_exclusive":true},{"id":8,"name":"Public","platform":"openai","rate_multiplier":1,"is_exclusive":false}]}`
					case "/api/v1/groups/rates":
						return 200, `{"code":0,"data":{}}`
					case "/api/v1/channels/available", "/api/v1/model-plaza", "/api/pricing":
						return 404, `{}`
					default:
						t.Fatalf("unexpected path: %s", r.URL.Path)
						return 500, `{}`
					}
				})
				site := Site{Platform: platform, BaseURL: "https://upstream.example"}
				session := Session{AccessToken: "fixture", UserID: 42}
				var groups []RemoteGroup
				if fast {
					observation, err := connector.(FastObservationConnector).ObserveGroups(t.Context(), site, session)
					require.NoError(t, err)
					groups = observation.Groups
				} else {
					catalog, err := connector.Discover(t.Context(), site, session)
					require.NoError(t, err)
					groups = catalog.Groups
				}
				raw, err := json.Marshal(groups)
				require.NoError(t, err)
				var payload []map[string]any
				require.NoError(t, json.Unmarshal(raw, &payload))
				require.Equal(t, true, payload[0]["is_exclusive"])
				require.Equal(t, false, payload[1]["is_exclusive"])
			})
		}
	}
}

func TestCatalogNoticesSuppressExclusiveGroupsAcrossAllChanges(t *testing.T) {
	decode := func(raw string) RemoteGroup {
		var group RemoteGroup
		require.NoError(t, json.Unmarshal([]byte(raw), &group))
		return group
	}
	private := decode(`{"id":"7","name":"Private","description":"secret","platform":"openai","resolved_rate_multiplier":1,"is_exclusive":true}`)
	changedPrivate := decode(`{"id":"7","name":"Private changed","description":"more secret","platform":"openai","resolved_rate_multiplier":2,"is_exclusive":true,"models":["secret-model"],"prices":[{"model":"secret-model","unit":"token","input":5}]}`)
	public := decode(`{"id":"7","name":"Public","description":"public","platform":"openai","resolved_rate_multiplier":1,"is_exclusive":false}`)
	otherBefore := decode(`{"id":"8","name":"Other public","resolved_rate_multiplier":1,"is_exclusive":false}`)
	otherAfter := decode(`{"id":"8","name":"Other public changed","resolved_rate_multiplier":1,"is_exclusive":false}`)
	for _, tt := range []struct {
		name          string
		before, after []RemoteGroup
	}{
		{"created", nil, []RemoteGroup{private}},
		{"deleted", []RemoteGroup{private}, nil},
		{"name description rate models prices", []RemoteGroup{private}, []RemoteGroup{changedPrivate}},
		{"public to exclusive", []RemoteGroup{public}, []RemoteGroup{changedPrivate}},
		{"exclusive to public", []RemoteGroup{private}, []RemoteGroup{public}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := Catalog{GroupsComplete: true, Groups: append(tt.before, otherBefore)}
			after := Catalog{GroupsComplete: true, Groups: append(tt.after, otherAfter)}
			events := DiffCatalog(1, before, after)
			require.NotEmpty(t, events, "private changes remain available in operator audit history")
			notices := renderCatalogChangeNotices(Site{ID: 1}, events, after, time.Now(), true)
			require.Len(t, notices, 1)
			require.Equal(t, "8", notices[0].GroupResource)
			require.Contains(t, notices[0].Body, "Other public changed")
			require.NotContains(t, notices[0].Body, "Private")
		})
	}
}

func TestFastObservationFingerprintIncludesExclusiveGroupType(t *testing.T) {
	var public, private RemoteGroup
	require.NoError(t, json.Unmarshal([]byte(`{"id":"7","name":"Group","resolved_rate_multiplier":1,"is_exclusive":false}`), &public))
	require.NoError(t, json.Unmarshal([]byte(`{"id":"7","name":"Group","resolved_rate_multiplier":1,"is_exclusive":true}`), &private))
	before, err := groupObservationFingerprint(GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{public}})
	require.NoError(t, err)
	after, err := groupObservationFingerprint(GroupObservation{GroupsComplete: true, Groups: []RemoteGroup{private}})
	require.NoError(t, err)
	require.NotEqual(t, before, after, "a privacy-only change must refresh the stored catalog and audit history")
}

func TestDiffCatalogRetainsPublicDeletionVisibilityForQueuedOfflineNotice(t *testing.T) {
	events := DiffCatalog(1,
		Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "7", Name: "Public"}}},
		Catalog{GroupsComplete: true},
	)
	var checkpoint *Event
	for i := range events {
		if events[i].Kind == "group_visibility_changed" && events[i].Resource == "7" {
			checkpoint = &events[i]
		}
	}
	require.NotNil(t, checkpoint, "a delayed offline notice must remain verifiably public after old catalogs expire")
	require.Equal(t, "false", checkpoint.Before)
	require.Equal(t, "false", checkpoint.After)
	notices := renderCatalogChangeNotices(Site{ID: 1}, events, Catalog{GroupsComplete: true}, time.Now(), true)
	require.Len(t, notices, 1)
	require.Contains(t, notices[0].Body, "分组下线：Public")
}

func TestDiffCatalogDeletionDoesNotMakeAnExclusiveSourcePublic(t *testing.T) {
	events := DiffCatalog(1,
		Catalog{GroupsComplete: true, Groups: []RemoteGroup{{ID: "7", Name: "Private", IsExclusive: true}}},
		Catalog{GroupsComplete: true},
	)
	var checkpoint *Event
	for i := range events {
		if events[i].Kind == "group_visibility_changed" && events[i].Resource == "7" {
			checkpoint = &events[i]
		}
	}
	require.NotNil(t, checkpoint)
	require.Equal(t, "true", checkpoint.Before)
	require.Equal(t, "true", checkpoint.After, "later changes of a bound local group must still treat its deleted upstream source as private")
	require.Empty(t, renderCatalogChangeNotices(Site{ID: 1}, events, Catalog{GroupsComplete: true}, time.Now(), true))
}
