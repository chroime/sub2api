package upstreamgovernance

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type modelEngineNotifierFixture struct {
	calls      int
	fail       bool
	recipients []string
}

func (n *modelEngineNotifierFixture) Recipients(context.Context, []string) ([]string, error) {
	return n.recipients, nil
}
func (n *modelEngineNotifierFixture) SendModel(context.Context, string, ModelNotice) error {
	n.calls++
	if n.fail {
		return errors.New("fixture mail failure")
	}
	return nil
}

func (f *modelEngineFixture) finishPolicyBatch(t *testing.T, p *ModelPolicy, id, status string, r ModelRunResult) *ModelRun {
	t.Helper()
	_, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: id, PolicyID: p.ID})
	require.NoError(t, err)
	claim, err := f.store.claim(t.Context(), "fixture-worker", time.Now())
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.NoError(t, f.store.finish(t.Context(), claim.Run.ID, "fixture-worker", status, r, time.Now()))
	require.NoError(t, f.service.aggregateModelNotices(t.Context(), f.store))
	run, err := f.service.GetModelRun(t.Context(), f.site.ID, claim.Run.ID)
	require.NoError(t, err)
	return run
}

func TestModelEngineNoticeDedupRecoveryAndReviewedQuality(t *testing.T) {
	f := newModelEngineFixture(t)
	p := f.policy(t, true)
	f.finishPolicyBatch(t, p, "fail1", "failed", ModelRunResult{ErrorCode: "upstream_unavailable"})
	require.Zero(t, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices`))
	f.finishPolicyBatch(t, p, "fail2", "failed", ModelRunResult{ErrorCode: "upstream_unavailable"})
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='failure'`))
	f.finishPolicyBatch(t, p, "fail3", "failed", ModelRunResult{ErrorCode: "upstream_unavailable"})
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='failure'`))
	f.finishPolicyBatch(t, p, "recovered", "succeeded", ModelRunResult{Success: true})
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='recovered'`))
	run := f.finishPolicyBatch(t, p, "usage-difference", "succeeded", ModelRunResult{Success: true, ResponseText: "Original proof and exact answer preserved", Tokens: ModelTokenAudit{InputState: "suspected_difference"}})
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='token_suspicious'`))
	_, err := f.service.ReviewModelRun(t.Context(), f.site.ID, run.ID, "fail", "incorrect proof", 42)
	require.NoError(t, err)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='quality_failed'`))
	_, err = f.service.ReviewModelRun(t.Context(), f.site.ID, run.ID, "fail", "still incorrect", 42)
	require.NoError(t, err)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='quality_failed'`))
	got, err := f.service.ReviewModelRun(t.Context(), f.site.ID, run.ID, "pass", "corrected review after checking proof", 42)
	require.NoError(t, err)
	require.Equal(t, "Original proof and exact answer preserved", got.Result.ResponseText)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices WHERE kind='quality_recovered'`))
	require.NoError(t, f.service.aggregateModelNotices(t.Context(), f.store))
	require.Equal(t, 5, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices`))
	mail := &modelEngineNotifierFixture{fail: true, recipients: []string{"fixture@example.test"}}
	f.service.SetModelNotifier(mail)
	require.NoError(t, f.service.deliverModelNotices(t.Context(), f.store))
	gotPolicy, err := f.store.policy(t.Context(), f.site.ID, p.ID)
	require.NoError(t, err)
	require.Equal(t, "notification_failed", gotPolicy.NotifyError)
	require.NotNil(t, gotPolicy.NotifyAt)
	require.Equal(t, 5, mail.calls)
	require.NoError(t, f.service.deliverModelNotices(t.Context(), f.store))
	require.Equal(t, 5, mail.calls, "retry delay prevents repeated SMTP work")
	mail.fail = false
	_, err = f.db.Exec(`UPDATE upstream_governance_model_notices SET next_attempt_at=NOW()-interval '1 minute'`)
	require.NoError(t, err)
	require.NoError(t, f.service.deliverModelNotices(t.Context(), f.store))
	require.Equal(t, 10, mail.calls)
	require.NoError(t, f.service.deliverModelNotices(t.Context(), f.store))
	require.Equal(t, 10, mail.calls, "sent incidents are durably deduplicated")
	gotPolicy, err = f.store.policy(t.Context(), f.site.ID, p.ID)
	require.NoError(t, err)
	require.Empty(t, gotPolicy.NotifyError)
}

func TestModelEngineCancelledAndObsoleteResultsDoNotAlert(t *testing.T) {
	f := newModelEngineFixture(t)
	p := f.policy(t, true)
	no := false
	f.finishPolicyBatch(t, p, "cancelled-marker", "cancelled", ModelRunResult{Tokens: ModelTokenAudit{MarkersPassed: &no, InputState: "suspected_difference"}})
	f.finishPolicyBatch(t, p, "interrupted-marker", "indeterminate", ModelRunResult{Tokens: ModelTokenAudit{MarkersPassed: &no, InputState: "suspected_difference"}})
	f.finishPolicyBatch(t, p, "skipped-config", "skipped", ModelRunResult{ErrorCode: "target_changed"})
	require.Zero(t, f.count(t, `SELECT failure_count FROM upstream_governance_model_policies WHERE id=$1`, p.ID))
	require.Zero(t, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices`))
	f.finishPolicyBatch(t, p, "first-failure", "failed", ModelRunResult{ErrorCode: "upstream_unavailable"})
	require.Equal(t, 1, f.count(t, `SELECT failure_count FROM upstream_governance_model_policies WHERE id=$1`, p.ID))
	p.Name = "Revised baseline"
	p, err := f.service.SaveModelPolicy(t.Context(), f.site.ID, *p)
	require.NoError(t, err)
	require.Zero(t, f.count(t, `SELECT failure_count FROM upstream_governance_model_policies WHERE id=$1`, p.ID))
	f.finishPolicyBatch(t, p, "revision-fail1", "failed", ModelRunResult{ErrorCode: "upstream_unavailable"})
	require.Zero(t, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices`))
	f.finishPolicyBatch(t, p, "revision-fail2", "failed", ModelRunResult{ErrorCode: "upstream_unavailable"})
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices`))
	p.Config.Model = "gpt-6-sol"
	_, err = f.service.SaveModelPolicy(t.Context(), f.site.ID, *p)
	require.NoError(t, err)
	mail := &modelEngineNotifierFixture{recipients: []string{"fixture@example.test"}}
	f.service.SetModelNotifier(mail)
	require.NoError(t, f.service.deliverModelNotices(t.Context(), f.store))
	require.Zero(t, mail.calls, "old policy revision cannot send pending mail")
}

