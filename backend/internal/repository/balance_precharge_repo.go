package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.BalancePrechargeRepository = (*usageBillingRepository)(nil)

type balancePrechargeRecord struct {
	id                 string
	userID             int64
	apiKeyID           int64
	groupID            int64
	threshold          decimal.Decimal
	amount             decimal.Decimal
	state              string
	duplicateRequestID sql.NullString
}

func balancePrechargeMoney(value float64, positive, exact bool) (decimal.Decimal, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || (positive && value == 0) {
		return decimal.Zero, service.ErrBalancePrechargeInvalid
	}
	amount := decimal.NewFromFloat(value)
	if !amount.LessThan(decimal.New(1, 12)) || (exact && !amount.Equal(amount.Round(service.UsageBillingMonetaryScale))) {
		return decimal.Zero, service.ErrBalancePrechargeInvalid
	}
	return amount, nil
}

func validateBalancePrechargeCommand(cmd *service.BalancePrechargeCommand) (*balancePrechargeRecord, error) {
	if cmd == nil || strings.TrimSpace(cmd.ID) == "" || len(strings.TrimSpace(cmd.ID)) > 128 || cmd.UserID <= 0 || cmd.APIKeyID <= 0 || cmd.GroupID < 0 {
		return nil, service.ErrBalancePrechargeInvalid
	}
	threshold, err := balancePrechargeMoney(cmd.Threshold, true, true)
	if err != nil {
		return nil, err
	}
	amount, err := balancePrechargeMoney(cmd.Amount, true, true)
	if err != nil || amount.GreaterThan(threshold) {
		return nil, service.ErrBalancePrechargeInvalid
	}
	return &balancePrechargeRecord{id: strings.TrimSpace(cmd.ID), userID: cmd.UserID, apiKeyID: cmd.APIKeyID, groupID: cmd.GroupID, threshold: threshold, amount: amount}, nil
}

