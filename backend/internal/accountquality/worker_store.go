package accountquality

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

func (s *Store) Due(ctx context.Context) ([]int64, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT b.id FROM account_quality_bindings b JOIN account_quality_policies p ON p.id=b.policy_id JOIN accounts a ON a.id=b.account_id WHERE b.next_run_at<=NOW() AND p.enabled=TRUE AND p.deleted_at IS NULL AND a.deleted_at IS NULL ORDER BY b.next_run_at,b.id LIMIT 100")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (s *Store) Reap(ctx context.Context) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE account_quality_runs SET execution_status='cancelled',finished_at=NOW(),evidence=evidence||jsonb_build_object('execution_status','cancelled','quality_verdict','not_evaluated','reason','执行租约已过期，保留证据且不改变质量状态','finished_at',NOW()) WHERE execution_status='running' AND lease_until<NOW()`)
	return e
}
func (s *Store) Claim(ctx context.Context) (Run, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Run{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(61824001)"); e != nil {
		return Run{}, e
	}
	settings, e := settingsTx(ctx, tx)
	if e != nil {
		return Run{}, e
	}
	var active int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM account_quality_runs WHERE execution_status='running' AND lease_until>NOW()").Scan(&active); e != nil {
		return Run{}, e
	}
	if active >= settings.Concurrency {
		return Run{}, ErrBusy
	}
	r, e := readRun(tx.QueryRowContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE execution_status='queued' ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1"))
	if e != nil {
		return r, e
	}
	now := time.Now().UTC()
	r.StartedAt = &now
	r.ExecutionStatus = "running"
	r.Lease = uuid.NewString()
	_, e = tx.ExecContext(ctx, "UPDATE account_quality_runs SET execution_status='running',evidence=$2::jsonb,lease_token=$3,lease_until=$4 WHERE id=$1", r.ID, encode(r), r.Lease, now.Add(time.Duration(r.Config.TimeoutSeconds+60)*time.Second))
	if e != nil {
		return r, e
	}
	return r, tx.Commit()
}
func (s *Store) Authorize(ctx context.Context, r Run) bool {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	var currentPolicy, currentBinding int64
	var deleted bool
	e := tx.QueryRowContext(ctx, "SELECT p.version,b.version,(p.deleted_at IS NOT NULL OR a.deleted_at IS NOT NULL) FROM account_quality_bindings b JOIN account_quality_policies p ON p.id=b.policy_id JOIN accounts a ON a.id=b.account_id JOIN account_quality_runs r ON r.binding_id=b.id WHERE b.id=$1 AND r.id=$2 AND r.execution_status='running' AND r.lease_token=$3 AND r.lease_until>NOW() AND COALESCE((r.evidence->>'cancel_requested')::boolean,FALSE)=FALSE", r.BindingID, r.ID, r.Lease).Scan(&currentPolicy, &currentBinding, &deleted)
	if e != nil || deleted || currentPolicy != r.PolicyVersion || currentBinding != r.BindingVersion {
		return false
	}
	identity, e := identityTx(ctx, tx, r.AccountID)
	if e != nil || identity != r.IdentityVersion {
		return false
	}
	if s.ResolveModel != nil && r.MappedModel != "" {
		mapped, e := s.ResolveModel(ctx, r.AccountID, r.Model)
		if e != nil || mapped != r.MappedModel {
			return false
		}
	}
	return tx.Commit() == nil
}
func (s *Store) Complete(ctx context.Context, r Run) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	stored, e := readRun(tx.QueryRowContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE id=$1 FOR UPDATE", r.ID))
	if e != nil {
		return e
	}
	if stored.ExecutionStatus != "running" || stored.Lease != r.Lease {
		return nil
	}
	if stored.MappedModel != "" && stored.MappedModel != r.MappedModel {
		r.Stale = true
	}
	if stored.CancelRequested {
		r.CancelRequested = true
		r.ExecutionStatus = "cancelled"
		r.QualityVerdict = "not_evaluated"
		r.Reason = "管理员已取消检测；不改变质量计数或隔离"
	}
	now := time.Now().UTC()
	r.FinishedAt = &now
	p, e := loadPolicy(ctx, tx, r.PolicyID)
	if errors.Is(e, ErrNotFound) {
		r.Stale = true
	} else if e != nil {
		return e
	}
	var b Binding
	if !r.Stale {
		b, e = loadBinding(ctx, tx, r.BindingID)
		if errors.Is(e, ErrNotFound) {
			r.Stale = true
		} else if e != nil {
			return e
		}
	}
	if !r.Stale && (p.Version != r.PolicyVersion || b.Version != r.BindingVersion) {
		r.Stale = true
	}
	if !r.Stale {
		identity, err := identityTx(ctx, tx, r.AccountID)
		if errors.Is(err, ErrNotFound) {
			r.Stale = true
		} else if err != nil {
			return err
		} else if identity != r.IdentityVersion {
			r.Stale = true
		}
	}
	if !r.Stale && s.ResolveModel != nil && r.MappedModel != "" {
		mapped, e := s.ResolveModel(ctx, r.AccountID, r.Model)
		if e != nil || mapped != r.MappedModel {
			r.Stale = true
		}
	}
	if r.Stale {
		r.Reason = "检测期间账号、凭据身份、策略或绑定已变化；结果仅留档，不改变当前质量门"
		r.QualityVerdict = "not_evaluated"
	}
	_, e = tx.ExecContext(ctx, "UPDATE account_quality_runs SET execution_status=$2,evidence=$3::jsonb,finished_at=$4,lease_until=NULL WHERE id=$1", r.ID, r.ExecutionStatus, encode(r), now)
	if e != nil {
		return e
	}
	if !r.Stale && !r.CancelRequested {
		effective := EffectivePolicy(p, b.Overrides)
		wasIsolated := b.Isolated
		var event string
		b, event = Advance(b, effective, r, now)
		b.PolicyVersion = p.Version
		b.PolicyName = p.Name
		b.AutoControlEligible = r.AutoControlEligible
		b.Model = p.Model
		b.IdentityVersion = r.IdentityVersion
		if wasIsolated != b.Isolated {
			b.Version++
		}
		if e = saveBinding(ctx, tx, b); e != nil {
			return e
		}
		if wasIsolated != b.Isolated {
			if e = gateTx(ctx, tx, b, p); e != nil {
				return e
			}
			if e = actionTx(ctx, tx, Action{AccountID: b.AccountID, BindingID: b.ID, Action: event, Reason: b.Reason}); e != nil {
				return e
			}
		}
		settings, e := settingsTx(ctx, tx)
		if e != nil {
			return e
		}
		if e = enqueueNoticeTx(ctx, tx, settings, b, r, event); e != nil {
			return e
		}
		if event == "" && b.Isolated {
			if e = enqueueNoticeTx(ctx, tx, settings, b, r, "quarantined", true); e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if !r.Stale {
		s.refresh(ctx, []int64{r.AccountID})
	}
	return nil
}
func (s *Store) Cancel(ctx context.Context, id string, actor int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := readRun(tx.QueryRowContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return e
	}
	if r.ExecutionStatus != "queued" && r.ExecutionStatus != "running" {
		return ErrConflict
	}
	now := time.Now().UTC()
	if r.ExecutionStatus == "running" {
		r.CancelRequested = true
		r.Reason = "管理员请求取消检测，等待在途请求结束"
	} else {
		r.ExecutionStatus = "cancelled"
		r.QualityVerdict = "not_evaluated"
		r.Reason = "管理员取消检测"
		r.FinishedAt = &now
	}
	if _, e = tx.ExecContext(ctx, "UPDATE account_quality_runs SET execution_status=$3,evidence=$2::jsonb,finished_at=$4 WHERE id=$1", id, encode(r), r.ExecutionStatus, r.FinishedAt); e != nil {
		return e
	}
	if e = actionTx(ctx, tx, Action{AccountID: r.AccountID, BindingID: r.BindingID, Action: "cancelled", Reason: r.Reason, ActorID: actor}); e != nil {
		return e
	}
	return tx.Commit()
}
