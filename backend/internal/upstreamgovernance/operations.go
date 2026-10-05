package upstreamgovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

type OperationsQuery struct {
	SiteID         int64
	Page, PageSize int
	Kind           string
}

type WorkbenchItem struct {
	ID            string     `json:"id"`
	SiteID        int64      `json:"site_id"`
	SiteName      string     `json:"site_name"`
	BaseURL       string     `json:"base_url"`
	Kind          string     `json:"kind"`
	Severity      string     `json:"severity"`
	Status        string     `json:"status"`
	Reason        string     `json:"reason"`
	ResourceID    string     `json:"resource_id"`
	ResourceName  string     `json:"resource_name"`
	ImpactCount   int        `json:"impact_count"`
	ObservedAt    *time.Time `json:"observed_at"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	Shared        bool       `json:"shared"`
	TargetTab     string     `json:"target_tab"`
}

type WorkbenchSummary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}
type WorkbenchResult struct {
	Items       []WorkbenchItem  `json:"items"`
	Total       int64            `json:"total"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
	EvaluatedAt time.Time        `json:"evaluated_at"`
	Summary     WorkbenchSummary `json:"summary"`
}

type TimelineItem struct {
	ID              string     `json:"id"`
	RecordID        string     `json:"record_id"`
	Kind            string     `json:"kind"`
	SiteID          int64      `json:"site_id"`
	SiteName        string     `json:"site_name"`
	BaseURL         string     `json:"base_url"`
	ResourceID      string     `json:"resource_id"`
	ResourceName    string     `json:"resource_name"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason"`
	CreatedAt       time.Time  `json:"created_at"`
	Shared          bool       `json:"shared"`
	Acknowledged    *bool      `json:"acknowledged"`
	BeforeRate      *float64   `json:"before_rate"`
	AfterRate       *float64   `json:"after_rate"`
	BeforeCost      *float64   `json:"before_cost"`
	AfterCost       *float64   `json:"after_cost"`
	Attempts        *int       `json:"attempts"`
	NextAttemptAt   *time.Time `json:"next_attempt_at"`
	SentAt          *time.Time `json:"sent_at"`
	RelatedRecordID *string    `json:"related_record_id"`
}
type TimelineResult struct {
	Items       []TimelineItem `json:"items"`
	Total       int64          `json:"total"`
	Page        int            `json:"page"`
	PageSize    int            `json:"page_size"`
	EvaluatedAt time.Time      `json:"evaluated_at"`
}

type workbenchSite struct {
	ID                                          int64
	Name, BaseURL                               string
	Enabled, HasCredential                      bool
	Status                                      string
	Version, FullSeconds                        int64
	LastAttemptAt, SnapshotAt                   *time.Time
	SnapshotVersion                             int64
	SnapshotUserID                              int64
	FastEnabled                                 bool
	FastSeconds                                 int64
	FastStatus                                  string
	FastAt, NextFastAt                          *time.Time
	FastSourceUserID                            int64
	FastComplete                                bool
	FastRevision                                int64
	BalanceEnabled                              bool
	BalanceState                                string
	KeyIssues                                   int
	KeyPending, KeyUnknown, KeyProtectionFailed int
}
type pricingWorkbenchRow struct {
	GroupID            int64
	GroupName, Reason  string
	Enabled, Protected bool
	SiteIDs            []int64
	UpdatedAt          time.Time
}
type notificationWorkbenchRow struct {
	SiteID        int64
	Count         int
	Status        string
	NextAttemptAt *time.Time
}
type timelineRecord struct {
	ID, Kind, ResourceID, ResourceName, Status, Reason, Severity, Before, After string
	CreatedAt                                                                   time.Time
	Shared, Acknowledged, HasError                                              bool
	BeforeRate, AfterRate, BeforeCost, AfterCost                                *float64
	Attempts                                                                    int
	NextAttemptAt, SentAt                                                       *time.Time
}

