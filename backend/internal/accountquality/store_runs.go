package accountquality

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

func (s *Store) Bind(ctx context.Context, in BindInput, actor int64) ([]Binding, error) {
	if in.PolicyID <= 0 || len(in.AccountIDs) == 0 || len(in.AccountIDs) > 500 || len(in.Source) > 200 || in.Overrides.IntervalSeconds < 0 || in.Overrides.FailureThreshold < 0 || in.Overrides.RecoveryThreshold < 0 {
		return nil, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	p, e := loadPolicy(ctx, tx, in.PolicyID)
	if e != nil {
		return nil, e
	}
	if e = ValidatePolicy(EffectivePolicy(p, in.Overrides)); e != nil {
		return nil, e
	}
	out := []Binding{}
	seen := map[int64]bool{}
	// Every multi-account writer acquires account locks in the same order.
	in.AccountIDs = append([]int64(nil), in.AccountIDs...)
	sort.Slice(in.AccountIDs, func(i, j int) bool { return in.AccountIDs[i] < in.AccountIDs[j] })
	for _, id := range in.AccountIDs {
		if id <= 0 {
			return nil, ErrInvalid
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		var name, platform, typ string
		if e = tx.QueryRowContext(ctx, "SELECT name,platform,type FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id).Scan(&name, &platform, &typ); e != nil {
			return nil, storeErr(e)
		}
		if platform != p.Platform {
			return nil, fmt.Errorf("%w: account platform does not match policy", ErrInvalid)
		}
		eligible := s.eligible(platform, typ, p.APIMode)
		if in.AutoIsolate && !eligible {
			return nil, fmt.Errorf("%w: adapter is not eligible for automatic quality control", ErrInvalid)
		}
		b := Binding{Version: 1, AccountID: id, AccountName: name, AccountType: typ, Platform: platform, PolicyID: p.ID, PolicyName: p.Name, PolicyVersion: p.Version, Model: p.Model, AutoIsolate: in.AutoIsolate, AutoControlEligible: eligible, Source: in.Source, Overrides: in.Overrides, State: "untested", RecentRuns: []Run{}, NextRunAt: time.Now().UTC().Add(time.Duration(id%30) * time.Second)}
		var existingID, existingVersion int64
		err := tx.QueryRowContext(ctx, "SELECT id,version FROM account_quality_bindings WHERE account_id=$1 AND policy_id=$2 FOR UPDATE", id, p.ID).Scan(&existingID, &existingVersion)
		if err == nil {
			old, loadErr := loadBinding(ctx, tx, existingID)
			if loadErr != nil {
				return nil, loadErr
			}
			if old.Overrides == in.Overrides && old.AutoIsolate == in.AutoIsolate {
				out = append(out, old)
				continue
			}
			// Reapplying a policy is not an implicit quality release. Evidence,
			// counters, gate ownership and probe deadlines survive assignment edits.
			b = old
			b.Version = existingVersion + 1
			b.AutoIsolate, b.Overrides, b.Source = in.AutoIsolate, in.Overrides, in.Source
			e = saveBinding(ctx, tx, b)
			if e == nil && b.Isolated && old.Overrides.IsolationScope != b.Overrides.IsolationScope {
				e = gateTx(ctx, tx, b, p)
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			e = tx.QueryRowContext(ctx, "INSERT INTO account_quality_bindings(account_id,policy_id,version,state,next_run_at) VALUES($1,$2,1,$3::jsonb,$4) RETURNING id", id, p.ID, encode(b), b.NextRunAt).Scan(&b.ID)
		} else {
			return nil, err
		}
		if e != nil {
			return nil, e
		}
		if e = actionTx(ctx, tx, Action{AccountID: id, BindingID: b.ID, Action: "bound", Reason: "绑定策略：" + p.Name, ActorID: actor}); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	ids := []int64{}
	for _, b := range out {
		ids = append(ids, b.AccountID)
	}
	s.refresh(ctx, ids)
	return out, nil
}

func identityTx(ctx context.Context, tx *sql.Tx, id int64) (string, error) {
	var platform, typ string
	var credentials, extra []byte
	var proxyID, parentID sql.NullInt64
	e := tx.QueryRowContext(ctx, "SELECT platform,type,credentials,COALESCE(extra,'{}'::jsonb),proxy_id,parent_account_id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id).Scan(&platform, &typ, &credentials, &extra, &proxyID, &parentID)
	if e != nil {
		return "", storeErr(e)
	}
	in := IdentityInput{ID: id, ParentAccountID: parentID.Int64, Platform: platform, Type: typ}
	if e = json.Unmarshal(credentials, &in.Credentials); e != nil {
		return "", e
	}
	if e = json.Unmarshal(extra, &in.Extra); e != nil {
		return "", e
	}
	if proxyID.Valid {
		in.ProxyID = &proxyID.Int64
		var protocol, host, user, password, status string
		var port int
		var expiry sql.NullTime
		e = tx.QueryRowContext(ctx, "SELECT protocol,host,port,COALESCE(username,''),COALESCE(password,''),status,expires_at FROM proxies WHERE id=$1 AND deleted_at IS NULL FOR SHARE", proxyID.Int64).Scan(&protocol, &host, &port, &user, &password, &status, &expiry)
		if e != nil {
			return "", storeErr(e)
		}
		var expires *time.Time
		if expiry.Valid {
			expires = &expiry.Time
		}
		in.Proxy = map[string]any{"id": proxyID.Int64, "protocol": protocol, "host": host, "port": port, "username": user, "password": password, "status": status, "expires_at": expires}
	}
	return Fingerprint(in), nil
}
func readRun(row scanner) (Run, error) {
	var r Run
	var raw []byte
	var status, lease string
	e := row.Scan(&raw, &status, &lease)
	if e == nil {
		e = json.Unmarshal(raw, &r)
		r.ExecutionStatus = status
		r.Lease = lease
	}
	return r, storeErr(e)
}
func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	return readRun(s.DB.QueryRowContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE id=$1", id))
}
func (s *Store) Runs(ctx context.Context, account, binding int64, limit int) ([]Run, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE ($1::bigint=0 OR account_id=$1) AND ($2::bigint=0 OR binding_id=$2) ORDER BY created_at DESC,id DESC LIMIT $3", account, binding, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, e := readRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Actions(ctx context.Context, account int64) ([]Action, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,account_id,binding_id,action,reason,actor_id,created_at FROM account_quality_actions WHERE ($1::bigint=0 OR account_id=$1) ORDER BY created_at DESC,id DESC LIMIT 200", account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Action{}
	for rows.Next() {
		var a Action
		if e = rows.Scan(&a.ID, &a.AccountID, &a.BindingID, &a.Action, &a.Reason, &a.ActorID, &a.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Queue(ctx context.Context, bindingID, version, actor int64, requestID string, estimate func(Policy, string) *float64) (Run, error) {
	if len(requestID) > 150 {
		return Run{}, ErrInvalid
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Run{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(61824001)"); e != nil {
		return Run{}, e
	}
	existing, err := readRun(tx.QueryRowContext(ctx, "SELECT evidence,execution_status,lease_token FROM account_quality_runs WHERE request_id=$1", requestID))
	if err == nil {
		if existing.BindingID != bindingID {
			return Run{}, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Run{}, err
	}
	var policyID int64
	if e = tx.QueryRowContext(ctx, "SELECT policy_id FROM account_quality_bindings WHERE id=$1", bindingID).Scan(&policyID); e != nil {
		return Run{}, storeErr(e)
	}
	p, e := loadPolicy(ctx, tx, policyID)
	if e != nil {
		return Run{}, e
	}
	b, e := loadBinding(ctx, tx, bindingID)
	if e != nil {
		return Run{}, e
	}
	if version > 0 && b.Version != version {
		return Run{}, ErrConflict
	}
	if actor == 0 && !p.Enabled {
		return Run{}, ErrConflict
	}
	p = EffectivePolicy(p, b.Overrides)
	var busy bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM account_quality_runs WHERE account_id=$1 AND execution_status IN ('queued','running'))", b.AccountID).Scan(&busy); e != nil {
		return Run{}, e
	}
	if busy {
		return Run{}, ErrBusy
	}
	identity, e := identityTx(ctx, tx, b.AccountID)
	if e != nil {
		return Run{}, e
	}
	resolvedModel := p.Model
	if s.ResolveModel != nil {
		value, resolveErr := s.ResolveModel(ctx, b.AccountID, p.Model)
		if resolveErr != nil || value == "" {
			return Run{}, ErrConflict
		}
		resolvedModel = value
	}
	identityChanged := (b.IdentityVersion != "" && identity != b.IdentityVersion) || (b.ResolvedModel != "" && b.ResolvedModel != resolvedModel)
	if identityChanged {
		b.Failures = 0
		b.RecoveryPasses = 0
		b.Isolated = false
		b.OverrideUntil = nil
		b.State = "untested"
		if e = gateTx(ctx, tx, b, p); e != nil {
			return Run{}, e
		}
	}
	b.IdentityVersion = identity
	b.ResolvedModel = resolvedModel
	var count, daily, totalDaily int
	var usedCost float64
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FILTER(WHERE binding_id=$1),count(*) FILTER(WHERE created_at>=date_trunc('day',NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai') FROM account_quality_runs WHERE account_id=$2 AND execution_status<>'budget_paused'", b.ID, b.AccountID).Scan(&count, &daily); e != nil {
		return Run{}, e
	}
	settings, e := settingsTx(ctx, tx)
	if e != nil {
		return Run{}, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*),COALESCE(sum(GREATEST(COALESCE((evidence->>'cost_usd')::numeric,0),COALESCE((evidence->>'reserved_cost_usd')::numeric,0))),0) FROM account_quality_runs WHERE created_at>=date_trunc('day',NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai' AND execution_status<>'budget_paused'").Scan(&totalDaily, &usedCost); e != nil {
		return Run{}, e
	}
	template := p.Templates[count%len(p.Templates)]
	prompt, expected, e := BuildPrompt(template)
	if e != nil {
		return Run{}, e
	}
	var reserve *float64
	if estimate != nil {
		costPolicy := p
		costPolicy.Model = resolvedModel
		reserve = estimate(costPolicy, prompt)
	}
	trustedCap := false
	for _, capability := range s.Capabilities {
		if capability.Platform == b.Platform && capability.APIMode == p.APIMode && capability.OutputLimitMode == "upstream" {
			for _, typ := range capability.AccountTypes {
				if typ == b.AccountType {
					trustedCap = true
				}
			}
		}
	}
	r := Run{ID: uuid.NewString(), RequestID: requestID, BindingID: b.ID, AccountID: b.AccountID, AccountName: b.AccountName, PolicyID: p.ID, PolicyVersion: p.Version, BindingVersion: b.Version, Model: p.Model, Template: template, TemplateVersion: TemplateVersion(template), Prompt: prompt, Expected: expected, RequestedModel: p.Model, APIMode: p.APIMode, Effort: p.Effort, MaxOutputTokens: p.MaxOutputTokens, ExecutionStatus: "queued", QualityVerdict: "not_evaluated", CreatedAt: time.Now().UTC(), Config: p, IdentityVersion: identity, ActorID: actor}
	r.MappedModel = resolvedModel
	r.AdapterVersion = "account-text-probe-20261025-v1"
	if reserve != nil && *reserve >= 0 {
		r.ReservedCostUSD = *reserve
	}
	paused := daily >= p.DailyRequestLimit || totalDaily >= settings.DailyRequestLimit || (settings.DailyCostLimitUSD > 0 && (!trustedCap || reserve == nil || usedCost+r.ReservedCostUSD > settings.DailyCostLimitUSD))
	if paused {
		r.ExecutionStatus = "budget_paused"
		r.Reason = "已达到诊断请求预算，或金额预算所需的可信模型单价不可用"
		now := time.Now().UTC()
		r.FinishedAt = &now
		b, event := Advance(b, p, r, now)
		_ = event
		b.NextRunAt = now.Add(time.Hour)
		if e = saveBinding(ctx, tx, b); e != nil {
			return Run{}, e
		}
		if e = enqueueNoticeTx(ctx, tx, settings, b, r, "budget_paused"); e != nil {
			return Run{}, e
		}
	} else {
		b.NextRunAt = time.Now().UTC().Add(time.Duration(p.IntervalSeconds) * time.Second)
		if e = saveBinding(ctx, tx, b); e != nil {
			return Run{}, e
		}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO account_quality_runs(id,request_id,account_id,binding_id,policy_id,execution_status,evidence,finished_at) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8)", r.ID, r.RequestID, r.AccountID, r.BindingID, r.PolicyID, r.ExecutionStatus, encode(r), r.FinishedAt)
	if e != nil {
		return Run{}, e
	}
	if actor > 0 {
		if e = actionTx(ctx, tx, Action{AccountID: b.AccountID, BindingID: b.ID, Action: "manual_run", Reason: "手动执行账号质量检测", ActorID: actor}); e != nil {
			return Run{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return Run{}, e
	}
	if identityChanged {
		s.refresh(ctx, []int64{b.AccountID})
	}
	return r, nil
}

func (s *Store) Control(ctx context.Context, id int64, in ControlInput, actor int64) (Binding, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version <= 0 || len(in.Reason) < 1 || len(in.Reason) > 2000 {
		return Binding{}, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Binding{}, e
	}
	defer tx.Rollback()
	var policyID int64
	if e = tx.QueryRowContext(ctx, "SELECT policy_id FROM account_quality_bindings WHERE id=$1", id).Scan(&policyID); e != nil {
		return Binding{}, storeErr(e)
	}
	p, e := loadPolicy(ctx, tx, policyID)
	if e != nil {
		return Binding{}, e
	}
	b, e := loadBinding(ctx, tx, id)
	if e != nil {
		return b, e
	}
	if b.Version != in.Version {
		return b, ErrConflict
	}
	switch in.Action {
	case "isolate":
		b.Isolated = true
		b.State = "quarantined"
		b.OverrideUntil = nil
		b.NextRunAt = time.Now().UTC().Add(time.Duration(p.CooldownSeconds) * time.Second)
	case "release", "confirm_false_positive":
		b.Isolated = false
		b.OverrideUntil = nil
		b.Failures = 0
		b.RecoveryPasses = 0
		b.State = "untested"
	case "override":
		if !b.Isolated || in.DurationMinutes < 1 || in.DurationMinutes > 1440 {
			return b, ErrInvalid
		}
		until := time.Now().UTC().Add(time.Duration(in.DurationMinutes) * time.Minute)
		b.OverrideUntil = &until
	case "set_auto":
		if in.AutoIsolate == nil {
			return b, ErrInvalid
		}
		if *in.AutoIsolate && !s.eligible(b.Platform, b.AccountType, p.APIMode) {
			return b, fmt.Errorf("%w: adapter does not support automatic control", ErrInvalid)
		}
		b.AutoIsolate = *in.AutoIsolate
	default:
		return b, ErrInvalid
	}
	b.Version++
	b.Reason = in.Reason
	b.PolicyName = p.Name
	b.PolicyVersion = p.Version
	b.Model = p.Model
	b.AutoControlEligible = s.eligible(b.Platform, b.AccountType, p.APIMode)
	if e = saveBinding(ctx, tx, b); e != nil {
		return b, e
	}
	if e = gateTx(ctx, tx, b, p); e != nil {
		return b, e
	}
	if e = actionTx(ctx, tx, Action{AccountID: b.AccountID, BindingID: b.ID, Action: in.Action, Reason: in.Reason, ActorID: actor}); e != nil {
		return b, e
	}
	settings, e := settingsTx(ctx, tx)
	if e != nil {
		return b, e
	}
	if e = enqueueNoticeTx(ctx, tx, settings, b, Run{ID: uuid.NewString(), Config: p, CreatedAt: time.Now().UTC()}, "manual"); e != nil {
		return b, e
	}
	if e = tx.Commit(); e != nil {
		return b, e
	}
	s.refresh(ctx, []int64{b.AccountID})
	return b, nil
}
