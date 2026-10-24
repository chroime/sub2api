package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceAstrBotNotifierNormalizesAPIVersionForDeliveryAndFallback(t *testing.T) {
	for _, tt := range []struct {
		name, basePath, endpointPath, expectedPath string
	}{
		{"default versioned path", "", "/api/v1/im/message", "/api/v1/im/message"},
		{"base and endpoint versioned", "/api/v1", "/api/v1/im/message", "/api/v1/im/message"},
		{"unversioned IM path", "", "/im/message", "/api/v1/im/message"},
		{"versioned base and IM path", "/api/v1", "/im/message", "/api/v1/im/message"},
		{"proxy prefix and default path", "/astrbot", "/api/v1/im/message", "/astrbot/api/v1/im/message"},
		{"proxy prefix and IM path", "/astrbot", "/im/message", "/astrbot/api/v1/im/message"},
		{"proxy prefix and both versioned", "/astrbot/api/v1/", "/api/v1/im/message", "/astrbot/api/v1/im/message"},
		{"custom endpoint", "", "/api/message", "/api/message"},
		{"custom proxy endpoint", "/astrbot", "/custom/message", "/astrbot/custom/message"},
		{"versioned base custom endpoint", "/api/v1", "/custom/message", "/api/v1/custom/message"},
		{"different API version", "/api/v2", "/custom/message", "/api/v2/custom/message"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, useFallback := range []bool{false, true} {
				var paths, authorization []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					paths = append(paths, r.URL.Path)
					authorization = append(authorization, r.Header.Get("Authorization"))
					if useFallback && len(paths) == 1 {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
				repo := newNotificationEmailMemorySettingRepo()
				raw, err := json.Marshal(gov.GlobalSettings{Version: 1, AstrBot: gov.AstrBotSettings{
					Enabled: true, BaseURL: server.URL + tt.basePath, EndpointPath: tt.endpointPath, APIKey: "endpoint-test-secret",
				}})
				require.NoError(t, err)
				require.NoError(t, repo.Set(t.Context(), gov.GlobalSettingsKey, string(raw)))
				adapter := &governanceBalanceNotifier{settings: repo}
				err = adapter.SendChange(t.Context(), "qq:bot:GroupMessage:123", gov.ChangeNotice{Subject: "Endpoint check"})
				server.Close()
				require.NoError(t, err)
				expectedPaths := []string{tt.expectedPath}
				expectedAuthorization := []string{"Bearer endpoint-test-secret"}
				if useFallback {
					expectedPaths = append(expectedPaths, strings.TrimSuffix(tt.expectedPath, "/message")+"/messages")
					expectedAuthorization = append(expectedAuthorization, "Bearer endpoint-test-secret")
				}
				require.Equal(t, expectedPaths, paths)
				require.Equal(t, expectedAuthorization, authorization)
			}
		})
	}
}
