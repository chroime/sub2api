package upstreamgovernance

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	ChangeNotificationPending = "pending"
	ChangeNotificationSending = "sending"
	ChangeNotificationSent    = "sent"
)

// ChangeNotification contains only operational metadata and rendered content;
// upstream credentials, API keys, and session tokens must never be placed in it.
type ChangeNotification struct {
	ID              int64      `json:"id"`
	SiteID          int64      `json:"site_id"`
	GroupResource   string     `json:"group_resource,omitempty"`
	LocalGroupID    int64      `json:"local_group_id,omitempty"`
	DedupKey        string     `json:"dedup_key"`
	Recipient       string     `json:"recipient"`
	Kind            string     `json:"kind"`
	Severity        string     `json:"severity"`
	Subject         string     `json:"subject"`
	Body            string     `json:"body"`
	InitialBaseline bool       `json:"initial_baseline"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	NextAttemptAt   time.Time  `json:"next_attempt_at"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ChangeNotice is the rendered, credential-free description of an upstream
// change. It is deliberately separate from ChangeNotification: a notice is
// resolved to one durable delivery row per recipient only after the event has
// been committed by the governance worker.
type ChangeNotice struct {
	SiteID   int64
	SiteName string
	BaseURL  string
	Kind     string
	Severity string
	DedupKey string
	Subject  string
	Body     string
	// GroupChanges are filtered before enqueueing the rendered durable body.
	GroupChanges  []GroupChangeDetail
	GroupResource string
	LocalGroupID  int64
	// AstrBotText is an optional compact rendering for QQ/AstrBot delivery.
	// Email delivery continues to use Subject and Body so operators retain the
	// detailed audit description there.
	AstrBotText     string
	InitialBaseline bool
	ObservedAt      time.Time
}

// ChangeNotifier resolves administrator recipients and sends one rendered
// notice. Implementations must use the application's existing mail transport;
// governance never stores SMTP credentials or recipient settings of its own.
type ChangeNotifier interface {
	Recipients(context.Context, []string) ([]string, error)
	SendChange(context.Context, string, ChangeNotice) error
}

// ChangeNotificationPolicyReader is optional so deployments can roll out the
// policy table independently of the durable delivery queue. A missing policy
// must not prevent the legacy administrator recipient fallback from working.
type ChangeNotificationPolicyReader interface {
	LoadChangeNotificationPolicy(context.Context, int64) (PricingNotificationPolicy, error)
}

func pricingOperationStatusText(status string) string {
	switch status {
	case "applied":
		return "已应用"
	case "protected":
		return "已启用保护"
	case "conflict":
		return "等待人工确认"
	case "failed":
		return "执行失败"
	default:
		return "状态待确认"
	}
}

func pricingOperationReasonText(reason string) string {
	switch reason {
	case "baseline":
		return "建立成本基线"
	case "price_ready":
		return "成本变化已核算"
	case "increase":
		return "成本上升并完成调价"
	case "decrease":
		return "成本下降并完成调价"
	case "increase_review":
		return "成本涨幅超过审核阈值"
	case "unknown_cost":
		return "成本暂时无法确认"
	case "invalid_policy":
		return "调价策略无效"
	case "manual_owner":
		return "存在人工管理"
	case "policy_disabled":
		return "自动调价未启用"
	default:
		return "自动调价结果待确认"
	}
}

func renderPricingOperationNotice(site Site, op PricingOperation) ChangeNotice {
	kind, severity, subject := "pricing_change", "warning", "本地分组售价已自动调整"
	if op.Protected || op.Status == "protected" {
		kind, severity, subject = "protection_change", "critical", "上游成本触发亏损保护"
	} else if op.Status == "conflict" || op.Status == "failed" {
		kind, severity, subject = "pricing_change", "critical", "本地自动调价未完成"
	}
	return ChangeNotice{
		SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL,
		LocalGroupID: op.LocalGroupID,
		Kind:         kind, Severity: severity,
		DedupKey:        fmt.Sprintf("site:%d:pricing:group:%d:revision:%d", site.ID, op.LocalGroupID, op.CostFactRevision),
		Subject:         subject,
		Body:            fmt.Sprintf("上游名称：%s\n站点URL：%s\n本地分组编号：%d\n处理结果：%s\n处理原因：%s\n观察成本：%.8g\n目标售价：%.8g。", site.Name, site.BaseURL, op.LocalGroupID, pricingOperationStatusText(op.Status), pricingOperationReasonText(op.Reason), op.Decision.Cost, op.Decision.TargetSale),
		InitialBaseline: op.Reason == "baseline",
		ObservedAt:      time.Now().UTC(),
	}
}

// EnqueuePricingOperationNotice converts a committed automatic-pricing result
// into a durable administrator notice. The coordinator can call this after
// its atomic pricing commit; no pricing credentials or customer data are
// included. Baseline establishment is intentionally silent.
func (s *Service) EnqueuePricingOperationNotice(ctx context.Context, siteID int64, op PricingOperation) error {
	if s == nil || s.store == nil || siteID <= 0 || (op.Status != "applied" && op.Status != "protected" && op.Status != "conflict" && op.Status != "failed") {
		return nil
	}
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	return s.EnqueueChangeNotice(ctx, renderPricingOperationNotice(*site, op))
}

func changeNotificationPolicyAllows(policy PricingNotificationPolicy, kind string) bool {
	if !policy.Enabled {
		return false
	}
	switch kind {
	case "group_change", "catalog_change":
		return policy.GroupChanges
	case "rate_change":
		return policy.RateChanges
	case "pricing_change":
		return policy.PricingChanges
	case "protection_change":
		return policy.ProtectionChanges
	default:
		return true
	}
}

// ChangeNotificationStore is optional so governance can be used with the
// existing in-memory service fixtures. The SQL implementation is added by the
// notification migration and retains rows across process restarts.
type ChangeNotificationStore interface {
	EnqueueChangeNotification(context.Context, ChangeNotification) error
	DueChangeNotifications(context.Context, time.Time, int) ([]ChangeNotification, error)
	ClaimChangeNotification(context.Context, int64, time.Time, time.Time) (bool, error)
	CompleteChangeNotification(context.Context, int64, time.Time, error, time.Time) error
}

// A visibility read is required for group notices; unavailable reads must not
// authorize delivery. SQL checks current attributes and later private history.
type ChangeNotificationPrivacyReader interface {
	ChangeNotificationIsPublic(context.Context, ChangeNotification) (bool, error)
}

func changeNotificationGroupScoped(item ChangeNotification) bool {
	if item.GroupResource != "" || item.LocalGroupID > 0 || strings.HasPrefix(item.Kind, "group_") {
		return true
	}
	switch item.Kind {
	case "catalog_change", "rate_change", "price_change", "pricing_change", "protection_change", "model_change":
		return true
	}
	return false
}

// Old pricing rows have a structured key. Catalog keys are hashes and cannot
// safely identify a group; do not infer group identity from rendered text.
func locateChangeNotification(item *ChangeNotification) {
	item.GroupResource = strings.TrimSpace(item.GroupResource)
	if item.LocalGroupID != 0 || (item.Kind != "pricing_change" && item.Kind != "protection_change") {
		return
	}
	parts := strings.Split(item.DedupKey, ":")
	if len(parts) != 7 || parts[0] != "site" || parts[2] != "pricing" || parts[3] != "group" || parts[5] != "revision" {
		return
	}
	siteID, siteErr := strconv.ParseInt(parts[1], 10, 64)
	groupID, groupErr := strconv.ParseInt(parts[4], 10, 64)
	revision, revisionErr := strconv.ParseInt(parts[6], 10, 64)
	if siteErr == nil && groupErr == nil && revisionErr == nil && siteID == item.SiteID && groupID > 0 && revision >= 0 {
		item.LocalGroupID = groupID
	}
}

func (q *ChangeNotificationQueue) notificationIsPublic(ctx context.Context, item ChangeNotification) (bool, error) {
	if item.InitialBaseline {
		return false, nil
	}
	if !changeNotificationGroupScoped(item) {
		return true, nil
	}
	if item.GroupResource == "" && item.LocalGroupID <= 0 {
		return false, nil
	}
	reader, ok := q.store.(ChangeNotificationPrivacyReader)
	if !ok {
		return false, errors.New("change notification visibility reader is unavailable")
	}
	return reader.ChangeNotificationIsPublic(ctx, item)
}

type ChangeNotificationSender interface {
	Send(context.Context, ChangeNotification) error
}

type changeNotificationSender struct{ notifier ChangeNotifier }

func (s changeNotificationSender) Send(ctx context.Context, item ChangeNotification) error {
	if s.notifier == nil {
		return errors.New("change notification sender is unavailable")
	}
	return s.notifier.SendChange(ctx, item.Recipient, ChangeNotice{
		SiteID: item.SiteID, Kind: item.Kind, Severity: item.Severity,
		GroupResource: item.GroupResource, LocalGroupID: item.LocalGroupID,
		DedupKey: item.DedupKey, Subject: item.Subject, Body: item.Body,
		InitialBaseline: item.InitialBaseline, ObservedAt: item.CreatedAt,
	})
}

// ChangeNotificationQueue persists before sending. A failed SMTP attempt only
// changes the delivery row and never rolls back the catalog or pricing change.
type ChangeNotificationQueue struct {
	store  ChangeNotificationStore
	sender ChangeNotificationSender
	now    func() time.Time
}

func NewChangeNotificationQueue(store ChangeNotificationStore, sender ChangeNotificationSender, now func() time.Time) *ChangeNotificationQueue {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ChangeNotificationQueue{store: store, sender: sender, now: now}
}

func (q *ChangeNotificationQueue) Enqueue(ctx context.Context, item ChangeNotification) error {
	if q == nil || q.store == nil {
		return errors.New("change notification store is unavailable")
	}
	if item.InitialBaseline {
		return nil
	}
	item.DedupKey = strings.TrimSpace(item.DedupKey)
	item.Recipient = strings.TrimSpace(item.Recipient)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Subject = strings.TrimSpace(item.Subject)
	if item.SiteID <= 0 || item.DedupKey == "" || item.Recipient == "" || item.Kind == "" || item.Subject == "" || item.Body == "" {
		return ErrInvalid
	}
	if item.Status == "" {
		item.Status = ChangeNotificationPending
	}
	if item.Status != ChangeNotificationPending {
		return ErrInvalid
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = q.now().UTC()
	}
	if item.NextAttemptAt.IsZero() {
		item.NextAttemptAt = item.CreatedAt
	}
	locateChangeNotification(&item)
	if public, err := q.notificationIsPublic(ctx, item); err != nil || !public {
		return err
	}
	return q.store.EnqueueChangeNotification(ctx, item)
}

// EnqueueNotice expands one logical change into durable, recipient-scoped
// deliveries. Deduplication remains in the SQL unique constraint so repeated
// observations and concurrent workers cannot create duplicate mail.
func (q *ChangeNotificationQueue) EnqueueNotice(ctx context.Context, notice ChangeNotice, recipients []string) error {
	if q == nil || q.store == nil {
		return errors.New("change notification store is unavailable")
	}
	if notice.InitialBaseline {
		return nil
	}
	if notice.SiteID <= 0 || strings.TrimSpace(notice.DedupKey) == "" || strings.TrimSpace(notice.Kind) == "" || strings.TrimSpace(notice.Subject) == "" || notice.Body == "" {
		return ErrInvalid
	}
	normalized, err := normalizeChangeRecipients(recipients)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}
	created := notice.ObservedAt
	if created.IsZero() {
		created = q.now().UTC()
	}
	var firstErr error
	for _, recipient := range normalized {
		if err := q.Enqueue(ctx, ChangeNotification{
			SiteID: notice.SiteID, DedupKey: notice.DedupKey,
			GroupResource: notice.GroupResource, LocalGroupID: notice.LocalGroupID,
			Recipient: recipient, Kind: notice.Kind, Severity: notice.Severity,
			Subject: notice.Subject, Body: notice.Body,
			InitialBaseline: notice.InitialBaseline, CreatedAt: created,
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// normalizeChangeRecipients accepts normal email addresses plus the internal
// queue references used by global AstrBot notifications. References are
// validated here so arbitrary values cannot reach the outbound transport.
func normalizeChangeRecipients(recipients []string) ([]string, error) {
	if len(recipients) > 100 {
		return nil, ErrInvalid
	}
	result := make([]string, 0, len(recipients))
	seen := make(map[string]struct{}, len(recipients))
	for _, raw := range recipients {
		value := strings.TrimSpace(raw)
		if value == "" || strings.ContainsAny(value, "\r\n") || len(value) > 2048 {
			return nil, ErrInvalid
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		if strings.HasPrefix(value, "qq:") {
			if strings.TrimSpace(strings.TrimPrefix(value, "qq:")) == "" {
				return nil, ErrInvalid
			}
		} else {
			if _, err := normalizeBalanceRecipients([]string{value}); err != nil {
				return nil, err
			}
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func changeNotificationRetry(now time.Time, attempts int) time.Time {
	if attempts < 0 {
		attempts = 0
	}
	delay := 15 * time.Minute
	for i := 0; i < attempts && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}
	return now.Add(delay)
}

func (q *ChangeNotificationQueue) Dispatch(ctx context.Context, limit int) error {
	if q == nil || q.store == nil {
		return errors.New("change notification store is unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	now := q.now().UTC()
	items, err := q.store.DueChangeNotifications(ctx, now, limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, item := range items {
		claimUntil := now.Add(10 * time.Minute)
		claimed, claimErr := q.store.ClaimChangeNotification(ctx, item.ID, now, claimUntil)
		if claimErr != nil {
			if firstErr == nil {
				firstErr = claimErr
			}
			continue
		}
		if !claimed {
			continue
		}
		locateChangeNotification(&item)
		public, sendErr := q.notificationIsPublic(ctx, item)
		if sendErr != nil || !public {
			// A private or unlocatable notice is consumed silently. A failed
			// visibility query stays pending for retry, without sending content.
		} else if q.sender == nil {
			sendErr = errors.New("change notification sender is unavailable")
		} else {
			sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			sendErr = q.sender.Send(sendCtx, item)
			cancel()
		}
		next := now
		if sendErr != nil {
			next = changeNotificationRetry(now, item.Attempts)
		}
		if completeErr := q.store.CompleteChangeNotification(ctx, item.ID, now, sendErr, next); completeErr != nil && firstErr == nil {
			firstErr = completeErr
		}
	}
	return firstErr
}
