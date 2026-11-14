package accountquality

import (
	"context"
	"time"
)

// Unbind deletes only the association. Runs and actions intentionally do not
// reference bindings with foreign keys, so their original evidence survives.
func (s *Store) Unbind(ctx context.Context, id, version, actor int64) ([]string, error) {
	if id <= 0 || version <= 0 || actor <= 0 {
		return nil, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Queue/Claim use this lock too. Lock existing executions before policy and
	// binding rows, matching Complete's order and preventing a late queue insert.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(61824001)"); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE binding_id=$1 AND execution_status IN ('queued','running') ORDER BY id FOR UPDATE", id)
	if err != nil {
		return nil, err
	}
	runs := []Run{}
	for rows.Next() {
		run, scanErr := readRun(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var policyID int64
	if err = tx.QueryRowContext(ctx, "SELECT policy_id FROM account_quality_bindings WHERE id=$1", id).Scan(&policyID); err != nil {
		return nil, storeErr(err)
	}
	if err = tx.QueryRowContext(ctx, "SELECT id FROM account_quality_policies WHERE id=$1 FOR UPDATE", policyID).Scan(&policyID); err != nil {
		return nil, storeErr(err)
	}
	binding, err := loadBinding(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if binding.Version != version {
		return nil, ErrConflict
	}
	running := []string{}
	for _, run := range runs {
		if run.ExecutionStatus == "running" {
			// Retain the lease until the request really exits, including on other
			// application nodes. Authorize also rejects the deleted binding.
			run.CancelRequested = true
			run.Reason = "质量策略已解绑，等待在途检测结束"
			running = append(running, run.ID)
		} else {
			now := time.Now().UTC()
			run.ExecutionStatus, run.QualityVerdict = "cancelled", "not_evaluated"
			run.Reason = "质量策略已解绑，取消排队检测"
			run.FinishedAt = &now
		}
		if _, err = tx.ExecContext(ctx, "UPDATE account_quality_runs SET execution_status=$2,evidence=$3::jsonb,finished_at=$4 WHERE id=$1", run.ID, run.ExecutionStatus, encode(run), run.FinishedAt); err != nil {
			return nil, err
		}
	}
	binding.Isolated, binding.OverrideUntil = false, nil
	if err = gateTx(ctx, tx, binding, Policy{ID: policyID}); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM account_quality_bindings WHERE id=$1", id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE account_quality_notifications SET status='cancelled' WHERE binding_id=$1 AND status='pending'", id); err != nil {
		return nil, err
	}
	if err = actionTx(ctx, tx, Action{AccountID: binding.AccountID, BindingID: id, Action: "unbound", Reason: "解绑质量策略：" + binding.PolicyName, ActorID: actor}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.refresh(ctx, []int64{binding.AccountID})
	return running, nil
}

func (s *Service) Unbind(ctx context.Context, id, version, actor int64) error {
	running, err := s.Store.Unbind(ctx, id, version, actor)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, runID := range running {
		if cancel := s.active[runID]; cancel != nil {
			cancel()
		}
	}
	return nil
}
