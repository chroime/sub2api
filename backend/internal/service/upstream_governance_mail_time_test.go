package service

import (
	"testing"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceMailDatesUseBeijingTimeAcrossDayBoundary(t *testing.T) {
	for _, stamp := range []string{
		"2026-10-01T16:08:02Z",
		"2026-10-01T10:08:02-06:00",
		"2026-10-02T00:08:02+08:00",
	} {
		t.Run(stamp, func(t *testing.T) {
			observed, err := time.Parse(time.RFC3339, stamp)
			require.NoError(t, err)
			for _, kind := range []string{"balance", "key", "model", "change"} {
				t.Run(kind, func(t *testing.T) {
					repo := newNotificationEmailMemorySettingRepo()
					server := startNotificationEmailTestSMTPServer(t)
					require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
					n := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}
					var err error
					switch kind {
					case "balance":
						err = n.Send(t.Context(), "admin@example.test", gov.BalanceNotice{SiteID: 5, SiteName: "测试上游", BaseURL: "https://upstream.example.test", ObservedAt: observed})
					case "key":
						err = n.SendKey(t.Context(), "admin@example.test", gov.KeyNotice{SiteID: 5, SiteName: "测试上游", BaseURL: "https://upstream.example.test", ObservedAt: observed})
					case "model":
						err = n.SendModel(t.Context(), "admin@example.test", gov.ModelNotice{SiteID: 5, SiteName: "测试上游", BaseURL: "https://upstream.example.test", ObservedAt: observed})
					case "change":
						err = n.SendChange(t.Context(), "admin@example.test", gov.ChangeNotice{SiteID: 5, SiteName: "测试上游", BaseURL: "https://upstream.example.test", Subject: "倍率变更", ObservedAt: observed})
					}
					require.NoError(t, err)
					body := server.lastMessageBody(t)
					require.Contains(t, body, "2026-10-02 00:08:02")
				require.Contains(t, body, "北京时间（UTC+08:00）")
					require.NotContains(t, body, "(UTC)")
				})
			}
		})
	}
}

func TestGovernanceChangeMailLocalizesTechnicalLabels(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	server := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(t.Context(), server.settings()))
	n := &governanceBalanceNotifier{mail: NewEmailService(repo, nil)}

	require.NoError(t, n.SendChange(t.Context(), "admin@example.test", gov.ChangeNotice{
		SiteID:     5,
		SiteName:   "测试上游",
		BaseURL:    "https://upstream.example.test",
		Kind:       "rate_change",
		Severity:   "warning",
		Subject:    "倍率变更",
		Body:       "上游倍率发生变化。",
		ObservedAt: time.Date(2026, 10, 2, 0, 8, 2, 0, time.UTC),
	}))

	body := server.lastMessageBody(t)
	require.Contains(t, body, "倍率变化")
	require.Contains(t, body, "警告")
	require.NotContains(t, body, "rate_change")
	require.NotContains(t, body, "warning")
}
