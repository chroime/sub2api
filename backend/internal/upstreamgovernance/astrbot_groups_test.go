package upstreamgovernance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type astrBotChangingSettingsStore struct {
	GlobalSettingsStore
	reads         int
	first, second string
}

func (s *astrBotChangingSettingsStore) GetMultiple(context.Context, []string) (map[string]string, error) {
	s.reads++
	if s.reads == 1 {
		return map[string]string{GlobalSettingsKey: s.first}, nil
	}
	return map[string]string{GlobalSettingsKey: s.second}, nil
}

func TestAstrBotGroupsUsesOneCredentialAndAddressSnapshot(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"complete":true,"groups":[]}}`))
	}))
	defer server.Close()
	store := &astrBotChangingSettingsStore{
		first:  `{"astrbot":{"base_url":"` + server.URL + `","api_key":"first-key"}}`,
		second: `{"astrbot":{"base_url":"https://other.example","api_key":"other-key"}}`,
	}
	svc := NewService(nil, nil, nil, nil, false)
	svc.SetGlobalSettingsStore(store)
	_, err := svc.ListAstrBotGroups(t.Context(), AstrBotGroupsQuery{BaseURL: server.URL, TimeoutSeconds: 5})
	require.NoError(t, err)
	require.Equal(t, "Bearer first-key", authorization)
	require.Equal(t, 1, store.reads)
}

func TestAstrBotGroupsUsesCurrentConnectionAndSavedKeyWithoutSavingDraft(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/im/groups", r.URL.Path)
		require.Equal(t, "Bearer saved-secret", r.Header.Get("Authorization"))
		require.Equal(t, http.MethodGet, r.Method)
		_, _ = w.Write([]byte(`{"status":"ok","data":{"complete":true,"groups":[{"umo":"外星机器人:GroupMessage:1139252052","bot_id":"外星机器人","group_id":"1139252052","group_name":"外星人AI"}]}}`))
	}))
	defer server.Close()
	store := &globalSettingsMemoryStore{values: map[string]string{GlobalSettingsKey: `{"version":3,"astrbot":{"base_url":"` + server.URL + `","api_key":"saved-secret"}}`}}
	svc := NewService(nil, nil, nil, fakeCipher{}, true)
	svc.SetGlobalSettingsStore(store)
	before := store.values[GlobalSettingsKey]
	groups, err := svc.ListAstrBotGroups(t.Context(), AstrBotGroupsQuery{BaseURL: server.URL, TimeoutSeconds: 5})
	require.NoError(t, err)
	require.True(t, groups.Complete)
	require.Equal(t, []AstrBotGroup{{UMO: "外星机器人:GroupMessage:1139252052", BotID: "外星机器人", GroupID: "1139252052", GroupName: "外星人AI"}}, groups.Groups)
	require.Equal(t, before, store.values[GlobalSettingsKey])
}

func TestAstrBotGroupsDoesNotSendSavedKeyToChangedAddress(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	svc := NewService(nil, nil, nil, nil, false)
	svc.SetGlobalSettingsStore(&globalSettingsMemoryStore{values: map[string]string{GlobalSettingsKey: `{"astrbot":{"base_url":"https://original.example","api_key":"saved-secret"}}`}})
	_, err := svc.ListAstrBotGroups(t.Context(), AstrBotGroupsQuery{BaseURL: server.URL, TimeoutSeconds: 5})
	var queryErr *AstrBotGroupsError
	require.ErrorAs(t, err, &queryErr)
	require.Equal(t, "astrbot_not_configured", queryErr.Code)
	require.Zero(t, requests)
}

func TestAstrBotGroupsBaseURLMayAlreadyContainAPIVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/im/groups", r.URL.Path)
		_, _ = w.Write([]byte(`{"status":"ok","data":{"complete":true,"groups":[]}}`))
	}))
	defer server.Close()
	_, err := NewService(nil, nil, nil, nil, false).ListAstrBotGroups(t.Context(), AstrBotGroupsQuery{BaseURL: server.URL + "/api/v1", APIKey: "test-key", TimeoutSeconds: 5})
	require.NoError(t, err)
}

func TestAstrBotGroupsAcceptsDraftKeyAndDisablesRedirects(t *testing.T) {
	requests := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer draft-secret", r.Header.Get("Authorization"))
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	svc := NewService(nil, nil, nil, nil, false)
	_, err := svc.ListAstrBotGroups(t.Context(), AstrBotGroupsQuery{BaseURL: server.URL, APIKey: "draft-secret", TimeoutSeconds: 5})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "draft-secret")
	require.Zero(t, requests)
}

func TestAstrBotGroupsRejectsFailedPartialAndHistoricalResponses(t *testing.T) {
	for _, tt := range []struct {
		name, body, code string
		status           int
	}{
		{"unauthorized", "secret-canary", "astrbot_auth_failed", 401},
		{"scope", "secret-canary", "astrbot_scope_required", 403},
		{"old version", "secret-canary", "astrbot_groups_unsupported", 404},
		{"offline", "secret-canary", "astrbot_unavailable", 503},
		{"logical failure", `{"status":"error","message":"secret-canary"}`, "astrbot_unavailable", 200},
		{"partial", `{"status":"ok","data":{"complete":false,"groups":[]}}`, "astrbot_groups_incomplete", 200},
		{"history only", `{"status":"ok","data":{"umos":["qq:GroupMessage:123"]}}`, "astrbot_groups_incomplete", 200},
		{"invalid", "<html>secret-canary</html>", "astrbot_invalid_response", 200},
		{"private target", `{"status":"ok","data":{"complete":true,"groups":[{"umo":"qq:FriendMessage:123","bot_id":"qq","group_id":"123","group_name":"friend"}]}}`, "astrbot_invalid_response", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := NewService(nil, nil, nil, nil, false).ListAstrBotGroups(t.Context(), AstrBotGroupsQuery{BaseURL: server.URL, APIKey: "test-key", TimeoutSeconds: 5})
			var queryErr *AstrBotGroupsError
			require.ErrorAs(t, err, &queryErr)
			require.Equal(t, tt.code, queryErr.Code)
			require.NotContains(t, err.Error(), "secret-canary")
		})
	}
}

func TestAstrBotGroupsEmptyLiveListAndContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","data":{"complete":true,"groups":[]}}`))
	}))
	defer server.Close()
	svc := NewService(nil, nil, nil, nil, false)
	query := AstrBotGroupsQuery{BaseURL: server.URL, APIKey: "test-key", TimeoutSeconds: 5}
	result, err := svc.ListAstrBotGroups(t.Context(), query)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.NotNil(t, result.Groups)
	require.Empty(t, result.Groups)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.ListAstrBotGroups(ctx, query)
	require.Error(t, err)
	for _, invalid := range []string{"file:///etc/passwd", "https://user:pass@example.test", "https://example.test?token=" + strings.Repeat("x", 2)} {
		query.BaseURL = invalid
		_, err := svc.ListAstrBotGroups(t.Context(), query)
		require.Error(t, err)
	}
}
