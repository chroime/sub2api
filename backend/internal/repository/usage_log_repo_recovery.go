package repository

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Financial columns in usage_logs are NUMERIC(20,10); compare using that same
// database rounding. Match the persisted row, not an in-memory queue result.
const usageLogOutboxMatchesPersistedSQL = `
	u.request_id = o.request_id AND u.api_key_id = o.api_key_id
	AND u.user_id = (o.payload #>> '{usage,UserID}')::bigint
	AND u.account_id = (o.payload #>> '{usage,AccountID}')::bigint
	AND u.actual_cost = (o.payload #>> '{usage,ActualCost}')::numeric(20,10)
	AND u.total_cost = (o.payload #>> '{usage,TotalCost}')::numeric(20,10)
`

// AcknowledgeUsageLogOutbox keeps healthy-path recovery work short-lived. If a
// write only reached a queue, a mismatched failure placeholder was inserted, or
// acknowledgement fails, the durable item remains available for recovery.
func (r *usageLogRepository) AcknowledgeUsageLogOutbox(ctx context.Context, log *service.UsageLog) error {
	if r == nil || r.db == nil || log == nil || strings.TrimSpace(log.RequestID) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM usage_log_outbox o USING usage_logs u
		WHERE o.request_id = $1 AND o.api_key_id = $2 AND `+usageLogOutboxMatchesPersistedSQL,
		strings.TrimSpace(log.RequestID), log.APIKeyID)
	return err
}

// RecoverUsageLog inserts missing usage records and handles an ambiguous-commit
// corner case: the gateway may have received a billing error after COMMIT and
// written its existing ActualCost=0 failure placeholder. Only committed outbox
// evidence can restore that amount. It cannot overwrite a nonzero bill, a row
// owned by another user/account, or a different total-cost calculation.
func (r *usageLogRepository) RecoverUsageLog(ctx context.Context, log *service.UsageLog) (bool, error) {
	inserted, err := r.Create(ctx, log)
	if err != nil || inserted || r.db == nil || log == nil {
		return inserted, err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE usage_logs u
		SET actual_cost = (o.payload #>> '{usage,ActualCost}')::numeric(20,10)
		FROM usage_log_outbox o
		WHERE u.request_id=$1 AND u.api_key_id=$2
			AND u.user_id=$3 AND u.account_id=$4 AND u.actual_cost=0
			AND o.request_id=u.request_id AND o.api_key_id=u.api_key_id
			AND (o.payload #>> '{usage,UserID}')::bigint=u.user_id
			AND (o.payload #>> '{usage,AccountID}')::bigint=u.account_id
			AND (o.payload #>> '{usage,ActualCost}')::numeric(20,10) > 0
			AND (o.payload #>> '{usage,ActualCost}')::numeric(20,10) = $5::numeric(20,10)
			AND (o.payload #>> '{usage,TotalCost}')::numeric(20,10) = u.total_cost
	`, strings.TrimSpace(log.RequestID), log.APIKeyID, log.UserID, log.AccountID, log.ActualCost)
	return usageLogOutboxAffected(res, err)
}
