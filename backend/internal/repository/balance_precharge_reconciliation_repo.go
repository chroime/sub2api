package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.BalancePrechargeReconciliationRepository = (*usageBillingRepository)(nil)

func (r *usageBillingRepository) MarkBalancePrechargeForReview(ctx context.Context, evidence *service.BalancePrechargeReviewEvidence) error {
	if evidence == nil {
		return service.ErrBalancePrechargeReviewInvalid
	}
	id, reason := strings.TrimSpace(evidence.PrechargeID), strings.TrimSpace(evidence.Reason)
	requestID, model := strings.TrimSpace(evidence.RequestID), strings.TrimSpace(evidence.Model)
	if id == "" || len(id) > 128 || evidence.UserID <= 0 || evidence.AccountID < 0 || reason == "" || utf8.RuneCountInString(reason) > 512 || utf8.RuneCountInString(requestID) > 255 || utf8.RuneCountInString(model) > 255 {
		return service.ErrBalancePrechargeReviewInvalid
	}
	if r == nil || r.db == nil {
		return errors.New("usage billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := lockBalancePrechargeUser(ctx, tx, evidence.UserID); err != nil {
		return err
	}
	hold, err := loadBalancePrecharge(ctx, tx, id)
	if err != nil {
		return err
	}
	if hold.userID != evidence.UserID {
		return service.ErrBalancePrechargeConflict
	}
	if hold.state != "reserved" {
		return nil // A successful automatic settlement already owns the outcome.
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO balance_precharge_reviews (precharge_id, reason, request_id, account_id, model)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT (precharge_id) DO NOTHING
	`, id, reason, requestID, evidence.AccountID, model)
	if err != nil {
		return err
	}
	return tx.Commit()
}

const balancePrechargeReviewStatus = `CASE
	WHEN review.resolved_at IS NOT NULL THEN 'resolved'
	WHEN hold.state = 'reserved' THEN 'pending'
	ELSE 'settled' END`

const balancePrechargeReviewColumns = `
	review.precharge_id, hold.user_id, COALESCE(owner.email, ''), hold.api_key_id,
	hold.group_id, COALESCE(grp.name, ''), hold.amount, review.reason,
	review.request_id, review.account_id, review.model, ` + balancePrechargeReviewStatus + `,
	review.created_at, review.updated_at, review.resolved_at, review.resolved_by,
	COALESCE(review.resolution, ''), review.actual_cost, COALESCE(review.note, ''), review.new_balance`

const balancePrechargeReviewJoins = `
	FROM balance_precharge_reviews_history review
	JOIN balance_precharges_history hold ON hold.id = review.precharge_id
	LEFT JOIN users owner ON owner.id = hold.user_id
	LEFT JOIN groups grp ON grp.id = hold.group_id`

type balancePrechargeReviewScanner interface{ Scan(...any) error }

func scanBalancePrechargeReview(row balancePrechargeReviewScanner) (*service.BalancePrechargeReview, error) {
	review := new(service.BalancePrechargeReview)
	var resolvedAt sql.NullTime
	var resolvedBy sql.NullInt64
	var actualCost, newBalance sql.NullFloat64
	err := row.Scan(&review.ID, &review.UserID, &review.UserEmail, &review.APIKeyID,
		&review.GroupID, &review.GroupName, &review.Amount, &review.Reason,
		&review.RequestID, &review.AccountID, &review.Model, &review.Status,
		&review.CreatedAt, &review.UpdatedAt, &resolvedAt, &resolvedBy,
		&review.Resolution, &actualCost, &review.Note, &newBalance)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		review.ResolvedAt = &resolvedAt.Time
	}
	if resolvedBy.Valid {
		review.ResolvedBy = &resolvedBy.Int64
	}
	if actualCost.Valid {
		review.ActualCost = &actualCost.Float64
	}
	if newBalance.Valid {
		review.NewBalance = &newBalance.Float64
	}
	return review, nil
}

func (r *usageBillingRepository) ListBalancePrechargeReviews(ctx context.Context, status string, limit, offset int) ([]*service.BalancePrechargeReview, int64, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "pending"
	}
	if (status != "pending" && status != "resolved" && status != "settled" && status != "all") || limit <= 0 || limit > 200 || offset < 0 {
		return nil, 0, service.ErrBalancePrechargeReviewInvalid
	}
	if r == nil || r.db == nil {
		return nil, 0, errors.New("usage billing repository db is nil")
	}
	// The count and page share one snapshot, including races with settlement.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	filter := ` WHERE ($1 = 'all' OR (` + balancePrechargeReviewStatus + `) = $1)`
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) `+balancePrechargeReviewJoins+filter, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+balancePrechargeReviewColumns+balancePrechargeReviewJoins+filter+`
		ORDER BY review.created_at DESC, review.precharge_id LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	reviews := make([]*service.BalancePrechargeReview, 0)
	for rows.Next() {
		review, err := scanBalancePrechargeReview(rows)
		if err != nil {
			return nil, 0, err
		}
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return reviews, total, nil
}

func (r *usageBillingRepository) ResolveBalancePrechargeReview(ctx context.Context, cmd *service.BalancePrechargeResolutionCommand) (*service.BalancePrechargeReview, error) {
	if cmd == nil {
		return nil, service.ErrBalancePrechargeReviewInvalid
	}
	id, note := strings.TrimSpace(cmd.PrechargeID), strings.TrimSpace(cmd.Note)
	cost, moneyErr := balancePrechargeMoney(cmd.ActualCost, false, true)
	if id == "" || len(id) > 128 || cmd.ActorID <= 0 || note == "" || utf8.RuneCountInString(note) > 2000 || moneyErr != nil || cost.GreaterThan(decimal.NewFromInt(1000000)) || (cmd.Action != "release" && cmd.Action != "charge") || (cmd.Action == "charge" && !cost.IsPositive()) || (cmd.Action == "release" && !cost.IsZero()) {
		return nil, service.ErrBalancePrechargeReviewInvalid
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Discover immutable ownership without locking the ledger first. All money
	// operations then acquire user -> hold -> review, just like automatic billing.
	var userID int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM balance_precharges_history WHERE id=$1`, id).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrBalancePrechargeReviewNotFound
		}
		return nil, err
	}
	if _, err := lockBalancePrechargeUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	hold, err := loadBalancePrecharge(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var reviewID string
	err = tx.QueryRowContext(ctx, `SELECT precharge_id FROM balance_precharge_reviews WHERE precharge_id=$1 FOR UPDATE`, id).Scan(&reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT precharge_id FROM balance_precharge_reviews_archive WHERE precharge_id=$1 FOR UPDATE`, id).Scan(&reviewID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrBalancePrechargeReviewNotFound
	}
	if err != nil {
		return nil, err
	}
	review, err := scanBalancePrechargeReview(tx.QueryRowContext(ctx,
		`SELECT `+balancePrechargeReviewColumns+balancePrechargeReviewJoins+` WHERE review.precharge_id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrBalancePrechargeReviewNotFound
	}
	if err != nil {
		return nil, err
	}
	if review.ResolvedAt != nil {
		// A retry is idempotent only for the entire audited decision, including
		// actor and note. Never let a later call silently rewrite attribution.
		if review.ResolvedBy == nil || *review.ResolvedBy != cmd.ActorID || review.Resolution != cmd.Action || review.ActualCost == nil || !decimal.NewFromFloat(*review.ActualCost).Equal(cost) || review.Note != note {
			return nil, service.ErrBalancePrechargeReviewConflict
		}
		return review, nil
	}
	if hold.state != "reserved" {
		return nil, service.ErrBalancePrechargeReviewConflict
	}
	var activeOwner bool
	if err := tx.QueryRowContext(ctx, `SELECT lease_owner IS NOT NULL AND lease_expires_at>NOW()
		FROM balance_precharges WHERE id=$1`, id).Scan(&activeOwner); err != nil {
		return nil, err
	}
	if activeOwner {
		return nil, service.ErrBalancePrechargeReviewConflict
	}
	var newBalance decimal.Decimal
	err = tx.QueryRowContext(ctx, `
		UPDATE users SET balance = balance + $1 - $2,
			frozen_balance = COALESCE(frozen_balance, 0) - $1, updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance
	`, hold.amount.StringFixed(8), cost.StringFixed(8), userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("balance precharge frozen funds are insufficient")
	}
	if err != nil {
		return nil, err
	}
	// Manual decisions intentionally use released, with no duplicate usage ID.
	// This terminal state rejects late automatic captures instead of double
	// charging. Confirmed costs belong solely to the review audit, not fake usage.
	transition, err := tx.ExecContext(ctx, `
		UPDATE balance_precharges SET state='released', duplicate_request_id=NULL, updated_at=NOW()
		WHERE id=$1 AND state='reserved'
	`, id)
	if err := requireBalancePrechargeTransition(transition, err); err != nil {
		return nil, err
	}
	transition, err = tx.ExecContext(ctx, `
		UPDATE balance_precharge_reviews
		SET resolved_at=NOW(), resolved_by=$2, resolution=$3, actual_cost=$4,
			note=$5, new_balance=$6, updated_at=NOW()
		WHERE precharge_id=$1 AND resolved_at IS NULL
	`, id, cmd.ActorID, cmd.Action, cost.StringFixed(8), note, newBalance.StringFixed(8))
	if err := requireBalancePrechargeTransition(transition, err); err != nil {
		return nil, err
	}
	review, err = scanBalancePrechargeReview(tx.QueryRowContext(ctx,
		`SELECT `+balancePrechargeReviewColumns+balancePrechargeReviewJoins+` WHERE review.precharge_id=$1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return review, nil
}
