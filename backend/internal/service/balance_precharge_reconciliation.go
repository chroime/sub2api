package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

var (
	ErrBalancePrechargeReviewNotFound = infraerrors.NotFound("BALANCE_PRECHARGE_REVIEW_NOT_FOUND", "Precharge review not found")
	ErrBalancePrechargeReviewConflict = infraerrors.Conflict("BALANCE_PRECHARGE_REVIEW_CONFLICT", "Precharge was already settled or the resolution conflicts with an earlier decision")
	ErrBalancePrechargeReviewInvalid  = infraerrors.BadRequest("BALANCE_PRECHARGE_REVIEW_INVALID", "Invalid precharge reconciliation request")
)

// Failure evidence contains only accounting identifiers and a bounded reason,
// never request bodies, access tokens, or upstream credentials.
type BalancePrechargeFailureEvidence struct {
	Reason    string
	RequestID string
	AccountID int64
	Model     string
}

type BalancePrechargeReviewEvidence struct {
	PrechargeID string
	UserID      int64
	Reason      string
	RequestID   string
	AccountID   int64
	Model       string
}

type BalancePrechargeReview struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"user_id"`
	UserEmail  string     `json:"user_email"`
	APIKeyID   int64      `json:"api_key_id"`
	GroupID    int64      `json:"group_id"`
	GroupName  string     `json:"group_name"`
	Amount     float64    `json:"amount"`
	Reason     string     `json:"reason"`
	RequestID  string     `json:"request_id"`
	AccountID  int64      `json:"account_id"`
	Model      string     `json:"model"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	ResolvedBy *int64     `json:"resolved_by"`
	Resolution string     `json:"resolution"`
	ActualCost *float64   `json:"actual_cost"`
	Note       string     `json:"note"`
	NewBalance *float64   `json:"new_balance"`
}

type BalancePrechargeResolutionCommand struct {
	PrechargeID string
	ActorID     int64
	Action      string
	ActualCost  float64
	Note        string
}

// This optional interface keeps the normal hold/usage contract compatible.
// Reviews are marked only after the request and its usage workers have ended.
type BalancePrechargeReconciliationRepository interface {
	MarkBalancePrechargeForReview(context.Context, *BalancePrechargeReviewEvidence) error
	ListBalancePrechargeReviews(ctx context.Context, status string, limit, offset int) ([]*BalancePrechargeReview, int64, error)
	ResolveBalancePrechargeReview(context.Context, *BalancePrechargeResolutionCommand) (*BalancePrechargeReview, error)
}

type BalancePrechargeReconciliationService struct {
	repo       BalancePrechargeReconciliationRepository
	invalidate func(context.Context, int64) error
	waiter     *balancePrechargeWaiter
}

func NewBalancePrechargeReconciliationService(repo BalancePrechargeReconciliationRepository, invalidate func(context.Context, int64) error) *BalancePrechargeReconciliationService {
	return &BalancePrechargeReconciliationService{repo: repo, invalidate: invalidate, waiter: defaultBalancePrechargeWaiter}
}

func (s *BalancePrechargeReconciliationService) List(ctx context.Context, status string, limit, offset int) ([]*BalancePrechargeReview, int64, error) {
	if status == "" {
		status = "pending"
	}
	if (status != "pending" && status != "resolved" && status != "settled" && status != "all") || limit < 1 || limit > 200 || offset < 0 {
		return nil, 0, ErrBalancePrechargeReviewInvalid
	}
	if s == nil || s.repo == nil {
		return nil, 0, ErrBillingServiceUnavailable
	}
	items, total, err := s.repo.ListBalancePrechargeReviews(ctx, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []*BalancePrechargeReview{}
	}
	return items, total, nil
}

func (s *BalancePrechargeReconciliationService) Resolve(ctx context.Context, cmd *BalancePrechargeResolutionCommand) (*BalancePrechargeReview, error) {
	if cmd == nil {
		return nil, ErrBalancePrechargeReviewInvalid
	}
	copy := *cmd
	copy.Note = strings.TrimSpace(copy.Note)
	if _, err := uuid.Parse(copy.PrechargeID); err != nil {
		return nil, ErrBalancePrechargeReviewInvalid
	}
	if copy.ActorID <= 0 || copy.Note == "" || utf8.RuneCountInString(copy.Note) > 2000 ||
		(copy.Action != "release" && copy.Action != "charge") ||
		(copy.Action == "release" && copy.ActualCost != 0) || (copy.Action == "charge" && copy.ActualCost <= 0) {
		return nil, ErrBalancePrechargeReviewInvalid
	}
	if err := validateBalancePrechargeMoney(copy.ActualCost, 0, false); err != nil {
		return nil, ErrBalancePrechargeReviewInvalid.WithCause(err)
	}
	if s == nil || s.repo == nil {
		return nil, ErrBillingServiceUnavailable
	}
	result, err := s.repo.ResolveBalancePrechargeReview(ctx, &copy)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("precharge resolution returned no audit record")
	}
	// A committed wallet adjustment is authoritative even if the browser has
	// disconnected or cache invalidation fails. Identical retries remain safe.
	if s.invalidate != nil {
		cacheCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.invalidate(cacheCtx, result.UserID); err != nil {
			slog.Warn("precharge reconciliation cache invalidation failed", "user_id", result.UserID, "error", err)
		}
	}
	s.waiter.notify(result.UserID)
	return result, nil
}
