package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type modelActiveRun struct {
	batchID string
	cancel  context.CancelFunc
}

// SetModelNotifier is wired before Start, like the existing balance notifier.
func (s *Service) SetModelNotifier(n ModelNotifier) { s.modelNotifier = n }
func (s *Service) wakeModels() {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.modelWake != nil {
		select {
		case s.modelWake <- struct{}{}:
		default:
		}
	}
}
func (s *Service) cancelActiveModels(batchID string) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	for _, r := range s.modelActive {
		if r.batchID == batchID {
			r.cancel()
		}
	}
}
func (s *Service) startModelWorker() {
	if (!s.durableKey || s.cipher == nil) && s.localModelRunner == nil {
		return
	}
	m, err := s.models()
	if err != nil {
		return
	}
	if _, ok := s.connector.(ModelRunner); !ok && s.localModelRunner == nil {
		return
	}
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.modelDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.modelDone = done
	s.modelCancel = cancel
	s.modelWake = make(chan struct{}, 1)
	s.modelActive = map[string]modelActiveRun{}
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); s.modelScheduleLoop(ctx, m) }()
		go func() { defer workers.Done(); s.modelNoticeLoop(ctx, m) }()
		s.modelDispatchLoop(ctx, m, uuid.NewString())
		workers.Wait()
	}()
}
func (s *Service) stopModelWorker() {
	s.modelMu.Lock()
	done := s.modelDone
	if done == nil {
		s.modelMu.Unlock()
		return
	}
	s.modelCancel()
	s.modelMu.Unlock()
	<-done
	s.modelMu.Lock()
	if s.modelDone == done {
		s.modelDone = nil
		s.modelCancel = nil
		s.modelWake = nil
		s.modelActive = nil
	}
	s.modelMu.Unlock()
}
func (s *Service) modelDispatchLoop(ctx context.Context, m *modelStore, owner string) {
	var running sync.WaitGroup
	defer running.Wait()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil {
			s.modelMu.Lock()
			count := len(s.modelActive)
			s.modelMu.Unlock()
			if count >= 32 {
				break
			}
			claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			claim, err := m.claim(claimCtx, owner, s.now())
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[UpstreamGovernance] model claim: %s", ErrorCode(err))
				}
				break
			}
			if claim == nil {
				break
			}
			runCtx, runCancel := context.WithCancel(ctx)
			s.modelMu.Lock()
			s.modelActive[claim.Run.ID] = modelActiveRun{batchID: claim.Run.BatchID, cancel: runCancel}
			s.modelMu.Unlock()
			running.Add(1)
			go func(c modelClaim) {
				defer running.Done()
				defer runCancel()
				s.executeModelRun(runCtx, ctx, m, owner, c)
				s.modelMu.Lock()
				delete(s.modelActive, c.Run.ID)
				s.modelMu.Unlock()
				s.wakeModels()
			}(*claim)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.modelWake:
		}
	}
}

