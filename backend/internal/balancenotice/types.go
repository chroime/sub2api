// Package balancenotice records platform-user low-balance email outcomes.
// It observes delivery; it never schedules, retries or suppresses messages.
package balancenotice

import (
	"context"
	"errors"
	"strings"
	"time"
)

const Retention = 90 * 24 * time.Hour

var (
	ErrInvalid  = errors.New("invalid balance notification query")
	ErrNotFound = errors.New("balance notification record not found")
)

type Record struct {
	ID                int64      `json:"id"`
	NotificationID    string     `json:"notification_id"`
	TriggerType       string     `json:"trigger_type"`
	BatchID           string     `json:"batch_id"`
	UserID            int64      `json:"user_id"`
	UserEmail         string     `json:"user_email"`
	UserName          string     `json:"user_name"`
	Recipient         string     `json:"recipient"`
	Balance           *float64   `json:"balance"`
	Threshold         *float64   `json:"threshold"`
	Subject           string     `json:"subject"`
	Body              string     `json:"body,omitempty"`
	Status            string     `json:"status"`
	SuppressionReason string     `json:"suppression_reason"`
	Attempts          int        `json:"attempts"`
	LastAttemptAt     *time.Time `json:"last_attempt_at"`
	SentAt            *time.Time `json:"sent_at"`
	LastError         string     `json:"last_error"`
	DeliveryKey       string     `json:"delivery_key,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
}

type Input struct {
	TriggerType, BatchID, UserEmail, UserName, Recipient, DeliveryKey string
	UserID                                                            int64
	Balance, Threshold                                                *float64
}

type Query struct {
	Page, PageSize                            int
	UserID                                    int64
	UserEmail, Recipient, Status, TriggerType string
	From, To                                  time.Time
}

func (q Query) Normalize(now time.Time) (Query, error) {
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if q.Page < 1 || q.Page > 100000 || q.PageSize < 1 || q.PageSize > 100 || q.UserID < 0 {
		return q, ErrInvalid
	}
	q.Status = strings.TrimSpace(q.Status)
	q.TriggerType = strings.TrimSpace(q.TriggerType)
	if q.Status != "" && q.Status != "pending" && q.Status != "sending" && q.Status != "sent" && q.Status != "failed" && q.Status != "suppressed" {
		return q, ErrInvalid
	}
	if q.TriggerType != "" && q.TriggerType != "automatic" && q.TriggerType != "manual_batch" {
		return q, ErrInvalid
	}
	q.UserEmail = strings.TrimSpace(q.UserEmail)
	q.Recipient = strings.TrimSpace(q.Recipient)
	if len(q.UserEmail) > 320 || len(q.Recipient) > 320 {
		return q, ErrInvalid
	}
	if q.From.IsZero() {
		q.From = now.Add(-Retention)
	}
	if q.To.IsZero() {
		// PostgreSQL timestamps are microsecond-precision; a nanosecond bound
		// could round back to now and exclude a newly created record.
		q.To = now.Add(time.Microsecond)
	}
	if !q.From.Before(q.To) {
		return q, ErrInvalid
	}
	return q, nil
}

type Summary struct {
	Total      int64 `json:"total"`
	Pending    int64 `json:"pending"`
	Sent       int64 `json:"sent"`
	Failed     int64 `json:"failed"`
	Suppressed int64 `json:"suppressed"`
}

type Page struct {
	Items       []Record  `json:"items"`
	Total       int64     `json:"total"`
	Page        int       `json:"page"`
	PageSize    int       `json:"page_size"`
	Pages       int       `json:"pages"`
	Summary     Summary   `json:"summary"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

type Store interface {
	Save(context.Context, *Record) error
	List(context.Context, Query, time.Time) (*Page, error)
	Get(context.Context, int64, time.Time) (*Record, error)
	Prune(context.Context, time.Time, int) (int64, error)
}
