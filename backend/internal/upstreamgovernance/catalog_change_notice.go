package upstreamgovernance

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

type catalogRateNoticeValue struct {
	Name     string   `json:"Name"`
	Resolved *float64 `json:"Resolved"`
}

func catalogGroupName(catalog Catalog, id string) string {
	for _, group := range catalog.Groups {
		if group.ID == id && group.Name != "" {
			return group.Name
		}
	}
	return id
}

func formatNoticeRate(value *float64) string {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return "未知"
	}
	// Rates are decimal values, but subtraction can expose binary floating
	// point noise (for example 0.030000000000000027). Keep six meaningful
	// decimal places while preserving ordinary values such as 0.93.
	rounded := math.Round(*value*1_000_000) / 1_000_000
	if rounded == 0 {
		rounded = 0
	}
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

func formatNoticePercent(before, delta float64) string {
	// A zero or invalid baseline has no meaningful percentage denominator.
	// Keep the absolute change in the notice, but never expose +/-Inf or NaN.
	if before <= 0 || math.IsNaN(before) || math.IsInf(before, 0) || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return "无法计算"
	}
	percent := math.Round(delta/before*100*100) / 100
	return strconv.FormatFloat(percent, 'f', -1, 64) + "%"
}

func renderCatalogChangeNotice(site Site, event Event, catalog Catalog) ChangeNotice {
	groupName := catalogGroupName(catalog, event.Resource)
	notice := ChangeNotice{
		SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL,
		Kind: "catalog_change", Severity: "info",
		DedupKey:   fmt.Sprintf("site:%d:event:%s:%s", site.ID, event.Kind, event.Resource),
		Subject:    fmt.Sprintf("上游变更：%s - %s", site.Name, groupName),
		ObservedAt: event.CreatedAt,
	}
	switch event.Kind {
	case "rate_changed":
		var before, after catalogRateNoticeValue
		if json.Unmarshal([]byte(event.Before), &before) == nil && json.Unmarshal([]byte(event.After), &after) == nil && before.Resolved != nil && after.Resolved != nil {
			if after.Name != "" {
				groupName = after.Name
			} else if before.Name != "" {
				groupName = before.Name
			}
			delta := *after.Resolved - *before.Resolved
			verb := "倍率未变化"
			change := ""
			if delta > 0 {
				verb = "倍率上调"
				change = fmt.Sprintf("，上调%s，上调幅度为%s", formatNoticeRate(&delta), formatNoticePercent(*before.Resolved, delta))
			} else if delta < 0 {
				down := -delta
				verb = "倍率下调"
				change = fmt.Sprintf("，下调%s，下调幅度为%s", formatNoticeRate(&down), formatNoticePercent(*before.Resolved, down))
			}
			notice.Kind, notice.Severity = "rate_change", "warning"
			notice.Subject = fmt.Sprintf("上游倍率变更：%s - %s", site.Name, groupName)
			notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n倍率：%s -> %s，%s%s。", site.Name, site.BaseURL, groupName, formatNoticeRate(before.Resolved), formatNoticeRate(after.Resolved), verb, change)
			return notice
		}
		notice.Kind, notice.Severity = "rate_change", "warning"
		notice.Subject = fmt.Sprintf("上游倍率变更：%s - %s", site.Name, groupName)
		notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n倍率发生变化，但前后数值暂时无法完整核对。", site.Name, site.BaseURL, groupName)
	case "group_added":
		notice.Kind = "group_change"
		notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n上游新增了此可见分组。", site.Name, site.BaseURL, groupName)
	case "group_removed":
		notice.Kind = "group_change"
		notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n上游不再提供此可见分组。", site.Name, site.BaseURL, groupName)
	case "group_changed":
		notice.Kind = "group_change"
		notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n上游分组名称或协议发生变化。", site.Name, site.BaseURL, groupName)
	case "price_changed":
		notice.Kind, notice.Severity = "rate_change", "warning"
		notice.Subject = fmt.Sprintf("上游价格变更：%s - %s", site.Name, groupName)
		notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n该分组的模型或计价规则发生变化，请检查成本口径。", site.Name, site.BaseURL, groupName)
	default:
		notice.Body = fmt.Sprintf("上游名称：%s\n站点URL：%s\n分组名称：%s\n该分组的上游目录发生变化。", site.Name, site.BaseURL, groupName)
	}
	return notice
}