func (s *Service) executeModelRun(ctx, parent context.Context, m *modelStore, owner string, claim modelClaim) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(claim.Run.Request.Config.TimeoutSeconds)*time.Second+10*time.Second)
	defer cancel()
	result := ModelRunResult{}
	status := "skipped"
	defer func() {
		// Persist cancellation/partial evidence even when the request context was
		// cancelled. A failed write leaves a lease that becomes indeterminate.
		writeCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := m.finish(writeCtx, claim.Run.ID, owner, status, result, s.now()); err != nil && !errors.Is(err, ErrConflict) {
			log.Printf("[UpstreamGovernance] model result: %s", ErrorCode(err))
		}
	}()
	allowed, err := m.renew(ctx, claim.Run.ID, owner, s.now())
	if err != nil || !allowed {
		result.ErrorCode = "cancelled_before_request"
		status = "cancelled"
		return
	}
	target, err := s.verifyModelExecution(ctx, claim)
	if err != nil {
		result.ErrorCode = modelExecutionError(err)
		if claim.Run.PolicyID != nil && errors.Is(err, ErrModelGroupGone) {
			s.pauseModelPolicy(ctx, m, claim.Run.SiteID, *claim.Run.PolicyID, claim.PolicyVersion, "upstream_group_missing")
		}
		return
	}
	// Cross-process cancellation and policy disabling are observed while the
	// request is streaming; this lease is independent from site collection.
	heartbeatDone := make(chan struct{})
	var changedIdentity atomic.Bool
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				ok, e := m.renew(checkCtx, claim.Run.ID, owner, s.now())
				stop()
				if e != nil || !ok {
					cancel()
					return
				}
				if _, e = s.verifyModelExecution(ctx, claim); e != nil {
					if ctx.Err() == nil {
						changedIdentity.Store(true)
					}
					cancel()
					return
				}
			}
		}
	}()
	if target.local != nil {
		if s.localModelRunner == nil {
			result.ErrorCode = "local_runner_unavailable"
			err = ErrUnsupported
		}
		if s.localModelRunner != nil {
			result, err = s.localModelRunner.RunLocalModel(ctx, *target.local, claim.Run.Request)
		}
	} else {
		result, err = s.connector.(ModelRunner).RunModel(ctx, target.site, target.key, claim.Run.Request)
	}
	if result.Success && err == nil {
		status = "succeeded"
	} else {
		status = "failed"
		if result.HTTPStatus == 0 && (errors.Is(err, ErrUnsupported) || errors.Is(err, ErrInvalid)) {
			status = "skipped"
		}
		if result.ErrorCode == "effort_unsupported" {
			status = "skipped"
		}
		if result.ErrorCode == "" {
			result.ErrorCode = modelExecutionError(err)
		}
	}
	if ctx.Err() != nil {
		result.Success = false
		if parent.Err() != nil {
			status = "indeterminate"
			result.ErrorCode = "worker_interrupted"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			status = "cancelled"
			result.ErrorCode = "cancelled"
		} else {
			status = "failed"
			result.ErrorCode = "timeout"
		}
	}
	if ctx.Err() == nil {
		if _, verifyErr := s.verifyModelExecution(ctx, claim); verifyErr != nil {
			if ctx.Err() == nil {
				changedIdentity.Store(true)
			}
		}
	}
	cancel()
	<-heartbeatDone
	if changedIdentity.Load() {
		result.Success = false
		status = "indeterminate"
		result.ErrorCode = "target_changed"
	}
}
func modelExecutionError(err error) string {
	switch {
	case errors.Is(err, ErrModelGroupGone):
		return "upstream_group_missing"
	case errors.Is(err, ErrModelBudget):
		return "daily_request_budget"
	case errors.Is(err, ErrConflict):
		return "target_changed"
	case errors.Is(err, ErrNotFound):
		return "target_missing"
	case errors.Is(err, ErrEncryption), errors.Is(err, ErrReauth):
		return "reauth_required"
	default:
		return ErrorCode(err)
	}
}
func (s *Service) pauseModelPolicy(ctx context.Context, m *modelStore, siteID, id, version int64, code string) {
	_, _ = m.db.ExecContext(ctx, `UPDATE upstream_governance_model_policies SET enabled=FALSE,version=version+1,last_error=$3,updated_at=$4 WHERE site_id=$1 AND id=$2 AND enabled AND version=$5`, siteID, id, code, s.now(), version)
}
func (s *Service) modelScheduleLoop(ctx context.Context, m *modelStore) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		workCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		if err := s.scheduleModels(workCtx, m); err != nil && ctx.Err() == nil {
			log.Printf("[UpstreamGovernance] model schedule: %s", ErrorCode(err))
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) scheduleModels(ctx context.Context, m *modelStore) error {
	rows, err := m.db.QueryContext(ctx, `SELECT `+modelPolicyColumns+` FROM upstream_governance_model_policies WHERE enabled AND next_run_at<=$1 AND EXISTS(SELECT 1 FROM upstream_governance_sites s WHERE s.id=site_id AND s.enabled) ORDER BY next_run_at,id LIMIT 30`, s.now())
	if err != nil {
		return err
	}
	policies := []ModelPolicy{}
	for rows.Next() {
		p, e := scanModelPolicy(rows)
		if e != nil {
			rows.Close()
			return e
		}
		policies = append(policies, *p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range policies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		requestID := fmt.Sprintf("schedule:%d:%d:%s", p.ID, p.Version, p.NextRunAt.UTC().Format(time.RFC3339Nano))
		input, e := s.modelEnqueueInput(ctx, p.SiteID, ModelBatchInput{RequestID: requestID, PolicyID: p.ID}, true)
		if e == nil {
			_, e = m.enqueue(ctx, input)
		}
		if e == nil {
			s.wakeModels()
			continue
		}
		if errors.Is(e, ErrBusy) {
			continue
		}
		if errors.Is(e, ErrModelGroupGone) {
			s.pauseModelPolicy(ctx, m, p.SiteID, p.ID, p.Version, "upstream_group_missing")
			continue
		}
		if errors.Is(e, errModelIdentityChanged) {
			s.pauseModelPolicy(ctx, m, p.SiteID, p.ID, p.Version, "target_changed")
			continue
		}
		next := addMinutes(s.now(), int64(p.IntervalMinutes))
		if errors.Is(e, ErrConflict) {
			next = s.now().Add(time.Minute)
		}
		if errors.Is(e, ErrModelBudget) {
			now := s.now().UTC()
			next = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		}
		if _, err = m.db.ExecContext(ctx, `UPDATE upstream_governance_model_policies SET next_run_at=$3,last_error=$4,updated_at=$5 WHERE site_id=$1 AND id=$2 AND version=$6`, p.SiteID, p.ID, next, modelExecutionError(e), s.now(), p.Version); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) modelNoticeLoop(ctx context.Context, m *modelStore) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		workCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		if err := s.aggregateModelNotices(workCtx, m); err != nil && ctx.Err() == nil {
			log.Printf("[UpstreamGovernance] model aggregate: %s", ErrorCode(err))
		}
		if s.modelNotifier != nil {
			if err := s.deliverModelNotices(workCtx, m); err != nil && ctx.Err() == nil {
				log.Printf("[UpstreamGovernance] model notification: %s", ErrorCode(err))
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// A policy incident is updated once per completed batch; comparison tiers in
// the same batch cannot each send duplicate alerts.
func (s *Service) aggregateModelNotices(ctx context.Context, m *modelStore) error {
	for processed := 0; processed < 30; processed++ {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err = modelAdmission(ctx, tx); err != nil {
			tx.Rollback()
			return err
		}
		var batchID string
		var policyID sql.NullInt64
		var siteID, policyVersion int64
		err = tx.QueryRowContext(ctx, `SELECT id,site_id,policy_id,policy_version FROM upstream_governance_model_batches WHERE status IN ('completed','cancelled') AND NOT notification_processed ORDER BY finished_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&batchID, &siteID, &policyID, &policyVersion)
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return nil
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_batches SET notification_processed=TRUE WHERE id=$1`, batchID); err != nil {
			tx.Rollback()
			return err
		}
		if !policyID.Valid {
			if err = tx.Commit(); err != nil {
				return err
			}
			continue
		}
		p, err := scanModelPolicy(tx.QueryRowContext(ctx, `SELECT `+modelPolicyColumns+` FROM upstream_governance_model_policies WHERE id=$1 FOR UPDATE`, policyID.Int64))
		if errors.Is(err, ErrNotFound) {
			tx.Rollback()
			continue
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if p.Version != policyVersion {
			if err = tx.Commit(); err != nil {
				return err
			}
			continue
		}
		var failures, suspicious, successes int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='failed'),
count(*) FILTER(WHERE status='succeeded' AND (result->'tokens'->>'input_state' IN ('suspected_difference','invalid_usage') OR result->'tokens'->>'output_state' IN ('suspected_difference','invalid_usage') OR result->'tokens'->>'markers_passed'='false')),count(*) FILTER(WHERE status='succeeded') FROM upstream_governance_model_runs WHERE batch_id=$1`, batchID).Scan(&failures, &suspicious, &successes)
		if err != nil {
			tx.Rollback()
			return err
		}
		var count int
		var old string
		var incident int64
		if err = tx.QueryRowContext(ctx, `SELECT failure_count,incident_state,incident_number FROM upstream_governance_model_policies WHERE id=$1`, p.ID).Scan(&count, &old, &incident); err != nil {
			tx.Rollback()
			return err
		}
		state := old
		kind := ""
		if failures > 0 {
			count++
			if count >= p.FailureThreshold {
				state = "failure"
			}
		} else if suspicious > 0 {
			count = 0
			state = "token_suspicious"
		} else if successes > 0 {
			count = 0
			state = "healthy"
		}
		if state != old {
			incident++
			kind = state
			if state == "healthy" {
				kind = "recovered"
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_policies SET failure_count=$2,incident_state=$3,incident_number=$4 WHERE id=$1`, p.ID, count, state, incident); err != nil {
			tx.Rollback()
			return err
		}
		if kind != "" && p.NotifyEnabled && p.Enabled {
			var name, baseURL string
			if err = tx.QueryRowContext(ctx, `SELECT name,base_url FROM upstream_governance_sites WHERE id=$1`, siteID).Scan(&name, &baseURL); err != nil {
				tx.Rollback()
				return err
			}
			n := ModelNotice{SiteID: siteID, SiteName: name, BaseURL: baseURL, PolicyName: p.Name, Model: p.Config.Model, Kind: kind, Detail: fmt.Sprintf("batch %s; failed=%d, token_suspicious=%d, succeeded=%d", batchID, failures, suspicious, successes), ObservedAt: s.now()}
			raw, _ := json.Marshal(n)
			recipients, _ := json.Marshal(p.Recipients)
			if _, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_model_notices(policy_id,incident_number,kind,notice,recipients,policy_version) VALUES($1,$2,$3,$4::jsonb,$5::jsonb,$6) ON CONFLICT DO NOTHING`, p.ID, incident, kind, string(raw), string(recipients), p.Version); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deliverModelNotices(ctx context.Context, m *modelStore) error {
	for attempts := 0; attempts < 10; attempts++ {
		var id, policyID int64
		var raw, recipientsRaw, deliveredRaw []byte
		err := m.db.QueryRowContext(ctx, `WITH candidate AS (SELECT n.id FROM upstream_governance_model_notices n JOIN upstream_governance_model_policies p ON p.id=n.policy_id WHERE p.enabled AND p.notify_enabled AND p.version=n.policy_version AND n.next_attempt_at<=$1 AND (n.status='pending' OR(n.status='sending' AND n.lease_until<=$1)) ORDER BY n.id LIMIT 1 FOR UPDATE OF n SKIP LOCKED)
UPDATE upstream_governance_model_notices n SET status='sending',lease_until=$2 FROM candidate WHERE n.id=candidate.id RETURNING n.id,n.policy_id,n.notice,n.recipients,n.delivered`, s.now(), s.now().Add(2*time.Minute)).Scan(&id, &policyID, &raw, &recipientsRaw, &deliveredRaw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var notice ModelNotice
		var recipients, delivered []string
		if err = json.Unmarshal(raw, &notice); err != nil {
			return err
		}
		if err = json.Unmarshal(recipientsRaw, &recipients); err != nil {
			return err
		}
		if err = json.Unmarshal(deliveredRaw, &delivered); err != nil {
			return err
		}
		resolved, sendErr := s.modelNotifier.Recipients(ctx, recipients)
		if sendErr == nil && len(resolved) == 0 {
			sendErr = errors.New("no notification recipient")
		}
		if sendErr == nil {
			resolved, sendErr = normalizeBalanceRecipients(resolved)
		}
		if sendErr == nil {
			// Freeze resolved default recipients once so retries are deterministic.
			resolvedJSON, _ := json.Marshal(resolved)
			if _, err = m.db.ExecContext(ctx, `UPDATE upstream_governance_model_notices SET recipients=$2::jsonb WHERE id=$1`, id, string(resolvedJSON)); err != nil {
				return err
			}
			seen := map[string]bool{}
			for _, r := range delivered {
				seen[r] = true
			}
			for _, recipient := range resolved {
				if seen[recipient] {
					continue
				}
				sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				e := s.modelNotifier.SendModel(sendCtx, recipient, notice)
				cancel()
				if e != nil {
					sendErr = e
					break
				}
				delivered = append(delivered, recipient)
				record, _ := json.Marshal(delivered)
				if _, err = m.db.ExecContext(ctx, `UPDATE upstream_governance_model_notices SET delivered=$2::jsonb WHERE id=$1`, id, string(record)); err != nil {
					return err
				}
			}
		}
		code := ""
		status := "sent"
		var sentAt any = s.now()
		if sendErr != nil {
			code = "notification_failed"
			status = "pending"
			sentAt = nil
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = m.db.ExecContext(persistCtx, `UPDATE upstream_governance_model_notices SET status=$2,last_error=$3,next_attempt_at=$4,lease_until=NULL,sent_at=$5 WHERE id=$1`, id, status, code, s.now().Add(5*time.Minute), sentAt)
		if err == nil {
			_, err = m.db.ExecContext(persistCtx, `UPDATE upstream_governance_model_policies SET notify_error=$2,notify_at=$3 WHERE id=$1`, policyID, code, s.now())
		}
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
