package upstreamgovernance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorNamedKeysPreserveIdentityAndExcludeExistingNames(t *testing.T) {
	for _, platform := range []string{"sub2api", "newapi"} {
		t.Run(platform, func(t *testing.T) {
			name := "codex特惠-0.06-20260927"
			group := RemoteGroup{ID: "7", Name: "codex特惠"}
			keys := []map[string]any{}
			add := func(id int64, groupID string) {
				key := map[string]any{"id": id, "name": name, "key": fmt.Sprintf("sk-fixture-%d", id)}
				if platform == "sub2api" {
					key["group_id"] = 7
					if groupID != "7" {
						key["group_id"] = 99
					}
				} else {
					key["group"] = groupID
				}
				keys = append(keys, key)
			}
			add(1, "7")  // A manually created or another protocol's key.
			add(2, "99") // A different group may have the same display name.
			posts := 0
			markers := []string{}
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				if r.URL.Path == "/api/v1/user/profile" {
					return 200, `{"code":0,"data":{"id":42}}`
				}
				if r.URL.Path == "/api/user/self" {
					return 200, `{"success":true,"data":{"id":42}}`
				}
				if r.URL.Path == "/api/v1/keys" || r.URL.Path == "/api/token/" {
					if r.Method == http.MethodGet {
						raw, err := json.Marshal(map[string]any{"code": 0, "success": true, "data": map[string]any{"total": len(keys), "items": keys}})
						require.NoError(t, err)
						return 200, string(raw)
					}
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, name, body["name"])
					if platform == "sub2api" {
						markers = append(markers, r.Header.Get("Idempotency-Key"))
						require.Equal(t, float64(7), body["group_id"])
					} else {
						require.Equal(t, "7", body["group"])
					}
					posts++
					add(int64(len(keys)+1), "7")
					return 200, `{"code":0,"success":true,"data":{}}`
				}
				for _, id := range []int{3, 4} {
					if r.URL.Path == fmt.Sprintf("/api/token/%d/key", id) {
						return 200, fmt.Sprintf(`{"success":true,"data":{"key":"sk-fixture-%d"}}`, id)
					}
				}
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				return 500, ""
			})
			site, session := Site{Platform: platform, BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}
			ids, err := connector.PrepareKey(t.Context(), site, session, group, name)
			require.NoError(t, err)
			require.Equal(t, []int64{1}, ids)
			firstPlan := &KeyCreationPlan{Name: name, ExistingIDs: ids}
			first, err := connector.EnsureKey(t.Context(), site, session, group, "stable-openai", firstPlan)
			require.NoError(t, err)
			require.Equal(t, "3", first.ID)
			firstAgain, err := connector.EnsureKey(t.Context(), site, session, group, "stable-openai", firstPlan)
			require.NoError(t, err)
			require.Equal(t, first, firstAgain)
			require.Equal(t, 1, posts)
			ids, err = connector.PrepareKey(t.Context(), site, session, group, name)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 3}, ids)
			second, err := connector.EnsureKey(t.Context(), site, session, group, "stable-anthropic", &KeyCreationPlan{Name: name, ExistingIDs: ids})
			require.NoError(t, err)
			require.Equal(t, "4", second.ID)
			require.Equal(t, 2, posts)
			if platform == "sub2api" {
				require.Equal(t, []string{"stable-openai", "stable-anthropic"}, markers)
			}
		})
	}
}

func TestConnectorNamedKeyRecoveryRejectsAmbiguousNewKeys(t *testing.T) {
	id := int64(7)
	keys := []connectorKey{{ID: 1, Name: "group-20260927", GroupID: &id}, {ID: 2, Name: "group-20260927", GroupID: &id}}
	_, err := connectorFindPlannedKey(keys, RemoteGroup{ID: "7"}, "stable", "sub2api", &KeyCreationPlan{Name: "group-20260927", ExistingIDs: []int64{}})
	require.ErrorIs(t, err, ErrConflict)
}

func TestConnectorNamedKeyRequiresPersistedInventoryBeforeRemoteEffects(t *testing.T) {
	connector := fixtureConnector(t, func(*http.Request) (int, string) {
		t.Fatal("unprepared naming plan must not issue any request")
		return 500, ""
	})
	_, err := connector.EnsureKey(t.Context(), Site{Platform: "sub2api"}, Session{}, RemoteGroup{ID: "7"}, "stable", &KeyCreationPlan{Name: "group-20260927"})
	require.ErrorIs(t, err, ErrInvalid)
}
