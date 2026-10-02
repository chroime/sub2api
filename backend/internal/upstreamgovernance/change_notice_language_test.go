package upstreamgovernance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFastObservationNoticeIsChineseOnly(t *testing.T) {
	notice := renderFastObservationChangeNotice(Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"}, 3, time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC))

	require.Equal(t, "上游可见分组或倍率发生变化", notice.Subject)
	require.Contains(t, notice.Body, "上游名称：示例上游")
	require.Contains(t, notice.Body, "站点URL：https://up.example")
	assertNoEnglishNoticeLabels(t, notice.Subject+"\n"+notice.Body)
}

func TestPricingOperationNoticeIsChineseOnly(t *testing.T) {
	notice := renderPricingOperationNotice(Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"}, PricingOperation{
		LocalGroupID: 12,
		Status:       "applied",
		Reason:       "price_ready",
		Decision: PricingDecision{
			Cost:       0.2,
			TargetSale: 0.3,
			SourceID:   "site:7/binding:9",
		},
	})

	require.Contains(t, notice.Subject, "本地分组售价已自动调整")
	require.Contains(t, notice.Body, "上游名称：示例上游")
	require.Contains(t, notice.Body, "站点URL：https://up.example")
	require.Contains(t, notice.Body, "本地分组编号：12")
	assertNoEnglishNoticeLabels(t, notice.Subject+"\n"+notice.Body)
	require.NotContains(t, notice.Body, "{")
}

func assertNoEnglishNoticeLabels(t *testing.T, value string) {
	t.Helper()
	for _, label := range []string{"Upstream", "Local", "Status", "Reason", "Cost", "Target", "Source", "The", "Review"} {
		require.NotContains(t, value, label)
	}
}