func TestModelEngineQueuedBudgetFollowsDispatchUTCDay(t *testing.T) {
	f := newModelEngineFixture(t)
	day1 := time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)
	f.service.now = func() time.Time { return day1 }
	p, err := f.service.SaveModelPolicy(t.Context(), f.site.ID, ModelPolicy{Name: "Midnight budget", Config: f.config, Enabled: true, IntervalMinutes: 1, DailyRequestLimit: 1, FailureThreshold: 1})
	require.NoError(t, err)
	_, err = f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "queued-before-midnight", PolicyID: p.ID})
	require.NoError(t, err)
	day2 := day1.Add(2 * time.Minute)
	f.service.now = func() time.Time { return day2 }
	_, err = f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "reserved-after-midnight", PolicyID: p.ID})
	require.NoError(t, err)
	claim, err := f.store.claim(t.Context(), "fixture", day2)
	require.NoError(t, err)
	require.Nil(t, claim, "yesterday's reservation cannot bypass a full current-day cap")
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM upstream_governance_model_runs WHERE status='skipped' AND result->>'error_code'='daily_request_budget'`))
	claim, err = f.store.claim(t.Context(), "fixture", day2)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, 1, f.count(t, `SELECT reserved_requests FROM upstream_governance_model_budgets WHERE policy_id=$1 AND budget_day='2026-09-28'`, p.ID))
}

func TestModelEngineGlobalCapacityAcrossTargetsAndWorkers(t *testing.T) {
	f := newModelEngineFixture(t)
	c := f.config
	c.Samples = 40
	c.Concurrency = 32
	f.batch(t, "capacity-first", c)
	c.Model = "gpt-6-sol"
	f.batch(t, "capacity-second", c)
	claims := []*modelClaim{}
	for i := 0; i < 32; i++ {
		claim, err := f.store.claim(t.Context(), fmt.Sprintf("worker-%d", i), time.Now())
		require.NoError(t, err)
		require.NotNil(t, claim)
		claims = append(claims, claim)
	}
	claim, err := f.store.claim(t.Context(), "extra-worker", time.Now())
	require.NoError(t, err)
	require.Nil(t, claim)
	require.NoError(t, f.store.finish(t.Context(), claims[0].Run.ID, "worker-0", "succeeded", ModelRunResult{Success: true}, time.Now()))
	claim, err = f.store.claim(t.Context(), "extra-worker", time.Now())
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, 32, f.count(t, `SELECT count(*) FROM upstream_governance_model_runs WHERE status='running'`))
}

func TestModelEngineChangingIdentityDuringStreamExcludesOldEvidence(t *testing.T) {
	f := newModelEngineFixture(t)
	p := f.policy(t, true)
	var calls atomic.Int32
	f.runner.run = func(_ context.Context, _ Site, _ RemoteKey, _ ModelRunRequest) (ModelRunResult, error) {
		calls.Add(1)
		_, err := f.db.Exec(`UPDATE upstream_governance_keys SET key_cipher=$2 WHERE id=$1`, f.key.ID, "new-identity")
		return ModelRunResult{Success: true, ResponseText: "answer from prior identity"}, err
	}
	_, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "change-while-running", PolicyID: p.ID})
	require.NoError(t, err)
	claim, err := f.store.claim(t.Context(), "fixture", time.Now())
	require.NoError(t, err)
	require.NotNil(t, claim)
	f.service.executeModelRun(t.Context(), t.Context(), f.store, "fixture", *claim)
	run, err := f.service.GetModelRun(t.Context(), f.site.ID, claim.Run.ID)
	require.NoError(t, err)
	require.Equal(t, "indeterminate", run.Status)
	require.Equal(t, "target_changed", run.Result.ErrorCode)
	require.Equal(t, "answer from prior identity", run.Result.ResponseText)
	require.NoError(t, f.service.aggregateModelNotices(t.Context(), f.store))
	require.Zero(t, f.count(t, `SELECT count(*) FROM upstream_governance_model_notices`))
	require.NoError(t, f.service.scheduleModels(t.Context(), f.store))
	p, err = f.store.policy(t.Context(), f.site.ID, p.ID)
	require.NoError(t, err)
	require.False(t, p.Enabled)
	require.Equal(t, "target_changed", p.LastError)
	require.EqualValues(t, 1, calls.Load())
}

func TestModelEngineDisablingPolicyCancelsActiveStreamThroughLease(t *testing.T) {
	f := newModelEngineFixture(t)
	f.config.Samples = 3
	p := f.policy(t, true)
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	f.runner.run = func(ctx context.Context, _ Site, _ RemoteKey, _ ModelRunRequest) (ModelRunResult, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		return ModelRunResult{ResponseText: "partial evaluation"}, ctx.Err()
	}
	_, err := f.service.StartModelBatch(t.Context(), f.site.ID, ModelBatchInput{RequestID: "disable-running-policy", PolicyID: p.ID})
	require.NoError(t, err)
	f.service.startModelWorker()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	p.Enabled = false
	_, err = f.service.SaveModelPolicy(t.Context(), f.site.ID, *p)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT count(*) FROM upstream_governance_model_runs WHERE status='cancelled'`) == 3
	}, 6*time.Second, 30*time.Millisecond)
	require.EqualValues(t, 1, calls.Load(), "disabling the policy cancels streaming and leaves queued samples unsent")
	f.service.stopModelWorker()
}
