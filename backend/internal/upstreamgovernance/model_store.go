package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// This optional port leaves existing governance Store implementations compatible.
type modelDBProvider interface{ modelDB() *sql.DB }

func (s *sqlStore) modelDB() *sql.DB { return s.db }

type modelStore struct{ db *sql.DB }

func (s *Service) models() (*modelStore, error) {
	p, ok := s.store.(modelDBProvider)
	if !ok || p.modelDB() == nil {
		return nil, ErrUnsupported
	}
	return &modelStore{db: p.modelDB()}, nil
}

const modelPolicyColumns = `id,site_id,name,config,enabled,interval_minutes,daily_request_limit,notify_enabled,recipients,failure_threshold,version,next_run_at,last_error,created_at,updated_at,notify_error,notify_at`

func scanModelPolicy(row rowScanner) (*ModelPolicy, error) {
	var p ModelPolicy
	var config, recipients []byte
	err := row.Scan(&p.ID, &p.SiteID, &p.Name, &config, &p.Enabled, &p.IntervalMinutes, &p.DailyRequestLimit, &p.NotifyEnabled, &recipients, &p.FailureThreshold, &p.Version, &p.NextRunAt, &p.LastError, &p.CreatedAt, &p.UpdatedAt, &p.NotifyError, &p.NotifyAt)
	if err != nil {
		return nil, storeError(err)
	}
	if err = json.Unmarshal(config, &p.Config); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(recipients, &p.Recipients); err != nil {
		return nil, err
	}
	if p.Recipients == nil {
		p.Recipients = []string{}
	}
	return &p, nil
}
func (m *modelStore) policies(ctx context.Context, siteID int64) ([]ModelPolicy, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT `+modelPolicyColumns+` FROM upstream_governance_model_policies WHERE site_id=$1 ORDER BY id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ModelPolicy{}
	for rows.Next() {
		p, e := scanModelPolicy(rows)
		if e != nil {
			return nil, e
		}
		list = append(list, *p)
	}
	return list, rows.Err()
}
func (m *modelStore) policy(ctx context.Context, siteID, id int64) (*ModelPolicy, error) {
	return scanModelPolicy(m.db.QueryRowContext(ctx, `SELECT `+modelPolicyColumns+` FROM upstream_governance_model_policies WHERE site_id=$1 AND id=$2`, siteID, id))
}

const modelRunColumns = `id,batch_id,site_id,policy_id,sequence,request,status,result,review,review_note,created_at,started_at,finished_at,reviewer_id,reviewed_at,review_version`

// Large prompts and original HTML are loaded only from the run detail endpoint.
const modelRunSummaryColumns = `id,batch_id,site_id,policy_id,sequence,request,status,result-'request_body'-'input_text'-'response_text'-'html'-'sent_parameters'-'raw_usage',review,review_note,created_at,started_at,finished_at,reviewer_id,reviewed_at,review_version`

