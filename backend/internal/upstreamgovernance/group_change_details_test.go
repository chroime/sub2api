package upstreamgovernance

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupEventSwitchesAreIndependent(t *testing.T) {
	keys := []string{GroupEventOnline, GroupEventOffline, GroupEventNameChange, GroupEventDescriptionChange, GroupEventRateChange}
	for i, events := range []GlobalNotificationEvents{
		{GroupOnline: true}, {GroupOffline: true}, {GroupNameChange: true}, {GroupDescriptionChange: true}, {GroupRateChange: true},
	} {
		require.True(t, events.HasGroupEvents())
		for j, key := range keys {
			require.Equal(t, i == j, events.AllowsGroupEvent(key), key)
		}
		require.False(t, events.AllowsGroupEvent("local_group_change"))
	}
	require.False(t, (GlobalNotificationEvents{}).HasGroupEvents())
}

func TestGroupChangeMessageHasOneFinalTimestampAndOneLinePerDetail(t *testing.T) {
	observed := time.Date(2026, 10, 23, 6, 58, 18, 0, time.UTC)
	text := renderGroupChangeMessage([]GroupChangeDetail{
		{Kind: GroupEventNameChange, Text: "分组名称调整：A 更改为 B"},
		{Kind: GroupEventDescriptionChange, Text: "分组描述调整：B，原描述「旧」更改为「新」"},
	}, observed, "上游名称：站点\n甲")
	require.Equal(t, "【来至外星球的通知】\n上游名称：站点 甲\n分组名称调整：A 更改为 B\n分组描述调整：B，原描述「旧」更改为「新」\n通知时间：2026-10-23 14:58:18", text)
	require.Equal(t, 1, strings.Count(text, "通知时间："))
	require.Empty(t, renderGroupChangeMessage(nil, observed))
	require.Equal(t, "无描述", groupNoticeDescription(" \n "))
	require.Equal(t, "a b c", singleLineGroupValue(" a\r\nb\tc "))
}

func TestGroupRateChangeShowsOnlyActualNumericChanges(t *testing.T) {
	before, after := 0.01, 0.02
	require.Equal(t, "分组倍率调整：OpenAI 002，0.01x → 0.02x（上调）", renderGroupRateChange("OpenAI 002", &before, &after))
	require.Equal(t, "分组倍率调整：OpenAI 002，0.02x → 0.01x（下调）", renderGroupRateChange("OpenAI 002", &after, &before))
	require.Empty(t, renderGroupRateChange("OpenAI", &before, &before))
	require.Empty(t, renderGroupRateChange("OpenAI", nil, nil))
	require.Equal(t, "分组倍率调整：OpenAI，未提供 → 0.02x", renderGroupRateChange("OpenAI", nil, &after))
	invalid := math.NaN()
	require.NotContains(t, renderGroupRateChange("OpenAI", &invalid, &after), "NaN")
}
