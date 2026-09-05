package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	UsageBalanceReservationHeld       = "held"
	UsageBalanceReservationSettled    = "settled"
	UsageBalanceReservationReleased   = "released"
	DefaultUsageBalanceReservationTTL = 10 * time.Minute
)

var (
	ErrUsageBalanceReservationRequestIDRequired = errors.New("balance reservation request_id is required")
	ErrUsageBalanceReservationIdentityRequired  = errors.New("balance reservation user_id and api_key_id are required")
	ErrUsageBalanceReservationInvalidAmount     = errors.New("balance reservation hold amount is invalid")
	ErrUsageBalanceReservationExpired           = errors.New("balance reservation has expired")
	ErrUsageBalanceReservationNotFound          = errors.New("balance reservation not found")
	ErrUsageBalanceReservationConflict          = errors.New("balance reservation request conflict")
	ErrUsageBalanceSettlementExceedsHold        = errors.New("balance settlement exceeds reservation hold")
)

// UsageBalanceReservationCommand describes one durable hold on a user's
// available balance. HoldAmount == 0 means "hold all currently available
// balance" and is resolved atomically by the repository.
type UsageBalanceReservationCommand struct {
	RequestID          string
	APIKeyID           int64
	UserID             int64
	RequestFingerprint string
	HoldAmount         float64
	ActualAmount       float64
	ExpiresAt          time.Time
	Status             string
}

func (c *UsageBalanceReservationCommand) Normalize(now time.Time) {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	c.RequestFingerprint = strings.TrimSpace(c.RequestFingerprint)
	c.HoldAmount = QuantizeUsageBillingAmount(c.HoldAmount)
	c.ActualAmount = QuantizeUsageBillingAmount(c.ActualAmount)
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = now.Add(DefaultUsageBalanceReservationTTL)
	}
	if c.Status == "" {
		c.Status = UsageBalanceReservationHeld
	}
	if c.RequestFingerprint == "" {
		c.RequestFingerprint = fmt.Sprintf("%x", []byte(c.RequestID))
		if len(c.RequestFingerprint) > 64 {
			c.RequestFingerprint = c.RequestFingerprint[:64]
		}
	}
}

func (c *UsageBalanceReservationCommand) Validate(now time.Time) error {
	if c == nil || strings.TrimSpace(c.RequestID) == "" {
		return ErrUsageBalanceReservationRequestIDRequired
	}
	if c.UserID <= 0 || c.APIKeyID <= 0 {
		return ErrUsageBalanceReservationIdentityRequired
	}
	if c.HoldAmount < 0 || c.ActualAmount < 0 {
		return ErrUsageBalanceReservationInvalidAmount
	}
	if c.ExpiresAt.IsZero() || !c.ExpiresAt.After(now) {
		return ErrUsageBalanceReservationExpired
	}
	return nil
}

type UsageBalanceReservationResult struct {
	Applied            bool
	Status             string
	RequestID          string
	RequestFingerprint string
	APIKeyID           int64
	UserID             int64
	HoldAmount         float64
	ActualAmount       *float64
	NewBalance         *float64
	FrozenBalance      *float64
	ExpiresAt          time.Time
}

// UsageBalanceReservationRepository is intentionally narrower than
// UsageBillingRepository so existing test doubles and batch-image billing
// implementations remain source-compatible.
type UsageBalanceReservationRepository interface {
	ReserveUsageBalance(context.Context, *UsageBalanceReservationCommand) (*UsageBalanceReservationResult, error)
	SettleUsageBalance(context.Context, *UsageBalanceReservationCommand) (*UsageBalanceReservationResult, error)
	ReleaseUsageBalance(context.Context, *UsageBalanceReservationCommand) (*UsageBalanceReservationResult, error)
	RecoverExpiredUsageBalances(context.Context, int) ([]int64, error)
}
