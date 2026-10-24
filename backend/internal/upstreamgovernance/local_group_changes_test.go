package upstreamgovernance

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderLocalGroupChangeNoticePreservesAuditIdentity(t *testing.T) {
	observedAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	event := LocalGroupChangeEvent{
		ID: 17, GroupID: 42, Kind: LocalGroupChangeUpdated,
		Before:    `{"name":"旧名称","status":"active","rate_multiplier":1}`,
		After:     `{"name":"新名称","status":"active","rate_multiplier":2}`,
		CreatedAt: observedAt,
	}

	notice := RenderLocalGroupChangeNotice(event)

	require.Equal(t, event.ID, notice.EventID)
	require.Equal(t, event.GroupID, notice.GroupID)
	require.Equal(t, "local_group_change", notice.Kind)
	require.Equal(t, "info", notice.Severity)
	require.Equal(t, "local-group:42:updated:17", notice.DedupKey)
	require.Equal(t, observedAt, notice.ObservedAt)
	require.Contains(t, notice.Subject, "本地分组修改")
	require.Contains(t, notice.Subject, "新名称")
	require.Equal(t, notice.AstrBotText, notice.Body)
}

func TestRenderLocalGroupChangeNoticeBuildsAstrBotTemplates(t *testing.T) {
	observedAt := time.Date(2026, 10, 22, 3, 26, 35, 0, time.UTC)
	tests := []struct {
		name  string
		event LocalGroupChangeEvent
		want  string
	}{
		{
			name: "active creation includes description and current rate",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeCreated,
				After: `{"name":"GPT Pro","status":"active","description":"稳定线路","rate_multiplier":0.06}`},
			want: "分组上线：GPT Pro 上线，描述：稳定线路，倍率0.06x",
		},
		{
			name: "empty description is explicit without a rate",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeCreated,
				After: `{"name":"GPT Pro","status":"active","description":""}`},
			want: "分组上线：GPT Pro 上线，描述：无描述",
		},
		{
			name: "deleted",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeDeleted,
				Before: `{"name":"GPT Pro","status":"active","rate_multiplier":0.06}`},
			want: "分组下线：GPT Pro 下线",
		},
		{
			name: "disabled",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","status":"active","rate_multiplier":0.06}`,
				After:  `{"name":"GPT Pro","status":"disabled","rate_multiplier":0.06}`},
			want: "分组下线：GPT Pro 下线",
		},
		{
			name: "any nonactive status is offline",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","status":"active"}`,
				After:  `{"name":"GPT Pro","status":"archived"}`},
			want: "分组下线：GPT Pro 下线",
		},
		{
			name: "reactivation includes description and rate",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","status":"disabled","description":"稳定线路","rate_multiplier":0.06}`,
				After:  `{"name":"GPT Pro","status":"active","description":"稳定线路","rate_multiplier":0.06}`},
			want: "分组上线：GPT Pro 上线，描述：稳定线路，倍率0.06x",
		},
		{
			name: "renamed",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","rate_multiplier":0.06}`,
				After:  `{"name":"GPT Pro Test","rate_multiplier":0.06}`},
			want: "分组名称调整：GPT Pro 更改为 GPT Pro Test",
		},
		{
			name: "description cleared",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","description":"稳定线路"}`,
				After:  `{"name":"GPT Pro","description":""}`},
			want: "分组描述调整：GPT Pro，原描述「稳定线路」，描述已清空",
		},
		{
			name: "description added from null",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"Name":"GPT Pro","Description":null}`,
				After:  `{"Name":"GPT Pro","Description":"新线路"}`},
			want: "分组描述调整：GPT Pro，原描述「无描述」更改为「新线路」",
		},
		{
			name: "rate increased",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","rate_multiplier":0.02}`,
				After:  `{"name":"GPT Pro","rate_multiplier":0.023}`},
			want: "分组倍率调整：GPT Pro，0.02x → 0.023x（上调）",
		},
		{
			name: "rate decreased",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","rate_multiplier":0.065}`,
				After:  `{"name":"GPT Pro","rate_multiplier":0.06}`},
			want: "分组倍率调整：GPT Pro，0.065x → 0.06x（下调）",
		},
		{
			name: "rate provided after null",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","rate_multiplier":null}`,
				After:  `{"name":"GPT Pro","rate_multiplier":0.06}`},
			want: "分组倍率调整：GPT Pro，未提供 → 0.06x",
		},
		{
			name: "rate removed",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated,
				Before: `{"name":"GPT Pro","rate_multiplier":0.06}`,
				After:  `{"name":"GPT Pro","rate_multiplier":null}`},
			want: "分组倍率调整：GPT Pro，0.06x → 未提供",
		},
		{
			name:  "missing group name uses stable identity",
			event: LocalGroupChangeEvent{Kind: LocalGroupChangeDeleted, Before: `{}`},
			want:  "分组下线：分组 #42 下线",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.event.ID, tt.event.GroupID, tt.event.CreatedAt = 20, 42, observedAt
			notice := RenderLocalGroupChangeNotice(tt.event)
			require.Equal(t, "【来至外星球的通知】\n"+tt.want+"\n通知时间：2026-10-22 11:26:35", notice.AstrBotText)
			require.Equal(t, notice.AstrBotText, notice.Body)
		})
	}
}

