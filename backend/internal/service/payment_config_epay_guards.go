package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Provider updates hold the same provider-row lock used by gateway creation.
// An unresolved payment or notification must keep the credentials under which
// it was accepted; completed history also preserves the receiving identity.
func (s *PaymentConfigService) builtinEasyPayHasOrders(ctx context.Context, instanceID int64, unresolvedOnly bool) (bool, error) {
	query := `SELECT EXISTS (SELECT 1 FROM payment_epay_orders WHERE provider_instance_id=$1)`
	args := []any{instanceID}
	if unresolvedOnly {
		query = `SELECT EXISTS (SELECT 1 FROM payment_epay_orders WHERE provider_instance_id=$1 AND ((status='PENDING' AND expires_at>$2) OR (status='PAID' AND notify_status<>'delivered')))`
		args = append(args, time.Now().UTC())
	}
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("check built-in EasyPay history: %w", err)
	}
	defer rows.Close()
	var exists bool
	if rows.Next() {
		if err := rows.Scan(&exists); err != nil {
			return false, fmt.Errorf("read built-in EasyPay history: %w", err)
		}
	}
	return exists, rows.Err()
}

func (s *PaymentConfigService) ensureBuiltinEasyPayCredentialsIdle(ctx context.Context, instanceID int64) error {
	busy, err := s.builtinEasyPayHasOrders(ctx, instanceID, true)
	if err != nil {
		return err
	}
	if busy {
		return infraerrors.Conflict("PENDING_ORDERS", "gateway has pending payments or undelivered notifications; keep its current credentials")
	}
	return nil
}
