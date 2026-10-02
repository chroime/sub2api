package upstreamgovernance

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

var governanceTestPlatforms = []string{"openai", "anthropic", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go"}

func TestGovernancePlatformImportsAndTemplates(t *testing.T) {
	for _, platform := range governanceTestPlatforms {
		t.Run(platform, func(t *testing.T) {
			s, store, connector, local := setupEngine(t)
			connector.catalog.Groups[0].Platform = platform
			snapshot, err := s.Sync(t.Context(), 1)
			require.NoError(t, err)
			selected := selections()
			selected[0].Platform = platform
			keys, err := s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: platform}}})
			require.NoError(t, err)
			require.Equal(t, "created", keys.Items[0].Status)
			preview, err := s.Preview(t.Context(), 1, selected)
			require.NoError(t, err)
			require.False(t, preview.Rows[0].WillCreateKey)
			result, err := s.Apply(t.Context(), 1, preview.ID)
			require.NoError(t, err)
			require.Equal(t, "applied", result.Items[0].Status)
			require.Equal(t, platform, local.changes[0].Platform)
			require.Equal(t, platform, store.bindings[0].Platform)
			require.Equal(t, platform, store.keys[0].Platform)
			require.Equal(t, 1, connector.keyCalls, "import must reuse the reviewed managed key")
			_, err = normalizeModelTemplates(ModelTemplates{Templates: []ModelTemplate{{ID: "native", Name: "Native", Platform: platform, Models: []string{"text-model"}, IsDefault: true}}})
			require.NoError(t, err)
		})
	}
}

func TestGovernanceTransportCompatibility(t *testing.T) {
	for _, selected := range governanceTestPlatforms {
		for _, remote := range governanceTestPlatforms {
			want := remote == selected || remote == "grok" && selected == "openai"
			require.Equal(t, want, compatibleTransport(remote, selected), "%s → %s", remote, selected)
		}
		for _, remote := range []string{"", "unknown", "composite"} {
			require.True(t, compatibleTransport(remote, selected))
		}
		require.False(t, compatibleTransport("custom", selected))
	}
	for _, invalid := range []string{"", "unknown", "composite", "newapi", "custom"} {
		require.False(t, validTransport(invalid))
		require.False(t, compatibleTransport("unknown", invalid))
	}
}

func TestGovernanceNewAPIRejectsAntigravityBeforeSideEffects(t *testing.T) {
	s, store, connector, local := setupEngine(t)
	store.site.Platform = "newapi"
	connector.catalog.Groups[0].Platform = "unknown"
	snapshot, err := s.Sync(t.Context(), 1)
	require.NoError(t, err)
	selected := selections()
	selected[0].Platform = "antigravity"
	_, err = s.Preview(t.Context(), 1, selected)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.CreateKeys(t.Context(), 1, CreateKeysInput{SnapshotID: snapshot.ID, Selections: []KeySelection{{RemoteGroupID: "8", Platform: "antigravity"}}})
	require.ErrorIs(t, err, ErrInvalid)
	require.Zero(t, connector.keyCalls)
	require.Zero(t, local.calls)
	require.Empty(t, store.keys)
}

func TestConnectorProbesAllNativeAPIKeyPlatforms(t *testing.T) {
	for _, platform := range governanceTestPlatforms {
		for _, model := range []string{"claude-fixture", "gemini-fixture"} {
			t.Run(platform+"/"+model, func(t *testing.T) {
				path, authHeader, authValue := "/v1/chat/completions", "Authorization", "Bearer fixture-key"
				response := `{"choices":[{"message":{"content":"OK"}}]}`
				if platform == "anthropic" || platform == "antigravity" && model == "claude-fixture" {
					path, authHeader, authValue = "/v1/messages", "x-api-key", "fixture-key"
					response = `{"content":[{"type":"text","text":"OK"}]}`
				} else if platform == "gemini" || platform == "antigravity" && model == "gemini-fixture" {
					path, authHeader, authValue = "/v1beta/models/"+model+":generateContent", "x-goog-api-key", "fixture-key"
					response = `{"candidates":[{"content":{"parts":[{"text":"OK"}]}}]}`
				}
				if platform == "antigravity" {
					path = "/antigravity" + path
				}
				connector := fixtureConnector(t, func(r *http.Request) (int, string) {
					require.Equal(t, path, r.URL.Path)
					require.Equal(t, authValue, r.Header.Get(authHeader))
					require.Equal(t, http.MethodPost, r.Method)
					var payload map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
					if authHeader != "x-goog-api-key" {
						require.Equal(t, model, payload["model"])
					}
					return http.StatusOK, response
				})
				result, err := connector.Probe(t.Context(), Site{Platform: "sub2api", BaseURL: "https://upstream.example"}, RemoteKey{Key: "fixture-key"}, platform, model)
				require.NoError(t, err)
				require.True(t, result.Success)
			})
		}
	}
	connector := fixtureConnector(t, func(*http.Request) (int, string) {
		t.Fatal("unsupported Antigravity/NewAPI probe must not issue a request")
		return 500, ""
	})
	_, err := connector.Probe(t.Context(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, RemoteKey{Key: "fixture-key"}, "antigravity", "claude-fixture")
	require.ErrorIs(t, err, ErrUnsupported)
}
