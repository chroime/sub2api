package upstreamgovernance

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	GroupEventOnline            = "group_online"
	GroupEventOffline           = "group_offline"
	GroupEventNameChange        = "group_name_change"
	GroupEventDescriptionChange = "group_description_change"
	GroupEventRateChange        = "group_rate_change"
)

// GroupChangeDetail keeps independently selectable changes together until the
// channel and site policies have filtered them, before rendering the message.
type GroupChangeDetail struct {
	Kind      string
	GroupName string
	Text      string
}

func (events GlobalNotificationEvents) AllowsGroupEvent(kind string) bool {
	switch kind {
	case GroupEventOnline:
		return events.GroupOnline
	case GroupEventOffline:
		return events.GroupOffline
	case GroupEventNameChange:
		return events.GroupNameChange
	case GroupEventDescriptionChange:
		return events.GroupDescriptionChange
	case GroupEventRateChange:
		return events.GroupRateChange
	default:
		return false
	}
}

func (events GlobalNotificationEvents) HasGroupEvents() bool {
	return events.GroupOnline || events.GroupOffline || events.GroupNameChange || events.GroupDescriptionChange || events.GroupRateChange
}

func singleLineGroupValue(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

func groupNoticeDescription(raw string) string {
	value := singleLineGroupValue(raw)
	if value == "" {
		return "无描述"
	}
	return value
}

func formatLocalGroupTime(value time.Time) string {
	return value.In(time.FixedZone("北京时间", 8*60*60)).Format("2006-01-02 15:04:05")
}

func renderGroupChangeMessage(details []GroupChangeDetail, observedAt time.Time, contextLines ...string) string {
	if len(details) == 0 {
		return ""
	}
	lines := []string{"【来至外星球的通知】"}
	for _, line := range contextLines {
		if line = singleLineGroupValue(line); line != "" {
			lines = append(lines, line)
		}
	}
	count := 0
	for _, detail := range details {
		if line := singleLineGroupValue(detail.Text); line != "" {
			lines = append(lines, line)
			count++
		}
	}
	if count == 0 {
		return ""
	}
	lines = append(lines, "通知时间："+formatLocalGroupTime(observedAt))
	return strings.Join(lines, "\n")
}

func renderGroupRateChange(name string, before, after *float64) string {
	valid := func(value *float64) bool { return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) }
	beforeValid, afterValid := valid(before), valid(after)
	if !beforeValid && !afterValid || beforeValid && afterValid && *before == *after {
		return ""
	}
	old, next, direction := "未提供", "未提供", ""
	if beforeValid {
		old = formatNoticeRate(before) + "x"
	}
	if afterValid {
		next = formatNoticeRate(after) + "x"
	}
	if beforeValid && afterValid {
		direction = "（上调）"
		if *after < *before {
			direction = "（下调）"
		}
	}
	return fmt.Sprintf("分组倍率调整：%s，%s → %s%s", singleLineGroupValue(name), old, next, direction)
}
