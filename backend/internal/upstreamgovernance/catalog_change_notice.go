package upstreamgovernance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// catalogChangeNoticeDedupKey hashes only user-visible event details. Full
// and fast observations often differ in models/prices or rate metadata even
// when the visible group change is the same.
func catalogChangeNoticeDedupKey(siteID int64, event Event) string {
	details := catalogEventDetails(event, Catalog{})
	return catalogDetailsDedupKey(siteID, event.Resource, details)
}

func catalogDetailsDedupKey(siteID int64, resource string, details []GroupChangeDetail) string {
	if len(details) == 0 {
		return ""
	}
	parts := append([]GroupChangeDetail(nil), details...)
	// DiffCatalog already uses a stable kind/resource ordering. For one group,
	// sort explicitly so full and fast collectors have the same signature.
	for i := 0; i < len(parts); i++ {
		for j := i + 1; j < len(parts); j++ {
			if parts[j].Kind < parts[i].Kind || parts[j].Kind == parts[i].Kind && parts[j].Text < parts[i].Text {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
	var signature strings.Builder
	fmt.Fprintf(&signature, "%d\x00%s", siteID, resource)
	for _, part := range parts {
		fmt.Fprintf(&signature, "\x00%s\x00%s", part.Kind, part.Text)
	}
	digest := sha256.Sum256([]byte(signature.String()))
	return fmt.Sprintf("site:%d:group-change:%s", siteID, hex.EncodeToString(digest[:]))
}

type catalogRateNoticeValue struct {
	Name     string   `json:"name"`
	Resolved *float64 `json:"resolved"`
}

func catalogGroupName(catalog Catalog, id string) string {
	for _, group := range catalog.Groups {
		if group.ID == id && group.Name != "" {
			return singleLineGroupValue(group.Name)
		}
	}
	return singleLineGroupValue(id)
}

func formatNoticeRate(value *float64) string {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return "未知"
	}
	rounded := math.Round(*value*1_000_000) / 1_000_000
	if rounded == 0 {
		rounded = 0
	}
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

func formatNoticePercent(before, delta float64) string {
	if before <= 0 || math.IsNaN(before) || math.IsInf(before, 0) || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return "无法计算"
	}
	percent := math.Round(delta/before*100*100) / 100
	return strconv.FormatFloat(percent, 'f', -1, 64) + "%"
}

func catalogGroupFromJSON(raw string) RemoteGroup {
	var group RemoteGroup
	_ = json.Unmarshal([]byte(raw), &group)
	return group
}

func catalogGroupDescriptionChange(name, before, after string) GroupChangeDetail {
	text := fmt.Sprintf("分组描述调整：%s，原描述「%s」更改为「%s」", singleLineGroupValue(name), groupNoticeDescription(before), groupNoticeDescription(after))
	if strings.TrimSpace(after) == "" {
		text = fmt.Sprintf("分组描述调整：%s，原描述「%s」，描述已清空", singleLineGroupValue(name), groupNoticeDescription(before))
	}
	return GroupChangeDetail{Kind: GroupEventDescriptionChange, GroupName: singleLineGroupValue(name), Text: text}
}

func groupRate(group RemoteGroup) *float64 {
	if group.ResolvedRateMultiplier != nil {
		return group.ResolvedRateMultiplier
	}
	if group.RateMultiplier != nil {
		return group.RateMultiplier
	}
	return group.UserRateMultiplier
}

// catalogEventDetails maps audit diffs to the five user-facing group events.
// Protocol, model, pricing and peak-only changes remain in audit history but
// are deliberately not described as one of these events.
func catalogEventDetails(event Event, catalog Catalog) []GroupChangeDetail {
	name := catalogGroupName(catalog, event.Resource)
	switch event.Kind {
	case "group_added":
		group := catalogGroupFromJSON(event.After)
		if group.Name != "" {
			name = singleLineGroupValue(group.Name)
		}
		detail := GroupChangeDetail{Kind: GroupEventOnline, GroupName: name, Text: fmt.Sprintf("分组上线：%s 上线，描述：%s", name, groupNoticeDescription(group.Description))}
		if rate := groupRate(group); rate != nil && !math.IsNaN(*rate) && !math.IsInf(*rate, 0) {
			detail.Text += "，倍率" + formatNoticeRate(rate) + "x"
		}
		return []GroupChangeDetail{detail}
	case "group_removed":
		group := catalogGroupFromJSON(event.Before)
		if group.Name != "" {
			name = singleLineGroupValue(group.Name)
		}
		return []GroupChangeDetail{{Kind: GroupEventOffline, GroupName: name, Text: fmt.Sprintf("分组下线：%s 下线", name)}}
	case "group_changed":
		var before, after []string
		if json.Unmarshal([]byte(event.Before), &before) != nil || json.Unmarshal([]byte(event.After), &after) != nil || len(before) < 3 || len(after) < 3 {
			return nil
		}
		details := make([]GroupChangeDetail, 0, 2)
		oldName, newName := singleLineGroupValue(before[0]), singleLineGroupValue(after[0])
		if oldName == "" {
			oldName = singleLineGroupValue(event.Resource)
		}
		if newName == "" {
			newName = singleLineGroupValue(event.Resource)
		}
		if before[0] != after[0] {
			details = append(details, GroupChangeDetail{Kind: GroupEventNameChange, GroupName: newName, Text: fmt.Sprintf("分组名称调整：%s 更改为 %s", oldName, newName)})
		}
		if before[1] != after[1] {
			details = append(details, catalogGroupDescriptionChange(newName, before[1], after[1]))
		}
		return details
	case "rate_changed":
		var before, after catalogRateNoticeValue
		if json.Unmarshal([]byte(event.Before), &before) != nil || json.Unmarshal([]byte(event.After), &after) != nil {
			return nil
		}
		if after.Name != "" {
			name = singleLineGroupValue(after.Name)
		} else if before.Name != "" {
			name = singleLineGroupValue(before.Name)
		}
		if detail := renderGroupRateChange(name, before.Resolved, after.Resolved); detail != "" {
			return []GroupChangeDetail{{Kind: GroupEventRateChange, GroupName: name, Text: detail}}
		}
	}
	return nil
}

func renderCatalogChangeNotice(site Site, event Event, catalog Catalog) ChangeNotice {
	return renderCatalogGroupNotice(site, event.Resource, catalogEventDetails(event, catalog), event.CreatedAt)
}

func renderCatalogGroupNotice(site Site, resource string, details []GroupChangeDetail, observedAt time.Time) ChangeNotice {
	if len(details) == 0 {
		return ChangeNotice{SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL, ObservedAt: observedAt}
	}
	name := ""
	for _, detail := range details {
		if detail.Kind == GroupEventNameChange && detail.GroupName != "" {
			name = singleLineGroupValue(detail.GroupName)
			break
		}
	}
	if name == "" {
		for _, detail := range details {
			if groupName := singleLineGroupValue(detail.GroupName); groupName != "" {
				name = groupName
				break
			}
		}
	}
	if name == "" {
		name = singleLineGroupValue(resource)
	}
	subjectPrefix := "上游分组变化"
	if len(details) == 1 {
		switch details[0].Kind {
		case GroupEventOnline:
			subjectPrefix = "上游分组上线"
		case GroupEventOffline:
			subjectPrefix = "上游分组下线"
		case GroupEventNameChange:
			subjectPrefix = "上游分组名称调整"
		case GroupEventDescriptionChange:
			subjectPrefix = "上游分组描述调整"
		case GroupEventRateChange:
			subjectPrefix = "上游倍率变更"
		}
	}
	return ChangeNotice{
		SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL,
		Kind: "group_changes", Severity: "info", GroupChanges: details, GroupResource: resource,
		DedupKey:    catalogDetailsDedupKey(site.ID, resource, details),
		Subject:     fmt.Sprintf("%s：%s - %s", subjectPrefix, singleLineGroupValue(site.Name), singleLineGroupValue(name)),
		Body:        renderGroupChangeMessage(details, observedAt, "上游名称："+singleLineGroupValue(site.Name), "站点URL："+singleLineGroupValue(site.BaseURL)),
		AstrBotText: renderGroupChangeMessage(details, observedAt), ObservedAt: observedAt,
	}
}

func renderCatalogChangeNotices(site Site, events []Event, catalog Catalog, observedAt time.Time, completeBaseline bool) []ChangeNotice {
	if !completeBaseline || !catalog.GroupsComplete {
		return nil
	}
	byResource := make(map[string][]GroupChangeDetail)
	resources := make([]string, 0)
	for _, event := range events {
		if event.Kind != "group_added" && event.Kind != "group_removed" && event.Kind != "group_changed" && event.Kind != "rate_changed" {
			continue
		}
		details := catalogEventDetails(event, catalog)
		if len(details) == 0 {
			continue
		}
		if _, exists := byResource[event.Resource]; !exists {
			resources = append(resources, event.Resource)
		}
		byResource[event.Resource] = append(byResource[event.Resource], details...)
	}
	notices := make([]ChangeNotice, 0, len(resources))
	for _, resource := range resources {
		notices = append(notices, renderCatalogGroupNotice(site, resource, byResource[resource], observedAt))
	}
	return notices
}