func scanModelRun(row rowScanner) (*ModelRun, error) {
	var r ModelRun
	var request, result []byte
	err := row.Scan(&r.ID, &r.BatchID, &r.SiteID, &r.PolicyID, &r.Sequence, &request, &r.Status, &result, &r.Review, &r.ReviewNote, &r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.ReviewerID, &r.ReviewedAt, &r.ReviewVersion)
	if err != nil {
		return nil, storeError(err)
	}
	if err = json.Unmarshal(request, &r.Request); err != nil {
		return nil, err
	}
	if len(result) > 0 && string(result) != "null" {
		if err = json.Unmarshal(result, &r.Result); err != nil {
			return nil, err
		}
	}
	return &r, nil
}
func (m *modelStore) getRun(ctx context.Context, siteID int64, id string) (*ModelRun, error) {
	return scanModelRun(m.db.QueryRowContext(ctx, `SELECT `+modelRunColumns+` FROM upstream_governance_model_runs WHERE site_id=$1 AND id=$2`, siteID, id))
}
func (m *modelStore) listRuns(ctx context.Context, siteID int64, page, size int, batchID string) (*ModelRunPage, error) {
	p := &ModelRunPage{Items: []ModelRun{}, Page: page, PageSize: size, Counts: map[string]int64{}}
	if err := m.db.QueryRowContext(ctx, `SELECT count(*) FROM upstream_governance_model_runs WHERE site_id=$1 AND ($2='' OR batch_id=$2)`, siteID, batchID).Scan(&p.Total); err != nil {
		return nil, err
	}
	counts, err := m.db.QueryContext(ctx, `SELECT status,count(*) FROM upstream_governance_model_runs WHERE site_id=$1 AND ($2='' OR batch_id=$2) GROUP BY status`, siteID, batchID)
	if err != nil {
		return nil, err
	}
	for counts.Next() {
		var status string
		var n int64
		if err = counts.Scan(&status, &n); err != nil {
			counts.Close()
			return nil, err
		}
		p.Counts[status] = n
	}
	if err = counts.Err(); err != nil {
		counts.Close()
		return nil, err
	}
	counts.Close()
	rows, err := m.db.QueryContext(ctx, `SELECT `+modelRunSummaryColumns+` FROM upstream_governance_model_runs WHERE site_id=$1 AND ($2='' OR batch_id=$2) ORDER BY created_at DESC,batch_id,sequence LIMIT $3 OFFSET $4`, siteID, batchID, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scanModelRun(rows)
		if e != nil {
			return nil, e
		}
		p.Items = append(p.Items, *r)
	}
	return p, rows.Err()
}

type modelIdentity struct {
	BaseURL      string `json:"base_url"`
	SitePlatform string `json:"site_platform"`
	ProxyID      *int64 `json:"proxy_id"`
	ManagedKeyID int64  `json:"managed_key_id"`
	GroupID      string `json:"group_id"`
	Platform     string `json:"platform"`
	KeyHash      string `json:"key_hash"`
	OwnerUserID  int64  `json:"owner_user_id"`
}
type modelEnqueue struct {
	SiteID                             int64
	PolicyID                           int64
	RequestID, RequestHash, TargetHash string
	Identity                           modelIdentity
	Config                             ModelTestConfig
	Now                                time.Time
	Scheduled                          bool
	PolicyVersion                      int64
}

