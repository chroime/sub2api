package upstreamgovernance

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCatalogRateNoticeShowsActualMultiplierChange(t *testing.T) {
	observedAt := time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC)
	notice := renderCatalogChangeNotice(
		Site{ID: 7, Name: "示例上游", BaseURL: "https://up.example"},
		Event{Kind: "rate_changed", Resource: "claude", Before: `{"Name":"Claude Max","Resolved":0.9}`, After: `{"Name":"Claude Max","Resolved":0.93}`, CreatedAt: observedAt},
		Catalog{Groups: []RemoteGroup{{ID: "claude", Name: "Claude Max"}}},
	)

	require.Contains(t, notice.Body, "分组倍率调整：Claude Max，0.9x → 0.93x（上调）")
	require.Contains(t, notice.Body, "上游名称：示例上游\n站点URL：https://up.example")
	require.True(t, strings.HasPrefix(notice.Body, "【来至外星球的通知】\n"))
	require.True(t, strings.HasSuffix(notice.Body, "通知时间：2026-10-23 09:02:03"))
	require.Equal(t, 1, strings.Count(notice.Body, "通知时间："))
	require.NotContains(t, notice.Body, "0.030000000000000027")
	require.NotContains(t, notice.Body, "Resolved")
}

func TestCatalogRateNoticeHandlesZeroBaseline(t *testing.T) {
	notice := renderCatalogChangeNotice(
		Site{ID: 7},
		Event{Kind: "rate_changed", Resource: "zero", Before: `{"Name":"Zero","Resolved":0}`, After: `{"Name":"Zero","Resolved":0.2}`},
		Catalog{},
	)

	require.Contains(t, notice.Body, "分组倍率调整：Zero，0x → 0.2x（上调）")
	require.NotContains(t, notice.Body, "Inf")
	require.NotContains(t, notice.Body, "NaN")
}

func TestCatalogChangeNoticeSubjectUsesGroupNameForRateAndDescriptionChanges(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "rate-only change",
			event: Event{Kind: "rate_changed", Resource: "42", Before: `{"Resolved":0.01}`, After: `{"Resolved":0.02}`},
			want:  "上游倍率变更：示例上游 - OpenAI 001",
		},
		{
			name:  "description-only change",
			event: Event{Kind: "group_changed", Resource: "42", Before: `["OpenAI 001","旧描述","openai"]`, After: `["OpenAI 001","新描述","openai"]`},
			want:  "上游分组描述调整：示例上游 - OpenAI 001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notice := renderCatalogChangeNotice(
				Site{ID: 7, Name: "示例上游"},
				tt.event,
				Catalog{Groups: []RemoteGroup{{ID: "42", Name: "OpenAI 001"}}},
			)
			require.Equal(t, tt.want, notice.Subject)
			require.NotContains(t, notice.Subject, "分组描述调整：OpenAI 001，")
			require.NotContains(t, notice.Subject, "分组倍率调整：OpenAI 001，")
		})
	}
}

func TestCatalogChangeNoticeSubjectDoesNotParseGroupNameFromDisplayText(t *testing.T) {
	notice := renderCatalogChangeNotice(
		Site{ID: 7, Name: "示例上游"},
		Event{Kind: "group_added", Resource: "42", After: `{"id":"42","name":"QA 上线 001","description":"新测试组"}`},
		Catalog{},
	)

	require.Equal(t, "上游分组上线：示例上游 - QA 上线 001", notice.Subject)
}

func TestCatalogGroupNoticeProjectsOnlineAndOfflineDetails(t *testing.T) {
	for _, tt := range []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "online includes description and current rate",
			event: Event{Kind: "group_added", Resource: "42", After: `{"id":"42","name":"Claude Max","description":"可用分组","resolved_rate_multiplier":0.9}`},
			want:  "分组上线：Claude Max 上线，描述：可用分组，倍率0.9x",
		},
		{
			name:  "online explicitly identifies empty description",
			event: Event{Kind: "group_added", Resource: "42", After: `{"id":"42","name":"Claude Max"}`},
			want:  "分组上线：Claude Max 上线，描述：无描述",
		},
		{
			name:  "offline uses previous name",
			event: Event{Kind: "group_removed", Resource: "42", Before: `{"id":"42","name":"Claude Max"}`},
			want:  "分组下线：Claude Max 下线",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			notice := renderCatalogChangeNotice(Site{ID: 7}, tt.event, Catalog{})
			require.Contains(t, notice.Body, tt.want)
			require.NotContains(t, notice.Body, "分组下线：42")
		})
	}
}

func TestCatalogGroupNoticeDescribesRenameAndDescriptionClearing(t *testing.T) {
	notice := renderCatalogChangeNotice(
		Site{ID: 7},
		Event{Kind: "group_changed", Resource: "42", Before: `["旧名称","旧描述","openai"]`, After: `["新名称","","openai"]`},
		Catalog{Groups: []RemoteGroup{{ID: "42", Name: "新名称"}}},
	)

	require.Contains(t, notice.Body, "分组名称调整：旧名称 更改为 新名称\n分组描述调整：新名称，原描述「旧描述」，描述已清空")
	require.Equal(t, 1, strings.Count(notice.Body, "通知时间："))
	require.NotContains(t, notice.Body, "名称、描述或协议")
}

func TestCatalogGroupNoticeDescribesNewDescriptionFromEmpty(t *testing.T) {
	notice := renderCatalogChangeNotice(
		Site{ID: 7},
		Event{Kind: "group_changed", Resource: "42", Before: `["名称","","openai"]`, After: `["名称","新描述","openai"]`},
		Catalog{},
	)

	require.Contains(t, notice.Body, "分组描述调整：名称，原描述「无描述」更改为「新描述」")
}

func TestCatalogGroupNoticeDoesNotInventEventsForUnrelatedAuditChanges(t *testing.T) {
	for _, event := range []Event{
		{Kind: "group_changed", Resource: "42", Before: `["名称","描述","openai"]`, After: `["名称","描述","claude"]`},
		{Kind: "models_changed", Resource: "42", Before: `["old"]`, After: `["new"]`},
		{Kind: "price_changed", Resource: "42", Before: `[]`, After: `[{"model":"new"}]`},
		{Kind: "channels_changed", Resource: "channels", Before: `[]`, After: `[{"name":"new"}]`},
		{Kind: "rate_changed", Resource: "42", Before: `{"Resolved":0.8,"Enabled":false}`, After: `{"Resolved":0.8,"Enabled":true}`},
	} {
		t.Run(event.Kind+event.After, func(t *testing.T) {
			notice := renderCatalogChangeNotice(Site{ID: 7}, event, Catalog{})
			require.Empty(t, notice.Body)
		})
	}
}

func TestCatalogGroupNoticeCannotInjectExtraEventLines(t *testing.T) {
	notice := renderCatalogChangeNotice(
		Site{ID: 7, Name: "上游\n名称", BaseURL: "https://up.example"},
		Event{Kind: "group_changed", Resource: "42", Before: `["旧名称","旧\n描述","openai"]`, After: `["新\r\n名称","新\n描述","openai"]`},
		Catalog{},
	)

	require.Contains(t, notice.Body, "分组名称调整：旧名称 更改为 新 名称")
	require.Contains(t, notice.Body, "分组描述调整：新 名称，原描述「旧 描述」更改为「新 描述」")
	require.Equal(t, 1, strings.Count(notice.Body, "分组名称调整："))
	require.Equal(t, 1, strings.Count(notice.Body, "分组描述调整："))
	require.NotContains(t, notice.Body, "\r")
}
