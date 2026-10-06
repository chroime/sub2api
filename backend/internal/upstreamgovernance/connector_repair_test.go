package upstreamgovernance

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorRecoverPlannedKeyFindsOnlyNewNamedKey(t *testing.T) {
	for _, tc := range []struct {
		platform string
		profile  string
		list     string
		group    string
		body     string
		reveal   string
	}{
		{
			platform: "sub2api", profile: "/api/v1/user/profile", list: "/api/v1/keys", group: "7",
			body:   `{"code":0,"data":{"total":3,"items":[{"id":4,"name":"repair-20260930","group_id":7,"key":"sk-old"},{"id":8,"name":"repair-20260930","group_id":99,"key":"sk-other"},{"id":9,"name":"repair-20260930","group_id":7,"key":"sk-***"}]}}`,
			reveal: "/api/v1/keys/9",
		},
		{
			platform: "newapi", profile: "/api/user/self", list: "/api/token/", group: "vip",
			body:   `{"success":true,"data":{"total":3,"items":[{"id":4,"name":"repair-20260930","group":"vip","key":"sk-old"},{"id":8,"name":"repair-20260930","group":"basic","key":"sk-other"},{"id":9,"name":"repair-20260930","group":"vip","key":"sk-***"}]}}`,
			reveal: "/api/token/9/key",
		},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			requests := []string{}
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case tc.profile:
					require.Equal(t, http.MethodGet, r.Method)
					return 200, `{"code":0,"success":true,"data":{"id":42}}`
				case tc.list:
					require.Equal(t, http.MethodGet, r.Method, "recovery must not create a key")
					return 200, tc.body
				case tc.reveal:
					if tc.platform == "newapi" {
						require.Equal(t, http.MethodPost, r.Method, "New API reveals through POST")
					} else {
						require.Equal(t, http.MethodGet, r.Method)
					}
					return 200, `{"code":0,"success":true,"data":{"id":9,"key":"sk-recovered-fixture"}}`
				default:
					t.Fatalf("unexpected recovery request: %s %s", r.Method, r.URL.Path)
					return 500, ""
				}
			})
			key, found, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: tc.group}, KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{4}})
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, RemoteKey{ID: "9", Key: "sk-recovered-fixture"}, key)
			require.Equal(t, []string{"GET " + tc.profile, "GET " + tc.list, map[bool]string{true: "POST ", false: "GET "}[tc.platform == "newapi"] + tc.reveal}, requests)
		})
	}
}

func TestConnectorRecoverPlannedKeyAbsentNeverCreates(t *testing.T) {
	for _, tc := range []struct {
		platform string
		profile  string
		list     string
		body     string
		group    string
	}{
		{"sub2api", "/api/v1/user/profile", "/api/v1/keys", `{"code":0,"data":{"total":1,"items":[{"id":4,"name":"repair-20260930","group_id":7,"key":"sk-old"}]}}`, "7"},
		{"newapi", "/api/user/self", "/api/token/", `{"success":true,"data":{"total":1,"items":[{"id":4,"name":"repair-20260930","group":"vip","key":"sk-old"}]}}`, "vip"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			requests := 0
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				requests++
				require.Equal(t, http.MethodGet, r.Method, "absent candidate must not cause POST")
				switch r.URL.Path {
				case tc.profile:
					return 200, `{"code":0,"success":true,"data":{"id":42}}`
				case tc.list:
					return 200, tc.body
				default:
					t.Fatalf("unexpected request: %s", r.URL.Path)
					return 500, ""
				}
			})
			key, found, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: tc.group}, KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{4}})
			require.NoError(t, err)
			require.False(t, found)
			require.Empty(t, key)
			require.Equal(t, 2, requests)
		})
	}
}

func TestConnectorRecoverPlannedKeyRejectsUnfrozenInventoryBeforeRequests(t *testing.T) {
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		t.Fatalf("unfrozen repair plan made request: %s %s", r.Method, r.URL.Path)
		return 500, ""
	})
	_, _, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "7"}, KeyCreationPlan{Name: "repair-20260930"})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestConnectorRecoverPlannedKeyRejectsChangedManagementIdentity(t *testing.T) {
	requests := 0
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		requests++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/user/profile", r.URL.Path)
		return 200, `{"code":0,"data":{"id":43}}`
	})
	_, _, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "7"}, KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
	require.True(t, errors.Is(err, ErrReauth))
	require.Equal(t, 1, requests)
}

func TestConnectorRecoverPlannedKeyRequiresCompleteUnambiguousList(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"incomplete", `{"code":0,"data":{"total":1,"items":[]}}`, ErrUnsupported},
		{"ambiguous", `{"code":0,"data":{"total":2,"items":[{"id":9,"name":"repair-20260930","group_id":7,"key":"sk-a"},{"id":10,"name":"repair-20260930","group_id":7,"key":"sk-b"}]}}`, ErrConflict},
		{"malformed identity", `{"code":0,"data":{"total":1,"items":[{"id":0,"name":"repair-20260930","group_id":7,"key":"sk-a"}]}}`, ErrUnsupported},
		{"negative group", `{"code":0,"data":{"total":1,"items":[{"id":9,"name":"repair-20260930","group_id":-1,"key":"sk-a"}]}}`, ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				require.Equal(t, http.MethodGet, r.Method)
				switch r.URL.Path {
				case "/api/v1/user/profile":
					return 200, `{"code":0,"data":{"id":42}}`
				case "/api/v1/keys":
					return 200, tc.body
				default:
					t.Fatalf("invalid list caused request: %s", r.URL.Path)
					return 500, ""
				}
			})
			_, found, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "7"}, KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
			require.ErrorIs(t, err, tc.want)
			require.False(t, found)
		})
	}
}

func TestConnectorRecoverPlannedKeyRejectsMaskedPlaintext(t *testing.T) {
	for _, platform := range []string{"sub2api", "newapi"} {
		t.Run(platform, func(t *testing.T) {
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/api/v1/user/profile", "/api/user/self":
					return 200, `{"code":0,"success":true,"data":{"id":42}}`
				case "/api/v1/keys":
					return 200, `{"code":0,"data":{"total":1,"items":[{"id":9,"name":"repair-20260930","group_id":7,"key":"sk-***"}]}}`
				case "/api/token/":
					return 200, `{"success":true,"data":{"total":1,"items":[{"id":9,"name":"repair-20260930","group":"vip","key":"sk-***"}]}}`
				case "/api/v1/keys/9":
					require.Equal(t, http.MethodGet, r.Method)
					return 200, `{"code":0,"data":{"id":9,"key":"sk-***"}}`
				case "/api/token/9/key":
					require.Equal(t, http.MethodPost, r.Method)
					return 200, `{"success":true,"data":{"key":"sk-***"}}`
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
					return 500, ""
				}
			})
			group := "7"
			if platform == "newapi" {
				group = "vip"
			}
			_, found, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: group}, KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
			require.ErrorIs(t, err, ErrUnsupported, fmt.Sprintf("%s must reject a masked reveal", platform))
			require.False(t, found)
		})
	}
}
