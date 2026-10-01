package upstreamgovernance

import (
	"context"
	"errors"
	"fmt"
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
	SiteID          int64
	SiteName        string
	BaseURL         string
	Kind            string
	Severity        string
	DedupKey        string
	Subject         string
	Body            string
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
	kind, severity, subject := "pricing_change", "warning", "本地分组售价已自动调整 / Local group sale updated"
	if op.Protected || op.Status == "protected" {
		kind, severity, subject = "protection_change", "critical", "上游成本触发亏损保护 / Upstream cost protection enabled"
	} else if op.Status == "conflict" || op.Status == "failed" {
		kind, severity, subject = "pricing_change", "critical", "本地自动调价未完成 / Local automatic pricing failed"
	}
	baseline := op.Reason == "baseline"
	return s.EnqueueChangeNotice(ctx, ChangeNotice{
		SiteID: site.ID, SiteName: site.Name, BaseURL: site.BaseURL,
		Kind: kind, Severity: severity,
		DedupKey:        fmt.Sprintf("site:%d:pricing:group:%d:revision:%d", siteID, op.LocalGroupID, op.CostFactRevision),
		Subject:         subject,
		Body:            fmt.Sprintf("本地分组 ID：%d\n状态：%s\n原因：%s\n成本：%.8g\n目标售价：%.8g\n来源：%s\n\nLocal group ID: %d\nStatus: %s\nReason: %s\nCost: %.8g\nTarget sale: %.8g\nSource: %s", op.LocalGroupID, op.Status, op.Reason, op.Decision.Cost, op.Decision.TargetSale, op.Decision.SourceID, op.LocalGroupID, op.Status, op.Reason, op.Decision.Cost, op.Decision.TargetSale, op.Decision.SourceID),
		InitialBaseline: baseline,
		ObservedAt:      time.Now().UTC(),
	})
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
	normalized, err := normalizeBalanceRecipients(recipients)
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
			Recipient: recipient, Kind: notice.Kind, Severity: notice.Severity,
			Subject: notice.Subject, Body: notice.Body,
			InitialBaseline: notice.InitialBaseline, CreatedAt: created,
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
		var sendErr error
		if q.sender == nil {
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