func TestRenderLocalGroupChangeNoticeCombinesIndependentChanges(t *testing.T) {
	observedAt := time.Date(2026, 10, 22, 3, 26, 35, 0, time.UTC)
	tests := []struct {
		name, beforeStatus, afterStatus, statusLine string
	}{
		{"disabled", "active", "disabled", "分组下线：New Pro 下线"},
		{"reactivated", "disabled", "active", "分组上线：New Pro 上线，描述：新 线路，倍率0.023x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := LocalGroupChangeEvent{
				ID: 25, GroupID: 42, Kind: LocalGroupChangeUpdated, CreatedAt: observedAt,
				Before: `{"name":"Old\r\nPro","status":"` + tt.beforeStatus + `","description":"旧\n线路","rate_multiplier":0.02,"updated_at":"2026-10-22T01:00:00Z"}`,
				After:  `{"name":"New\nPro","status":"` + tt.afterStatus + `","description":"新\r\n线路","rate_multiplier":0.023,"updated_at":"2026-10-22T01:01:02Z"}`,
			}
			notice := RenderLocalGroupChangeNotice(event)
			require.Equal(t, strings.Join([]string{
				"【来至外星球的通知】",
				tt.statusLine,
				"分组名称调整：Old Pro 更改为 New Pro",
				"分组描述调整：New Pro，原描述「旧 线路」更改为「新 线路」",
				"分组倍率调整：New Pro，0.02x → 0.023x（上调）",
				"通知时间：2026-10-22 11:26:35",
			}, "\n"), notice.AstrBotText)
			require.Equal(t, 1, strings.Count(notice.AstrBotText, "通知时间："))
			require.NotContains(t, notice.Subject, "\n")
			require.NotContains(t, notice.Body, "更新时间")
		})
	}
}

func TestRenderLocalGroupChangeNoticeFiltersEachEventIndependently(t *testing.T) {
	observedAt := time.Date(2026, 10, 22, 3, 26, 35, 0, time.UTC)
	event := LocalGroupChangeEvent{
		ID: 25, GroupID: 42, Kind: LocalGroupChangeUpdated, CreatedAt: observedAt,
		Before: `{"name":"Old Pro","status":"disabled","description":"旧线路","rate_multiplier":0.02}`,
		After:  `{"name":"New Pro","status":"active","description":"新线路","rate_multiplier":0.023}`,
	}
	tests := []struct {
		kind string
		line string
	}{
		{"group_online", "分组上线：New Pro 上线，描述：新线路，倍率0.023x"},
		{"group_offline", "分组下线：New Pro 下线"},
		{"group_name_change", "分组名称调整：Old Pro 更改为 New Pro"},
		{"group_description_change", "分组描述调整：New Pro，原描述「旧线路」更改为「新线路」"},
		{"group_rate_change", "分组倍率调整：New Pro，0.02x → 0.023x（上调）"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			events := localGroupTestEvents(t, map[string]bool{tt.kind: true})
			filteredEvent := event
			if tt.kind == "group_offline" {
				filteredEvent.Before, filteredEvent.After = event.After, event.Before
				tt.line = "分组下线：Old Pro 下线"
			}
			notice := RenderLocalGroupChangeNoticeWithEvents(filteredEvent, &events)
			require.Len(t, notice.GroupChanges, 1)
			require.Equal(t, tt.kind, notice.GroupChanges[0].Kind)
			require.Equal(t, "【来至外星球的通知】\n"+tt.line+"\n通知时间：2026-10-22 11:26:35", notice.AstrBotText)
			require.Equal(t, notice.AstrBotText, notice.Body)
		})
	}
	events := localGroupTestEvents(t, nil)
	notice := RenderLocalGroupChangeNoticeWithEvents(event, &events)
	require.Empty(t, notice.GroupChanges)
	require.Empty(t, notice.AstrBotText)
	require.Empty(t, notice.Body)
}

func TestRenderLocalGroupChangeNoticeIsSilentWithoutActualGroupEvents(t *testing.T) {
	tests := []struct {
		name  string
		event LocalGroupChangeEvent
	}{
		{"disabled creation", LocalGroupChangeEvent{Kind: LocalGroupChangeCreated, After: `{"name":"GPT Pro","status":"disabled","description":"尚未上线","rate_multiplier":0.06}`}},
		{"unchanged update", LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated, Before: `{"name":"GPT Pro","status":"active","description":"稳定","rate_multiplier":0.06}`, After: `{"name":"GPT Pro","status":"active","description":"稳定","rate_multiplier":0.06}`}},
		{"unrelated configuration update", LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated, Before: `{"name":"GPT Pro","status":"active","sort_order":1,"updated_at":"2026-10-22T01:00:00Z"}`, After: `{"name":"GPT Pro","status":"active","sort_order":2,"updated_at":"2026-10-22T01:01:02Z"}`}},
		{"inactive status remains inactive", LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated, Before: `{"name":"GPT Pro","status":"disabled"}`, After: `{"name":"GPT Pro","status":"archived"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.event.ID, tt.event.GroupID = 26, 42
			notice := RenderLocalGroupChangeNotice(tt.event)
			require.Empty(t, notice.AstrBotText)
			require.Empty(t, notice.Body)
		})
	}
}

func TestLocalGroupChangeEventValidate(t *testing.T) {
	require.Error(t, (LocalGroupChangeEvent{GroupID: 9, Kind: LocalGroupChangeUpdated}).Validate())
	require.NoError(t, (LocalGroupChangeEvent{GroupID: 9, Kind: LocalGroupChangeUpdated, Before: `{}`, After: `{}`}).Validate())
}
