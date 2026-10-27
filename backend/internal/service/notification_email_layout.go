package service

import (
	"fmt"
	"html"
	"strings"
)

func groupUpdateEmailHeaders(locale string) []string {
	if normalizeNotificationLocale(locale) == notificationEmailLocaleChinese {
		return []string{"分组", "描述", "当前倍率"}
	}
	return []string{"Group", "Description", "Current multiplier"}
}

func groupUpdateEmailRow(headers []string, values []string) string {
	var row strings.Builder
	row.WriteString("<tr>")
	for index, value := range values {
		label := html.EscapeString(headers[index])
		fmt.Fprintf(&row, `<td data-label="%s" style="padding:12px;border-bottom:1px solid #e2e8f0"><span class="group-cell-label" style="display:none;mso-hide:all;">%s</span><span class="group-cell-value">%s</span></td>`, label, label, html.EscapeString(value))
	}
	row.WriteString("</tr>")
	return row.String()
}

func groupUpdateEmailTable(headers []string, rows string) string {
	table := `<table class="group-summary-table" style="width:100%;border-collapse:collapse;table-layout:fixed;text-align:left"><colgroup><col style="width:28%"><col style="width:52%"><col style="width:20%"></colgroup><thead><tr>`
	for _, header := range headers {
		table += `<th scope="col" style="padding:12px;background:#f1f5f9;word-break:keep-all">` + html.EscapeString(header) + "</th>"
	}
	return table + "</tr></thead><tbody>" + rows + "</tbody></table>"
}

func groupUpdateEmailSampleTable(locale string) string {
	headers := groupUpdateEmailHeaders(locale)
	description := "Default models"
	if normalizeNotificationLocale(locale) == notificationEmailLocaleChinese {
		description = "默认模型"
	}
	return groupUpdateEmailTable(headers, groupUpdateEmailRow(headers, []string{"GPT Pro", description, "0.06x"}))
}

// The group catalogue needs room for descriptions and rates. Apply its layout
// at render time so previously saved versions of this template benefit too.
// Other email events retain their existing compact layout.
func groupUpdateEmailLayout(body string) string {
	if strings.Contains(body, `id="sub2api-group-email-layout"`) {
		return body
	}
	const style = `<style id="sub2api-group-email-layout">
body { margin: 0; padding: 24px; box-sizing: border-box; }
.container { width: 100% !important; max-width: 960px !important; box-sizing: border-box; }
.header, .content, .footer { box-sizing: border-box; }
.content { padding: 28px 32px !important; overflow-wrap: anywhere; }
.content table { width: 100% !important; table-layout: fixed; border-collapse: collapse; }
.content th, .content td { box-sizing: border-box; vertical-align: top; text-align: left; padding: 12px; line-height: 1.6; }
.content th { white-space: normal; word-break: keep-all; }
.content td:nth-child(1), .content td:nth-child(2) { overflow-wrap: anywhere; }
.content td:nth-child(3) { white-space: nowrap; font-variant-numeric: tabular-nums; }
.group-cell-value { min-width: 0; overflow-wrap: anywhere; }
@media only screen and (max-width: 800px) {
  body { padding: 12px; }
  .container { max-width: 100% !important; }
  .header, .content, .footer { padding-left: 20px !important; padding-right: 20px !important; }
  .group-summary-table, .group-summary-table tbody { display: block; }
  .group-summary-table colgroup, .group-summary-table thead { display: none; }
  .group-summary-table tr { display: block; padding: 10px 0; border-bottom: 1px solid #e2e8f0; }
  .group-summary-table td { display: block; width: 100% !important; padding: 6px 0 !important; border: 0 !important; white-space: normal !important; overflow-wrap: anywhere; overflow: hidden; }
  .group-summary-table .group-cell-label { display: block !important; float: left; width: 76px; color: #64748b; font-size: 13px; font-weight: 600; }
  .group-summary-table .group-cell-value { display: block; margin-left: 88px; }
}
</style>`
	if index := strings.Index(strings.ToLower(body), "</head>"); index >= 0 {
		return body[:index] + style + body[index:]
	}
	return style + body
}