func operationsText(value string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	if len(runes) > 300 {
		runes = runes[:300]
	}
	return string(runes)
}
func operationsURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.ForceQuery = false
	return u.String()
}
func operationsFreshness(at *time.Time, seconds, floor int64, now time.Time) string {
	if at == nil || at.IsZero() || at.After(now.Add(time.Minute)) {
		return "unknown"
	}
	if seconds <= 0 || seconds > maxIntervalSeconds {
		return "unknown"
	}
	if !now.Before(addSeconds(*at, max(2*seconds, floor))) {
		return "stale"
	}
	return ""
}
func collectionReason(site workbenchSite, now time.Time) string {
	if site.Status == "error" {
		return "collection_failed"
	}
	if site.SnapshotAt != nil && site.SnapshotVersion != site.Version {
		return "snapshot_outdated"
	}
	switch operationsFreshness(site.SnapshotAt, site.FullSeconds, 600, now) {
	case "unknown":
		return "collection_unknown"
	case "stale":
		return "collection_stale"
	}
	return ""
}

func projectWorkbench(now time.Time, sites []workbenchSite, pricing []pricingWorkbenchRow, notifications []notificationWorkbenchRow, query OperationsQuery) WorkbenchResult {
	result := WorkbenchResult{Items: []WorkbenchItem{}, Page: query.Page, PageSize: query.PageSize, EvaluatedAt: now}
	items := []WorkbenchItem{}
	byID := map[int64]workbenchSite{}
	add := func(site workbenchSite, kind, severity, status, reason, target string, count int, at, next *time.Time) {
		items = append(items, WorkbenchItem{ID: fmt.Sprintf("%s:%d", kind, site.ID), SiteID: site.ID, SiteName: operationsText(site.Name), BaseURL: operationsURL(site.BaseURL), Kind: kind, Severity: severity, Status: status, Reason: reason, ResourceID: fmt.Sprint(site.ID), ResourceName: operationsText(site.Name), ImpactCount: count, ObservedAt: at, NextAttemptAt: next, TargetTab: target})
	}
	for _, site := range sites {
		byID[site.ID] = site
		if query.SiteID > 0 && query.SiteID != site.ID {
			continue
		}
		if !site.HasCredential || site.Status == "reauth_required" {
			reason, severity := "authorization_missing", "critical"
			if site.Status == "reauth_required" {
				reason = "reauth_required"
			}
			if !site.Enabled {
				severity = "info"
			}
			add(site, "authorization", severity, "action_required", reason, "connect", 1, nil, nil)
		}
		if site.KeyIssues > 0 {
			add(site, "key", "critical", "action_required", "key_issue", "keys", site.KeyIssues, nil, nil)
		}
		for _, pending := range []struct {
			Count                    int
			Reason, Severity, Status string
		}{{site.KeyPending, "key_verification_pending", "warning", "unknown"}, {site.KeyUnknown, "key_verification_unknown", "info", "unknown"}, {site.KeyProtectionFailed, "key_protection_failed", "warning", "error"}} {
			if pending.Count > 0 {
				add(site, "key", pending.Severity, pending.Status, pending.Reason, "keys", pending.Count, nil, nil)
				items[len(items)-1].ID += "/" + pending.Reason
			}
		}
		if !site.Enabled {
			continue
		}
		collection := collectionReason(site, now)
		if site.HasCredential && site.Status != "reauth_required" && collection != "" {
			status, severity := "unknown", "info"
			if collection == "collection_failed" {
				status, severity = "error", "warning"
			} else if collection == "collection_stale" || collection == "snapshot_outdated" {
				status, severity = "stale", "warning"
			}
			add(site, "collection", severity, status, collection, "monitor", 1, site.SnapshotAt, nil)
		}
		if site.HasCredential && site.Status != "reauth_required" && site.FastEnabled {
			reason, status, severity := "", "unknown", "info"
			if site.FastStatus == "error" {
				reason, status, severity = "observation_failed", "error", "warning"
			} else if site.FastStatus == "rate_limited" {
				reason, status, severity = "observation_rate_limited", "rate_limited", "warning"
			} else {
				switch operationsFreshness(site.FastAt, site.FastSeconds, 60, now) {
				case "unknown":
					reason = "observation_unknown"
				case "stale":
					reason, status, severity = "observation_stale", "stale", "warning"
				}
				// The fast row has no configuration version of its own. Confirm
				// identity only against a current-version full observation that
				// predates this complete fast fact. Reauthorization does not clear
				// older fast timestamps, so freshness alone is not proof.
				if reason == "" && (site.SnapshotAt == nil || site.SnapshotVersion != site.Version || site.SnapshotUserID <= 0 || site.FastSourceUserID != site.SnapshotUserID || !site.FastComplete || site.FastRevision <= 0 || site.FastAt.Before(*site.SnapshotAt)) {
					reason = "observation_unknown"
				}
			}
			if reason != "" {
				add(site, "fast_observation", severity, status, reason, "monitor", 1, site.FastAt, site.NextFastAt)
			}
		}
		if site.BalanceEnabled {
			reason, status, severity := "", "unknown", "info"
			if collection == "collection_stale" {
				reason, status, severity = "balance_stale", "stale", "warning"
			} else if collection != "" || !site.HasCredential || site.Status == "reauth_required" {
				reason = "balance_unknown"
			} else if site.BalanceState == "low" {
				reason, status, severity = "balance_low", "low", "warning"
			} else if site.BalanceState != "healthy" {
				reason = "balance_unknown"
			}
			if reason != "" {
				add(site, "balance", severity, status, reason, "monitor", 1, site.SnapshotAt, nil)
			}
		}
	}
	for _, p := range pricing {
		if !p.Enabled || !p.Protected {
			continue
		}
		ids := append([]int64{}, p.SiteIDs...)
		sortInt64s(ids)
		var site workbenchSite
		found := false
		for _, id := range ids {
			if query.SiteID > 0 && id != query.SiteID {
				continue
			}
			if candidate, ok := byID[id]; ok {
				site, found = candidate, true
				break
			}
		}
		if !found {
			continue
		}
		reason := pricingOperationReason(p.Reason, "pricing_protected")
		add(site, "pricing", "critical", "protected", reason, "monitor", len(ids), &p.UpdatedAt, nil)
		item := &items[len(items)-1]
		item.ID = fmt.Sprintf("pricing:%d", p.GroupID)
		item.ResourceID = fmt.Sprint(p.GroupID)
		item.ResourceName = operationsText(p.GroupName)
		item.Shared = len(ids) > 1
	}
	for _, n := range notifications {
		site, ok := byID[n.SiteID]
		if !ok || query.SiteID > 0 && query.SiteID != site.ID || n.Count <= 0 {
			continue
		}
		status := "pending"
		if n.Status == "sending" {
			status = "sending"
		}
		add(site, "notification", "warning", status, "notification_retry", "monitor", n.Count, nil, n.NextAttemptAt)
	}
	sort.Slice(items, func(i, j int) bool {
		rank := map[string]int{"critical": 0, "warning": 1, "info": 2}
		if rank[items[i].Severity] != rank[items[j].Severity] {
			return rank[items[i].Severity] < rank[items[j].Severity]
		}
		if items[i].SiteID != items[j].SiteID {
			return items[i].SiteID < items[j].SiteID
		}
		return items[i].ID < items[j].ID
	})
	for _, item := range items {
		switch item.Severity {
		case "critical":
			result.Summary.Critical++
		case "warning":
			result.Summary.Warning++
		default:
			result.Summary.Info++
		}
	}
	result.Total = int64(len(items))
	start := (query.Page - 1) * query.PageSize
	if start >= 0 && start < len(items) {
		result.Items = items[start:min(start+query.PageSize, len(items))]
	}
	return result
}

