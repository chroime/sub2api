package upstreamgovernance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorPostPlannedKeySendsOnlyCreateRequest(t *testing.T) {
	for _, tc := range []struct {
		name, platform, groupID, path, body, response, idempotency string
	}{
		{
			name: "sub2api", platform: "sub2api", groupID: "7", path: "/api/v1/keys",
			body: `{"group_id":7,"name":"repair-20260930"}`, response: `{"code":0,"data":{}}`,
			idempotency: "repair-operation-42",
		},
		{
			name: "newapi", platform: "newapi", groupID: "vip", path: "/api/token/",
			body:     `{"name":"repair-20260930","group":"vip","expired_time":-1,"unlimited_quota":true,"model_limits_enabled":false,"cross_group_retry":false,"auto_groups":[]}`,
			response: `{"success":true,"data":{}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				requests++
				require.Equal(t, http.MethodPost, r.Method, "repair create must not profile, list, or reveal")
				require.Equal(t, tc.path, r.URL.Path)
				require.Equal(t, tc.idempotency, r.Header.Get("Idempotency-Key"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, tc.body, string(body))
				return http.StatusOK, tc.response
			})
			err := connector.(KeyRepairCreateOnce).PostPlannedKey(t.Context(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: tc.groupID}, "repair-operation-42", KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
			require.NoError(t, err)
			require.Equal(t, 1, requests)
		})
	}
}

func TestConnectorPostPlannedKeyNeverRetriesRejectedOrUncertainRequests(t *testing.T) {
	for _, tc := range []struct {
		name, platform, groupID string
		status                  int
		body                    string
	}{
		{"sub2api 400", "sub2api", "7", 400, `{"code":400,"reason":"BAD_REQUEST"}`},
		{"sub2api 409", "sub2api", "7", 409, `{"code":409,"reason":"IN_PROGRESS"}`},
		{"sub2api 502", "sub2api", "7", 502, `{"message":"upstream failure"}`},
		{"newapi 400", "newapi", "vip", 400, `{"success":false,"message":"rejected"}`},
		{"newapi 500", "newapi", "vip", 500, `{"success":false,"message":"upstream failure"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				requests++
				require.Equal(t, http.MethodPost, r.Method)
				return tc.status, tc.body
			})
			err := connector.(KeyRepairCreateOnce).PostPlannedKey(t.Context(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: tc.groupID}, "repair-operation-42", KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
			require.ErrorIs(t, err, errConnectorUncertain)
			require.Equal(t, 1, requests)
		})
	}
}

func TestConnectorPostPlannedKeyTreatsTransportAndFactoryFailuresAsUncertain(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factory ClientFactory
		wantDo  int
	}{
		{
			name: "transport timeout", wantDo: 1,
			factory: func(context.Context, Site) (HTTPDoer, error) {
				return connectorDoer(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded }), nil
			},
		},
		{
			name: "factory failure", wantDo: 0,
			factory: func(context.Context, Site) (HTTPDoer, error) { return nil, errors.New("factory failed") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			does := 0
			connector := NewConnector(func(ctx context.Context, site Site) (HTTPDoer, error) {
				doer, err := tc.factory(ctx, site)
				if err != nil {
					return nil, err
				}
				return connectorDoer(func(r *http.Request) (*http.Response, error) {
					does++
					return doer.Do(r)
				}), nil
			})
			err := connector.(KeyRepairCreateOnce).PostPlannedKey(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "vip"}, "repair-operation-42", KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
			require.ErrorIs(t, err, errConnectorUncertain)
			require.Equal(t, tc.wantDo, does)
		})
	}
}

func TestConnectorListKeyInventoryRetainsUngroupedRowsAndGroupedTarget(t *testing.T) {
	for _, tc := range []struct {
		name, platform, profile, list, body string
		want                                []RemoteKeyIdentity
	}{
		{
			name: "sub2api", platform: "sub2api", profile: "/api/v1/user/profile", list: "/api/v1/keys",
			body: `{"code":0,"data":{"total":3,"items":[{"id":3,"name":"ungrouped-null","group_id":null},{"id":4,"name":"ungrouped-zero","group_id":0},{"id":9,"name":"managed","group_id":7}]}}`,
			want: []RemoteKeyIdentity{{ID: "3", GroupID: ""}, {ID: "4", GroupID: ""}, {ID: "9", GroupID: "7"}},
		},
		{
			name: "newapi", platform: "newapi", profile: "/api/user/self", list: "/api/token/",
			body: `{"success":true,"data":{"total":2,"items":[{"id":3,"name":"ungrouped","group":""},{"id":9,"name":"managed","group":"vip"}]}}`,
			want: []RemoteKeyIdentity{{ID: "3", GroupID: ""}, {ID: "9", GroupID: "vip"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				require.Equal(t, http.MethodGet, r.Method)
				switch r.URL.Path {
				case tc.profile:
					return http.StatusOK, `{"code":0,"success":true,"data":{"id":42}}`
				case tc.list:
					return http.StatusOK, tc.body
				default:
					t.Fatalf("unexpected request: %s", r.URL.Path)
					return http.StatusInternalServerError, ""
				}
			})
			got, err := connector.(KeyInventoryReader).ListKeyInventory(t.Context(), Site{Platform: tc.platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestConnectorListKeyInventoryRejectsNegativeSub2APIGroupID(t *testing.T) {
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/api/v1/user/profile" {
			return http.StatusOK, `{"code":0,"data":{"id":42}}`
		}
		require.Equal(t, "/api/v1/keys", r.URL.Path)
		return http.StatusOK, `{"code":0,"data":{"total":1,"items":[{"id":9,"name":"malformed","group_id":-1}]}}`
	})
	_, err := connector.(KeyInventoryReader).ListKeyInventory(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42})
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestConnectorRecoverPlannedKeyFindsGroupedCandidateAlongsideUngroupedKey(t *testing.T) {
	requests := 0
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		requests++
		require.Equal(t, http.MethodGet, r.Method)
		switch r.URL.Path {
		case "/api/v1/user/profile":
			return http.StatusOK, `{"code":0,"data":{"id":42}}`
		case "/api/v1/keys":
			return http.StatusOK, `{"code":0,"data":{"total":2,"items":[{"id":3,"name":"unrelated","group_id":null},{"id":9,"name":"repair-20260930","group_id":7,"key":"sk-recovered-fixture"}]}}`
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
			return http.StatusInternalServerError, ""
		}
	})
	key, found, err := connector.(KeyRepairRecovery).RecoverPlannedKey(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "7"}, KeyCreationPlan{Name: "repair-20260930", ExistingIDs: []int64{}})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, RemoteKey{ID: "9", Key: "sk-recovered-fixture"}, key)
	require.Equal(t, 2, requests)
}