// All admission changes are serialized briefly in PostgreSQL, not across the
// billable network operation. This also makes capacity global across processes.
func modelAdmission(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(741938221)`)
	return err
}
func modelQueueTx(ctx context.Context, tx *sql.Tx, e modelEnqueue) (*ModelBatch, error) {
	var previous ModelBatch
	var hash string
	err := tx.QueryRowContext(ctx, `SELECT id,total,concurrency,request_hash FROM upstream_governance_model_batches WHERE site_id=$1 AND request_id=$2`, e.SiteID, e.RequestID).Scan(&previous.ID, &previous.Total, &previous.Concurrency, &hash)
	if err == nil {
		if hash != e.RequestHash {
			return nil, ErrConflict
		}
		return &previous, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	batch := &ModelBatch{ID: uuid.NewString(), Total: len(e.Config.Efforts) * len(e.Config.Templates) * e.Config.Samples, Concurrency: e.Config.Concurrency}
	var policyID any
	if e.PolicyID > 0 {
		p, err := scanModelPolicy(tx.QueryRowContext(ctx, `SELECT `+modelPolicyColumns+` FROM upstream_governance_model_policies WHERE site_id=$1 AND id=$2 FOR UPDATE`, e.SiteID, e.PolicyID))
		if err != nil {
			return nil, err
		}
		if p.Version != e.PolicyVersion || !p.Enabled || (e.Scheduled && p.NextRunAt.After(e.Now)) {
			return nil, ErrConflict
		}
		if e.Scheduled {
			var active bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_governance_model_batches WHERE policy_id=$1 AND status IN ('queued','running'))`, p.ID).Scan(&active); err != nil {
				return nil, err
			}
			if active {
				return nil, ErrBusy
			}
		}
		// The daily cap is reserved when queued and never refunded: an uncertain
		// upstream outcome cannot accidentally make budget available for a retry.
		var reserved int
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_model_budgets(policy_id,budget_day,reserved_requests) VALUES($1,$2::date,$3)
ON CONFLICT(policy_id,budget_day) DO UPDATE SET reserved_requests=upstream_governance_model_budgets.reserved_requests+EXCLUDED.reserved_requests
WHERE upstream_governance_model_budgets.reserved_requests+EXCLUDED.reserved_requests<=$4 RETURNING reserved_requests`, e.PolicyID, e.Now.UTC().Format("2006-01-02"), batch.Total, p.DailyRequestLimit).Scan(&reserved)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelBudget
		}
		if err != nil {
			return nil, err
		}
		if reserved > p.DailyRequestLimit {
			return nil, ErrModelBudget
		}
		policyID = e.PolicyID
		if e.Scheduled {
			if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_policies SET next_run_at=$2,last_error='',updated_at=$3 WHERE id=$1`, p.ID, addMinutes(e.Now, int64(p.IntervalMinutes)), e.Now); err != nil {
				return nil, err
			}
		}
	}
	identity, _ := json.Marshal(e.Identity)
	config, _ := json.Marshal(e.Config)
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_model_batches(id,site_id,policy_id,request_id,request_hash,target_hash,identity,config,total,concurrency,scheduled,created_at,policy_version) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13)`, batch.ID, e.SiteID, policyID, e.RequestID, e.RequestHash, e.TargetHash, string(identity), string(config), batch.Total, batch.Concurrency, e.Scheduled, e.Now, e.PolicyVersion)
	if err != nil {
		return nil, err
	}
	sequence := 0
	for _, template := range e.Config.Templates {
		for _, effort := range e.Config.Efforts {
			for sample := 1; sample <= e.Config.Samples; sample++ {
				sequence++
				request, _ := json.Marshal(ModelRunRequest{Config: e.Config, Template: template, Effort: effort, Sample: sample})
				var budgetDay any
				if policyID != nil {
					budgetDay = e.Now.UTC().Format("2006-01-02")
				}
				_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_model_runs(id,batch_id,site_id,policy_id,sequence,request,created_at,budget_day) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8::date)`, uuid.NewString(), batch.ID, e.SiteID, policyID, sequence, string(request), e.Now, budgetDay)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return batch, nil
}
func (m *modelStore) enqueue(ctx context.Context, e modelEnqueue) (*ModelBatch, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return nil, err
	}
	b, err := modelQueueTx(ctx, tx, e)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return b, nil
}