func pricingOperationReason(reason, fallback string) string {
	switch reason {
	case "price_ready", "baseline", "increase", "decrease", "unchanged", "decrease_stability", "manual_owner", "unknown_cost", "invalid_policy", "increase_review", "policy_disabled":
		return reason
	}
	return fallback
}
func finiteAmount(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return nil
	}
	return value
}
func eventRate(raw string) (string, *float64) {
	var value struct {
		Name     string   `json:"Name"`
		Resolved *float64 `json:"Resolved"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return "", nil
	}
	return operationsText(value.Name), finiteAmount(value.Resolved)
}
func projectTimeline(site workbenchSite, record timelineRecord) TimelineItem {
	item := TimelineItem{ID: record.Kind + ":" + record.ID, RecordID: record.ID, Kind: record.Kind, SiteID: site.ID, SiteName: operationsText(site.Name), BaseURL: operationsURL(site.BaseURL), ResourceID: operationsText(record.ResourceID), ResourceName: operationsText(record.ResourceName), CreatedAt: record.CreatedAt, Shared: record.Shared, Severity: "info", Status: "unknown"}
	switch record.Kind {
	case "event":
		item.Status = "recorded"
		item.Acknowledged = &record.Acknowledged
		if record.Acknowledged {
			item.Status = "acknowledged"
		}
		item.Reason = "event_recorded"
		switch record.Reason {
		case "rate_changed", "group_added", "group_removed", "group_changed", "models_changed", "price_changed", "channels_changed", "balance_low", "balance_recovered", "reconciliation_applied", "import_applied", "auto_reauthorization_required", "sync_failed", "probe_failed", "probe_recovered", "key_missing_suspected", "key_missing_confirmed", "key_recovered", "key_account_paused", "key_account_restored", "key_repair_abandoned":
			item.Reason = record.Reason
		}
		switch item.Reason {
		case "rate_changed", "price_changed", "group_removed", "balance_low", "sync_failed", "probe_failed", "key_missing_suspected":
			item.Severity = "warning"
		case "auto_reauthorization_required", "key_missing_confirmed", "key_account_paused":
			item.Severity = "critical"
		}
		if item.Reason == "rate_changed" {
			beforeName, before := eventRate(record.Before)
			afterName, after := eventRate(record.After)
			item.BeforeRate, item.AfterRate = before, after
			if afterName != "" {
				item.ResourceName = afterName
			} else if beforeName != "" {
				item.ResourceName = beforeName
			}
		}
		if item.Reason == "group_added" || item.Reason == "group_removed" {
			beforeName, _ := eventRate(record.Before)
			afterName, _ := eventRate(record.After)
			if afterName != "" {
				item.ResourceName = afterName
			} else if beforeName != "" {
				item.ResourceName = beforeName
			}
		}
		if item.Reason == "group_changed" {
			var names []string
			if json.Unmarshal([]byte(record.After), &names) == nil && len(names) == 2 {
				item.ResourceName = operationsText(names[0])
			}
		}
	case "pricing":
		switch record.Status {
		case "prepared", "applied", "protected", "rejected", "conflict", "failed":
			item.Status = record.Status
		}
		item.Reason = pricingOperationReason(record.Reason, "pricing_recorded")
		if item.Status == "protected" || item.Status == "failed" || item.Status == "conflict" {
			item.Severity = "critical"
		}
		item.BeforeRate, item.AfterRate = finiteAmount(record.BeforeRate), finiteAmount(record.AfterRate)
		item.BeforeCost, item.AfterCost = finiteAmount(record.BeforeCost), finiteAmount(record.AfterCost)
	case "notification":
		switch record.Status {
		case "pending", "sending", "sent":
			item.Status = record.Status
		}
		item.Attempts = &record.Attempts
		item.NextAttemptAt = record.NextAttemptAt
		item.SentAt = record.SentAt
		item.Reason = "notification_pending"
		if record.Status == "sent" {
			item.Reason = "notification_accepted"
			item.NextAttemptAt = nil
		} else if record.HasError {
			item.Reason = "notification_retry"
			item.Severity = "warning"
		} else if record.Status == "sending" {
			item.Reason = "notification_sending"
		}
	}
	return item
}

type OperationsStore interface {
	ReadWorkbench(context.Context, OperationsQuery, time.Time) (WorkbenchResult, error)
	ReadTimeline(context.Context, OperationsQuery, time.Time) (TimelineResult, error)
}

func validateOperationsQuery(q OperationsQuery, timeline bool) error {
	if q.SiteID < 0 || timeline && q.SiteID == 0 || q.Page < 1 || q.Page > 100000 || q.PageSize < 1 || q.PageSize > 100 {
		return ErrInvalid
	}
	if timeline && q.Kind != "" && q.Kind != "all" && q.Kind != "event" && q.Kind != "pricing" && q.Kind != "notification" {
		return ErrInvalid
	}
	return nil
}
func (s *Service) Workbench(ctx context.Context, q OperationsQuery) (WorkbenchResult, error) {
	if err := validateOperationsQuery(q, false); err != nil {
		return WorkbenchResult{}, err
	}
	store, ok := s.store.(OperationsStore)
	if !ok {
		return WorkbenchResult{}, ErrUnsupported
	}
	return store.ReadWorkbench(ctx, q, s.now().UTC())
}
func (s *Service) Timeline(ctx context.Context, q OperationsQuery) (TimelineResult, error) {
	if err := validateOperationsQuery(q, true); err != nil {
		return TimelineResult{}, err
	}
	store, ok := s.store.(OperationsStore)
	if !ok {
		return TimelineResult{}, ErrUnsupported
	}
	return store.ReadTimeline(ctx, q, s.now().UTC())
}
