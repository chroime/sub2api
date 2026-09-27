package upstreamgovernance

import (
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorSub2APIKeysAcceptNativeNestedGroupOnListAndRead(t *testing.T) {
	posts, lists, reads := 0, 0, 0
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case r.Method == "GET" && r.URL.Path == "/api/v1/keys":
			lists++
			unrelated := `{"id":1,"user_id":42,"name":"unrelated","group_id":99,"group":{"id":99,"name":"Other","platform":"anthropic"},"key":"sk-unrelated"}`
			if posts == 0 {
				return 200, fmt.Sprintf(`{"code":0,"data":{"total":1,"page":1,"page_size":100,"items":[%s]}}`, unrelated)
			}
			return 200, fmt.Sprintf(`{"code":0,"data":{"total":2,"page":1,"page_size":100,"items":[%s,{"id":9,"user_id":42,"name":"governance-stable","group_id":7,"group":{"id":7,"name":"Selected","platform":"openai"},"key":"sk-***"}]}}`, unrelated)
		case r.Method == "POST" && r.URL.Path == "/api/v1/keys":
			posts++
			require.Equal(t, "governance-stable", r.Header.Get("Idempotency-Key"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"group_id":7,"name":"governance-stable"}`, string(body))
			return 200, `{"code":0,"data":{"id":9,"name":"governance-stable","group_id":7,"group":{"id":7},"key":"sk-fixture-key"}}`
		case r.Method == "GET" && r.URL.Path == "/api/v1/keys/9":
			reads++
			return 200, `{"code":0,"data":{"id":9,"name":"governance-stable","group_id":7,"group":{"id":7,"name":"Selected","platform":"openai"},"key":"sk-fixture-key"}}`
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return 500, ""
		}
	})
	site := Site{Platform: "sub2api", BaseURL: "https://upstream.example"}
	session := Session{AccessToken: "fixture", UserID: 42}
	key, err := connector.EnsureKey(t.Context(), site, session, RemoteGroup{ID: "7"}, "governance-stable", nil)
	require.NoError(t, err)
	require.Equal(t, RemoteKey{ID: "9", Key: "sk-fixture-key"}, key)
	require.Equal(t, 1, posts)
	require.Equal(t, 2, lists)
	require.Equal(t, 1, reads)
	key, err = connector.EnsureKey(t.Context(), site, session, RemoteGroup{ID: "7"}, "governance-stable", nil)
	require.NoError(t, err)
	require.Equal(t, "sk-fixture-key", key.Key)
	require.Equal(t, 1, posts, "reconciliation must reuse the nested native group key")
}

func TestConnectorNewAPIKeyGroupStillRequiresStringBeforeCreate(t *testing.T) {
	for _, group := range []string{`{"id":7}`, `["vip"]`, `7`} {
		t.Run(group, func(t *testing.T) {
			posts := 0
			connector := fixtureConnector(t, func(r *http.Request) (int, string) {
				if r.URL.Path == "/api/user/self" {
					return 200, `{"success":true,"data":{"id":42}}`
				}
				if r.Method == "POST" {
					posts++
				}
				return 200, fmt.Sprintf(`{"success":true,"data":{"total":1,"items":[{"id":1,"name":"unrelated","group":%s,"key":"sk-unrelated"}]}}`, group)
			})
			_, err := connector.EnsureKey(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "vip"}, "governance-stable", nil)
			require.ErrorIs(t, err, ErrUnsupported)
			require.Zero(t, posts, "invalid key-list data must prevent creation")
		})
	}
}

func TestConnectorSub2APIKeyCreationSupportsRequiredIdempotency(t *testing.T) {
	created := false
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case r.Method == "POST" && r.URL.Path == "/api/v1/keys":
			if r.Header.Get("Idempotency-Key") == "" {
				return 400, `{"code":400,"reason":"IDEMPOTENCY_KEY_REQUIRED"}`
			}
			created = true
			return 200, `{"code":0,"data":{}}`
		case r.Method == "GET" && r.URL.Path == "/api/v1/keys":
			if created {
				return 200, `{"code":0,"data":{"total":1,"items":[{"id":9,"name":"governance-stable","group_id":7,"key":"sk-fixture-key"}]}}`
			}
			return 200, `{"code":0,"data":{"total":0,"items":[]}}`
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return 500, ""
		}
	})
	key, err := connector.EnsureKey(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, Session{AccessToken: "fixture", UserID: 42}, RemoteGroup{ID: "7"}, "governance-stable", nil)
	require.NoError(t, err)
	require.Equal(t, RemoteKey{ID: "9", Key: "sk-fixture-key"}, key)
}

func TestConnectorSub2APIRetryKeepsIdempotencyWhileFirstKeyIsInvisible(t *testing.T) {
	postedKeys := []string{}
	postedBodies := []string{}
	operations := map[string]bool{}
	starts := 0
	published := false
	connector := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/api/v1/user/profile":
			return 200, `{"code":0,"data":{"id":42}}`
		case r.Method == "POST" && r.URL.Path == "/api/v1/keys":
			idempotency := r.Header.Get("Idempotency-Key")
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			postedKeys = append(postedKeys, idempotency)
			postedBodies = append(postedBodies, string(body))
			if idempotency != "" && operations[idempotency] {
				return 409, `{"code":409,"reason":"IDEMPOTENCY_IN_PROGRESS"}`
			}
			operations[idempotency] = true
			starts++
			// The request has started on the upstream. The response is lost
			// before its key becomes visible to the listing endpoint.
			return 502, `{"message":"fixture transient failure"}`
		case r.Method == "GET" && r.URL.Path == "/api/v1/keys":
			if published {
				return 200, `{"code":0,"data":{"total":1,"items":[{"id":9,"name":"governance-stable","group_id":7,"key":"sk-fixture-key"}]}}`
			}
			return 200, `{"code":0,"data":{"total":0,"items":[]}}`
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return 500, ""
		}
	})
	site := Site{Platform: "sub2api", BaseURL: "https://upstream.example"}
	session := Session{AccessToken: "fixture", UserID: 42}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := connector.EnsureKey(t.Context(), site, session, RemoteGroup{ID: "7"}, "governance-stable", nil)
		require.ErrorIs(t, err, errConnectorUncertain)
		require.Len(t, postedKeys, attempt+1, "at most one POST per explicit operation")
	}
	require.Equal(t, []string{"governance-stable", "governance-stable"}, postedKeys)
	require.Equal(t, []string{`{"group_id":7,"name":"governance-stable"}`, `{"group_id":7,"name":"governance-stable"}`}, postedBodies)
	require.Equal(t, 1, starts, "a pending upstream operation must not create a duplicate")
	published = true
	key, err := connector.EnsureKey(t.Context(), site, session, RemoteGroup{ID: "7"}, "governance-stable", nil)
	require.NoError(t, err)
	require.Equal(t, RemoteKey{ID: "9", Key: "sk-fixture-key"}, key)
	require.Len(t, postedKeys, 2, "a listed key must be reused without a creation POST")
}
