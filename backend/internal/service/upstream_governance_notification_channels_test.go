package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceRecipientsKeepEmailAndQQChannelsSeparate(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	raw, err := json.Marshal(gov.GlobalSettings{
		Version:       1,
		Notifications: gov.GlobalNotificationSettings{Enabled: true, Recipients: []string{"ops@example.com"}},
		AstrBot: gov.AstrBotSettings{
			Enabled: true, BaseURL: "https://astrbot.example", EndpointPath: "/api/v1/im/message", APIKey: "qq-secret",
			Targets: []gov.AstrBotTarget{{UMO: "bot:GroupMessage:1_2", Enabled: true}}, TimeoutSeconds: 10,
		},
	})
	require.NoError(t, repo.Set(t.Context(), "upstream_governance_global_settings", string(raw)))
	adapter := &governanceBalanceNotifier{settings: repo}
	recipients, err := adapter.Recipients(context.Background(), nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ops@example.com"}, recipients)
	local := &governanceLocalGroupNotifier{base: adapter}
	localRecipients, err := local.Recipients(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"qq:bot:GroupMessage:1_2"}, localRecipients)
}

func TestGovernanceAstrBotRecipientsDoNotDependOnEmailSettings(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	raw, err := json.Marshal(gov.GlobalSettings{
		Version: 1,
		AstrBot: gov.AstrBotSettings{
			Enabled: true, BaseURL: "https://astrbot.example", APIKey: "qq-secret",
			Events: gov.GlobalNotificationEvents{LocalGroupChange: true},
			Targets: []gov.AstrBotTarget{
				{UMO: "bot:GroupMessage:1_2", Enabled: true},
				{UMO: "bot:GroupMessage:disabled", Enabled: false},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Set(t.Context(), gov.GlobalSettingsKey, string(raw)))
	adapter := &governanceLocalGroupNotifier{base: &governanceBalanceNotifier{settings: repo}}
	recipients, err := adapter.Recipients(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"qq:bot:GroupMessage:1_2"}, recipients)
}

func TestGovernanceEmailRecipientsDoNotDependOnAstrBot(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	raw, err := json.Marshal(gov.GlobalSettings{
		Version:       1,
		Notifications: gov.GlobalNotificationSettings{Enabled: true, Recipients: []string{"ops@example.com"}},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Set(t.Context(), gov.GlobalSettingsKey, string(raw)))
	adapter := &governanceBalanceNotifier{settings: repo}
	recipients, err := adapter.Recipients(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ops@example.com"}, recipients)
}

func TestGovernanceChangeNotifierSendsAstrBotRequest(t *testing.T) {
	var paths []string
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	repo := newNotificationEmailMemorySettingRepo()
	raw, err := json.Marshal(gov.GlobalSettings{
		Version:       1,
		Notifications: gov.GlobalNotificationSettings{Enabled: true},
		AstrBot:       gov.AstrBotSettings{Enabled: true, BaseURL: server.URL, EndpointPath: "/api/v1/im/message", APIKey: "qq-secret", Targets: []gov.AstrBotTarget{{UMO: "bot:GroupMessage:1_2", Enabled: true}}, TimeoutSeconds: 10},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Set(t.Context(), "upstream_governance_global_settings", string(raw)))
	adapter := &governanceBalanceNotifier{settings: repo}
	notice := gov.ChangeNotice{Subject: "分组变化", Body: "邮件详细通知", AstrBotText: "【来至外星球的通知】\n\n统一QQ通知", Kind: "group_change"}
	require.NoError(t, adapter.SendChange(t.Context(), "qq:bot:GroupMessage:1_2", notice))
	require.Equal(t, []string{"/api/v1/im/message"}, paths)
	require.Contains(t, bodies[0], `"umo":"bot:GroupMessage:1_2"`)
	require.Contains(t, bodies[0], "统一QQ通知")
	require.NotContains(t, bodies[0], "邮件详细通知")
}
