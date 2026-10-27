package balancenotice

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/google/uuid"
)

type Service struct {
	store     Store
	now       func() time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	lifecycle sync.Mutex
}

func NewService(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

type Delivery struct {
	service *Service
	record  Record
	mu      sync.Mutex
}

func numeric(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	copy := *v
	return &copy
}

func (s *Service) Begin(in Input) *Delivery {
	if s == nil || s.store == nil || in.UserID <= 0 {
		return nil
	}
	if in.TriggerType != "manual_batch" {
		in.TriggerType = "automatic"
	}
	now := s.now()
	d := &Delivery{service: s, record: Record{NotificationID: uuid.NewString(), TriggerType: in.TriggerType, BatchID: in.BatchID, UserID: in.UserID, UserEmail: in.UserEmail, UserName: in.UserName, Recipient: strings.TrimSpace(in.Recipient), Balance: numeric(in.Balance), Threshold: numeric(in.Threshold), DeliveryKey: in.DeliveryKey, Status: "pending", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(Retention)}}
	d.persist()
	return d
}

func (d *Delivery) persist() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.service.store.Save(ctx, &d.record); err != nil {
		slog.Warn("balance notification record write failed", "user_id", d.record.UserID, "notification_id", d.record.NotificationID, "error", logredact.RedactText(err.Error()))
	}
}

func (d *Delivery) update(fn func(*Record)) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.record.Status == "sent" {
		return
	}
	fn(&d.record)
	d.record.UpdatedAt = d.service.now()
	d.persist()
}

func (d *Delivery) Content(subject, body string) {
	d.update(func(r *Record) {
		r.Subject = subject
		r.Body = body
		r.Status = "pending"
		r.LastError = ""
		r.SuppressionReason = ""
	})
}
func (d *Delivery) Attempt() {
	d.update(func(r *Record) { now := d.service.now(); r.Status = "sending"; r.Attempts++; r.LastAttemptAt = &now })
}
func (d *Delivery) Sent() {
	d.update(func(r *Record) {
		now := d.service.now()
		r.Status = "sent"
		r.SentAt = &now
		r.LastError = ""
		r.SuppressionReason = ""
	})
}
func (d *Delivery) Fail(err error) {
	if err == nil {
		return
	}
	d.update(func(r *Record) {
		if r.Status == "failed" && r.LastError != "" {
			return
		}
		r.Status = "failed"
		r.LastError = logredact.RedactText(err.Error())
		if len(r.LastError) > 4096 {
			r.LastError = strings.ToValidUTF8(r.LastError[:4096], "")
		}
	})
}
func (d *Delivery) Suppress(reason string) {
	d.update(func(r *Record) {
		if r.Status == "suppressed" && r.SuppressionReason != "" {
			return
		}
		r.Status = "suppressed"
		r.SuppressionReason = reason
	})
}
func (d *Delivery) Amounts(balance, threshold *float64) {
	d.update(func(r *Record) { r.Balance = numeric(balance); r.Threshold = numeric(threshold) })
}

func (s *Service) List(ctx context.Context, q Query) (*Page, error) {
	now := s.now()
	q, err := q.Normalize(now)
	if err != nil {
		return nil, err
	}
	return s.store.List(ctx, q, now)
}
func (s *Service) Get(ctx context.Context, id int64) (*Record, error) {
	if id <= 0 {
		return nil, ErrInvalid
	}
	return s.store.Get(ctx, id, s.now())
}