// ReserveBalancePrecharge serializes all cash admissions for a user, across API
// keys, groups, and service instances. users.balance already excludes all holds.
func (r *usageBillingRepository) ReserveBalancePrecharge(ctx context.Context, cmd *service.BalancePrechargeCommand) (*service.BalancePrechargeResult, error) {
	wanted, err := validateBalancePrechargeCommand(cmd)
	if err != nil {
		return nil, err
	}
	minimumBalance, err := balancePrechargeMoney(cmd.MinimumBalance, false, false)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	balance, frozen, userStatus, err := lockBalancePrechargeWallet(ctx, tx, wanted.userID)
	if err != nil {
		return nil, err
	}
	existing, err := loadBalancePrecharge(ctx, tx, wanted.id)
	if err != nil && !errors.Is(err, service.ErrBalancePrechargeNotFound) {
		return nil, err
	}
	if existing != nil {
		return existing.balancePrechargeAdmission(wanted, balance)
	}
	if userStatus != service.StatusActive {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user account is not active")
	}
	var ownerID int64
	var keyStatus string
	var keyQuota, keyQuotaUsed decimal.Decimal
	var keyExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, status, quota, quota_used, expires_at
		FROM api_keys WHERE id = $1 AND deleted_at IS NULL
	`, wanted.apiKeyID).Scan(&ownerID, &keyStatus, &keyQuota, &keyQuotaUsed, &keyExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerID != wanted.userID {
		return nil, service.ErrBalancePrechargeConflict
	}
	// Queued requests can outlive the authentication snapshot. Revalidate the
	// key on every new admission, while keeping retries of an owned hold above
	// idempotent and available for settlement even after the key is revoked.
	switch keyStatus {
	case service.StatusAPIKeyQuotaExhausted:
		return nil, service.ErrAPIKeyQuotaExhausted
	case service.StatusAPIKeyExpired:
		return nil, service.ErrAPIKeyExpired
	case service.StatusAPIKeyActive:
	default:
		return nil, infraerrors.Forbidden("API_KEY_DISABLED", "api key is disabled")
	}
	if keyExpiresAt.Valid && time.Now().After(keyExpiresAt.Time) {
		return nil, service.ErrAPIKeyExpired
	}
	if keyQuota.IsPositive() && !keyQuotaUsed.LessThan(keyQuota) {
		return nil, service.ErrAPIKeyQuotaExhausted
	}
	wanted.state = "skipped"
	required := minimumBalance
	if balance.LessThan(wanted.threshold) {
		wanted.state = "reserved"
		required = decimal.Max(required, wanted.amount)
	}
	if !balance.IsPositive() || balance.LessThan(required) {
		// Holds are already excluded from balance. Their possible release can
		// distinguish temporary contention from genuine insufficient funds, but
		// must never itself admit a request or be subtracted a second time.
		potentialBalance := balance.Add(frozen)
		if frozen.IsPositive() && potentialBalance.IsPositive() && !potentialBalance.LessThan(required) {
			return nil, service.ErrBalancePrechargeWaiting
		}
		return nil, service.ErrInsufficientBalance
	}
	insert, err := tx.ExecContext(ctx, `
		INSERT INTO balance_precharges (id, user_id, api_key_id, group_id, threshold, amount, state, lease_owner, lease_expires_at, recovery_after)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8,''),
			CASE WHEN $8<>'' THEN NOW()+INTERVAL '2 minutes' END, NOW()+INTERVAL '15 minutes')
		ON CONFLICT (id) DO NOTHING
	`, wanted.id, wanted.userID, wanted.apiKeyID, wanted.groupID, wanted.threshold.StringFixed(8), wanted.amount.StringFixed(8), wanted.state, cmd.LeaseOwner)
	if err != nil {
		return nil, err
	}
	inserted, err := insert.RowsAffected()
	if err != nil {
		return nil, err
	}
	if inserted == 0 {
		// A different user's request may have concurrently used this ID. Never
		// move money before checking the winning durable record's ownership.
		existing, err = loadBalancePrecharge(ctx, tx, wanted.id)
		if err != nil {
			return nil, err
		}
		return existing.balancePrechargeAdmission(wanted, balance)
	}
	if wanted.state == "reserved" {
		err = tx.QueryRowContext(ctx, `
			UPDATE users SET balance = balance - $1,
				frozen_balance = COALESCE(frozen_balance, 0) + $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
			RETURNING balance
		`, wanted.amount.StringFixed(8), wanted.userID).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrInsufficientBalance
		}
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return wanted.balancePrechargeAdmission(wanted, balance)
}

func (record *balancePrechargeRecord) balancePrechargeAdmission(wanted *balancePrechargeRecord, balance decimal.Decimal) (*service.BalancePrechargeResult, error) {
	if record.userID != wanted.userID || record.apiKeyID != wanted.apiKeyID || record.groupID != wanted.groupID || !record.amount.Equal(wanted.amount) || !record.threshold.Equal(wanted.threshold) {
		return nil, service.ErrBalancePrechargeConflict
	}
	result := &service.BalancePrechargeResult{Reserved: record.state == "reserved"}
	result.NewBalance, _ = balance.Float64()
	if result.Reserved {
		result.Amount, _ = record.amount.Float64()
	}
	return result, nil
}

func (r *usageBillingRepository) ReleaseBalancePrecharge(ctx context.Context, id string, userID int64) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 || userID <= 0 {
		return false, service.ErrBalancePrechargeInvalid
	}
	if r == nil || r.db == nil {
		return false, errors.New("usage billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	released, _, err := releaseBalancePrechargeTx(ctx, tx, id, userID, 0, "")
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return released, nil
}

// Every path takes the user lock before the reservation lock. This order both
// shares one wallet across groups and prevents release/capture lock inversion.
func lockBalancePrechargeWallet(ctx context.Context, tx *sql.Tx, userID int64) (decimal.Decimal, decimal.Decimal, string, error) {
	var balance, frozen decimal.Decimal
	var status string
	err := tx.QueryRowContext(ctx, `SELECT balance, COALESCE(frozen_balance, 0), status FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&balance, &frozen, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, decimal.Zero, "", service.ErrUserNotFound
	}
	return balance, frozen, status, err
}

func lockBalancePrechargeUser(ctx context.Context, tx *sql.Tx, userID int64) (decimal.Decimal, error) {
	var balance decimal.Decimal
	err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, service.ErrUserNotFound
	}
	return balance, err
}