func modelSettleTx(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE upstream_governance_model_batches b SET status=CASE WHEN cancel_requested THEN 'cancelled' ELSE 'completed' END,finished_at=$1
WHERE status IN ('queued','running') AND NOT EXISTS(SELECT 1 FROM upstream_governance_model_runs r WHERE r.batch_id=b.id AND r.status IN ('queued','running'))`, now)
	return err
}

type modelClaim struct {
	Run           ModelRun
	Identity      modelIdentity
	Scheduled     bool
	PolicyVersion int64
}

func (m *modelStore) claim(ctx context.Context, owner string, now time.Time) (*modelClaim, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return nil, err
	}
	// A sent request can have billed even if its worker died before saving the
	// answer. Never put an expired running lease back onto the runnable queue.
	_, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_runs SET status='indeterminate',finished_at=$1,result='{"success":false,"error_code":"worker_interrupted"}'::jsonb,lease_owner='',lease_until=NULL WHERE status='running' AND lease_until<=$1`, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_runs r SET status='cancelled',finished_at=$1,result='{"success":false,"error_code":"policy_or_site_disabled"}'::jsonb
FROM upstream_governance_model_batches b JOIN upstream_governance_sites s ON s.id=b.site_id LEFT JOIN upstream_governance_model_policies p ON p.id=b.policy_id
WHERE r.batch_id=b.id AND r.status='queued' AND (b.cancel_requested OR NOT s.enabled OR (b.policy_version>0 AND (p.id IS NULL OR NOT p.enabled OR p.version<>b.policy_version)))`, now)
	if err != nil {
		return nil, err
	}
	if err = modelSettleTx(ctx, tx, now); err != nil {
		return nil, err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM upstream_governance_model_runs WHERE status='running'`).Scan(&active); err != nil {
		return nil, err
	}
	if active >= 32 {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	var id, batch string
	var identity []byte
	var scheduled bool
	var policyVersion int64
	var policyID sql.NullInt64
	var budgetDay string
	err = tx.QueryRowContext(ctx, `SELECT r.id,b.id,b.identity,b.scheduled,b.policy_version,b.policy_id,COALESCE(r.budget_day::text,'')
FROM upstream_governance_model_runs r JOIN upstream_governance_model_batches b ON b.id=r.batch_id
WHERE r.status='queued' AND NOT b.cancel_requested AND b.status IN ('queued','running')
AND (SELECT count(*) FROM upstream_governance_model_runs a WHERE a.batch_id=b.id AND a.status='running')<b.concurrency
AND NOT EXISTS(SELECT 1 FROM upstream_governance_model_batches other WHERE other.target_hash=b.target_hash AND other.status='running' AND other.id<>b.id)
ORDER BY b.created_at,b.id,r.sequence LIMIT 1 FOR UPDATE OF r,b SKIP LOCKED`).Scan(&id, &batch, &identity, &scheduled, &policyVersion, &policyID, &budgetDay)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if policyID.Valid && budgetDay != now.UTC().Format("2006-01-02") {
		var limit, reserved int
		if err = tx.QueryRowContext(ctx, `SELECT daily_request_limit FROM upstream_governance_model_policies WHERE id=$1 FOR UPDATE`, policyID.Int64).Scan(&limit); err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_model_budgets(policy_id,budget_day,reserved_requests) VALUES($1,$2::date,1)
ON CONFLICT(policy_id,budget_day) DO UPDATE SET reserved_requests=upstream_governance_model_budgets.reserved_requests+1 WHERE upstream_governance_model_budgets.reserved_requests<$3 RETURNING reserved_requests`, policyID.Int64, now.UTC().Format("2006-01-02"), limit).Scan(&reserved)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_runs SET status='skipped',finished_at=$2,result='{"success":false,"error_code":"daily_request_budget"}'::jsonb WHERE id=$1`, id, now); err != nil {
				return nil, err
			}
			if err = modelSettleTx(ctx, tx, now); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_runs SET budget_day=$2::date WHERE id=$1`, id, now.UTC().Format("2006-01-02")); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_batches SET status='running' WHERE id=$1`, batch); err != nil {
		return nil, err
	}
	r, err := scanModelRun(tx.QueryRowContext(ctx, `UPDATE upstream_governance_model_runs SET status='running',lease_owner=$2,lease_until=$3,started_at=$4 WHERE id=$1 RETURNING `+modelRunColumns, id, owner, now.Add(30*time.Second), now))
	if err != nil {
		return nil, err
	}
	var ident modelIdentity
	if err = json.Unmarshal(identity, &ident); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &modelClaim{Run: *r, Identity: ident, Scheduled: scheduled, PolicyVersion: policyVersion}, nil
}
func (m *modelStore) renew(ctx context.Context, id, owner string, now time.Time) (bool, error) {
	var allowed bool
	err := m.db.QueryRowContext(ctx, `SELECT NOT b.cancel_requested AND s.enabled AND (b.policy_version=0 OR (COALESCE(p.enabled,FALSE) AND p.version=b.policy_version))
FROM upstream_governance_model_runs r JOIN upstream_governance_model_batches b ON b.id=r.batch_id JOIN upstream_governance_sites s ON s.id=b.site_id LEFT JOIN upstream_governance_model_policies p ON p.id=b.policy_id
WHERE r.id=$1 AND r.status='running' AND r.lease_owner=$2`, id, owner).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || !allowed {
		return allowed, err
	}
	result, err := m.db.ExecContext(ctx, `UPDATE upstream_governance_model_runs SET lease_until=$3 WHERE id=$1 AND status='running' AND lease_owner=$2`, id, owner, now.Add(30*time.Second))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (m *modelStore) finish(ctx context.Context, id, owner, status string, result ModelRunResult, now time.Time) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE upstream_governance_model_runs SET status=$3,result=$4::jsonb,finished_at=$5,lease_owner='',lease_until=NULL WHERE id=$1 AND lease_owner=$2 AND status='running'`, id, owner, status, string(raw), now)
	if err = affected(r, err, ErrConflict); err != nil {
		return err
	}
	if err = modelSettleTx(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}