func loadBalancePrecharge(ctx context.Context, tx *sql.Tx, id string) (*balancePrechargeRecord, error) {
	record := &balancePrechargeRecord{id: id}
	err := tx.QueryRowContext(ctx, `
		SELECT user_id, api_key_id, group_id, threshold, amount, state, duplicate_request_id
		FROM balance_precharges WHERE id = $1 FOR UPDATE
	`, id).Scan(&record.userID, &record.apiKeyID, &record.groupID, &record.threshold, &record.amount, &record.state, &record.duplicateRequestID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT user_id,api_key_id,group_id,threshold,amount,state,duplicate_request_id
			FROM balance_precharges_archive WHERE id=$1 FOR UPDATE`, id).Scan(&record.userID, &record.apiKeyID, &record.groupID, &record.threshold, &record.amount, &record.state, &record.duplicateRequestID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrBalancePrechargeNotFound
	}
	if err != nil {
		return nil, err
	}
	return record, nil
}

func releaseBalancePrechargeTx(ctx context.Context, tx *sql.Tx, id string, userID, apiKeyID int64, duplicateRequestID string) (bool, *float64, error) {
	if _, err := lockBalancePrechargeUser(ctx, tx, userID); err != nil {
		return false, nil, err
	}
	record, err := loadBalancePrecharge(ctx, tx, id)
	if err != nil {
		return false, nil, err
	}
	if record.userID != userID || (apiKeyID != 0 && record.apiKeyID != apiKeyID) {
		return false, nil, service.ErrBalancePrechargeConflict
	}
	if record.state != "reserved" {
		return false, nil, nil
	}
	var balance float64
	err = tx.QueryRowContext(ctx, `
		UPDATE users SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance
	`, record.amount.StringFixed(8), userID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, errors.New("balance precharge frozen funds are insufficient")
	}
	if err != nil {
		return false, nil, err
	}
	transition, err := tx.ExecContext(ctx, `
		UPDATE balance_precharges SET state = 'released', duplicate_request_id = NULLIF($2, ''), updated_at = NOW()
		WHERE id = $1 AND state = 'reserved'
	`, id, duplicateRequestID)
	if err := requireBalancePrechargeTransition(transition, err); err != nil {
		return false, nil, err
	}
	return true, &balance, nil
}

// captureBalancePrechargeTx participates in the usage dedup/quota transaction.
// A captured reservation can belong to multiple independently billed events
// (for example WebSocket turns); subsequent events deduct their actual cost.
func captureBalancePrechargeTx(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (float64, bool, error) {
	balance, err := lockBalancePrechargeUser(ctx, tx, cmd.UserID)
	if err != nil {
		return 0, false, err
	}
	record, err := loadBalancePrecharge(ctx, tx, cmd.BalancePrechargeID)
	if err != nil {
		return 0, false, err
	}
	if record.userID != cmd.UserID || record.apiKeyID != cmd.APIKeyID {
		return 0, false, service.ErrBalancePrechargeConflict
	}
	// A duplicate usage event can release a redundant hold before a later
	// legitimate event arrives. That event is still payable, but a hold released
	// as a known-unbilled request must continue to reject a racing capture.
	if record.state == "captured" || (record.state == "released" && record.duplicateRequestID.Valid) {
		if cmd.BalanceCost > 0 {
			return deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		}
		current, _ := balance.Float64()
		return current, !balance.IsNegative(), nil
	}
	if record.state != "reserved" {
		return 0, false, service.ErrBalancePrechargeConflict
	}
	var newBalance float64
	err = tx.QueryRowContext(ctx, `
		UPDATE users SET balance = balance + $1 - $2,
			frozen_balance = COALESCE(frozen_balance, 0) - $1, updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance
	`, record.amount.StringFixed(8), decimal.NewFromFloat(cmd.BalanceCost).StringFixed(8), cmd.UserID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, errors.New("balance precharge frozen funds are insufficient")
	}
	if err != nil {
		return 0, false, err
	}
	transition, err := tx.ExecContext(ctx, `
		UPDATE balance_precharges SET state = 'captured', captured_request_id = $2,
			captured_cost = $3, updated_at = NOW()
		WHERE id = $1 AND state = 'reserved'
	`, record.id, cmd.RequestID, decimal.NewFromFloat(cmd.BalanceCost).StringFixed(8))
	if err := requireBalancePrechargeTransition(transition, err); err != nil {
		return 0, false, err
	}
	return newBalance, newBalance >= 0, nil
}

func requireBalancePrechargeTransition(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrBalancePrechargeConflict
	}
	return nil
}
