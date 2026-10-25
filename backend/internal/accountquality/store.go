package accountquality

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"strconv"
	"time"
)

const SettingsKey = "account_quality_monitor_settings"

type Store struct {
	DB           *sql.DB
	Refresh      func(context.Context, int64)
	Capabilities []Capability
	ResolveModel func(context.Context, int64, string) (string, error)
}

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }
func encode(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return string(b)
}
func storeErr(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	return e
}

type scanner interface{ Scan(...any) error }

func scanPolicy(s scanner) (Policy, error) {
	var p Policy
	var raw []byte
	err := s.Scan(&p.ID, &p.Version, &p.Enabled, &raw, &p.CreatedAt, &p.UpdatedAt)
	if err == nil {
		var v Policy
		err = json.Unmarshal(raw, &v)
		v.ID, v.Version, v.Enabled, v.CreatedAt, v.UpdatedAt = p.ID, p.Version, p.Enabled, p.CreatedAt, p.UpdatedAt
		p = v
	}
	return p, storeErr(err)
}

const policyColumns = "id,version,enabled,config,created_at,updated_at"

func (s *Store) Policies(ctx context.Context) ([]Policy, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT "+policyColumns+" FROM account_quality_policies WHERE deleted_at IS NULL ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		p, e := scanPolicy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Policy(ctx context.Context, id int64) (Policy, error) {
	return scanPolicy(s.DB.QueryRowContext(ctx, "SELECT "+policyColumns+" FROM account_quality_policies WHERE id=$1 AND deleted_at IS NULL", id))
}
func loadPolicy(ctx context.Context, tx *sql.Tx, id int64) (Policy, error) {
	return scanPolicy(tx.QueryRowContext(ctx, "SELECT "+policyColumns+" FROM account_quality_policies WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id))
}
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var raw string
	e := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=$1", SettingsKey).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return DefaultSettings(), nil
	}
	if e != nil {
		return Settings{}, e
	}
	v := DefaultSettings()
	e = json.Unmarshal([]byte(raw), &v)
	return v, e
}
func settingsTx(ctx context.Context, tx *sql.Tx) (Settings, error) {
	var raw string
	e := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=$1 FOR UPDATE", SettingsKey).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return DefaultSettings(), nil
	}
	if e != nil {
		return Settings{}, e
	}
	v := DefaultSettings()
	e = json.Unmarshal([]byte(raw), &v)
	return v, e
}
func (s *Store) SaveSettings(ctx context.Context, v Settings, actor int64) (Settings, error) {
	if e := ValidateSettings(v); e != nil {
		return v, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return v, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(61824001)"); e != nil {
		return v, e
	}
	old, e := settingsTx(ctx, tx)
	if e != nil {
		return v, e
	}
	if old.Version != v.Version {
		return v, ErrConflict
	}
	v.Version++
	_, e = tx.ExecContext(ctx, "INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()", SettingsKey, encode(v))
	if e == nil {
		e = actionTx(ctx, tx, Action{Action: "settings_updated", Reason: "账号质量全局配置更新", ActorID: actor})
	}
	if e != nil {
		return v, e
	}
	return v, tx.Commit()
}
func actionTx(ctx context.Context, tx *sql.Tx, a Action) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO account_quality_actions(id,account_id,binding_id,action,reason,actor_id) VALUES($1,$2,$3,$4,$5,$6)", uuid.NewString(), a.AccountID, a.BindingID, a.Action, a.Reason, a.ActorID)
	return e
}
func (s *Store) refresh(ctx context.Context, ids []int64) {
	if s.Refresh != nil {
		for _, id := range ids {
			s.Refresh(ctx, id)
		}
	}
}
func (s *Store) eligible(platform, typ, mode string) bool {
	for _, c := range s.Capabilities {
		if c.Platform == platform && c.APIMode == mode && c.AutomaticControl {
			for _, t := range c.AccountTypes {
				if t == typ {
					return true
				}
			}
		}
	}
	return false
}
func scanBinding(row scanner) (Binding, error) {
	var b Binding
	var raw []byte
	e := row.Scan(&b.ID, &b.Version, &b.AccountID, &b.PolicyID, &raw, &b.NextRunAt, &b.AccountName, &b.Platform, &b.AccountType)
	if e == nil {
		var v Binding
		e = json.Unmarshal(raw, &v)
		v.ID, v.Version, v.AccountID, v.PolicyID, v.NextRunAt, v.AccountName, v.Platform, v.AccountType = b.ID, b.Version, b.AccountID, b.PolicyID, b.NextRunAt, b.AccountName, b.Platform, b.AccountType
		b = v
		b.RecentRuns = []Run{}
	}
	return b, storeErr(e)
}

const bindingColumns = "b.id,b.version,b.account_id,b.policy_id,b.state,b.next_run_at,a.name,a.platform,a.type"

func (s *Store) Bindings(ctx context.Context, account int64) ([]Binding, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT "+bindingColumns+" FROM account_quality_bindings b JOIN accounts a ON a.id=b.account_id JOIN account_quality_policies p ON p.id=b.policy_id WHERE a.deleted_at IS NULL AND p.deleted_at IS NULL AND ($1::bigint=0 OR b.account_id=$1) ORDER BY b.id", account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		b, e := scanBinding(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func loadBinding(ctx context.Context, tx *sql.Tx, id int64) (Binding, error) {
	return scanBinding(tx.QueryRowContext(ctx, "SELECT "+bindingColumns+" FROM account_quality_bindings b JOIN accounts a ON a.id=b.account_id WHERE b.id=$1 AND a.deleted_at IS NULL FOR UPDATE OF b,a", id))
}
func saveBinding(ctx context.Context, tx *sql.Tx, b Binding) error {
	_, e := tx.ExecContext(ctx, "UPDATE account_quality_bindings SET version=$2,state=$3::jsonb,next_run_at=$4 WHERE id=$1", b.ID, b.Version, encode(b), b.NextRunAt)
	return e
}
func gateTx(ctx context.Context, tx *sql.Tx, b Binding, p Policy) error {
	if b.Isolated {
		gate := map[string]any{"policy_id": p.ID, "policy_version": p.Version, "scope": EffectivePolicy(p, b.Overrides).IsolationScope, "model": b.Model, "blocked": true, "reason": b.Reason, "override_until": b.OverrideUntil}
		_, e := tx.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{account_quality_gates}',COALESCE(extra->'account_quality_gates','{}'::jsonb)||jsonb_build_object($2::text,$3::jsonb)),updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, b.AccountID, strconv.FormatInt(b.ID, 10), encode(gate))
		if e != nil {
			return e
		}
	} else {
		if _, e := tx.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{account_quality_gates}',COALESCE(extra->'account_quality_gates','{}'::jsonb)-$2::text),updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, b.AccountID, strconv.FormatInt(b.ID, 10)); e != nil {
			return e
		}
	}
	_, e := tx.ExecContext(ctx, "INSERT INTO scheduler_outbox(event_type,account_id,payload) VALUES('account_changed',$1,$2::jsonb)", b.AccountID, `{"source":"account_quality"}`)
	return e
}

func (s *Store) SavePolicy(ctx context.Context, p Policy, actor int64) (Policy, error) {
	if e := ValidatePolicy(p); e != nil {
		return p, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	ids := []int64{}
	if p.ID == 0 {
		p.Version = 1
		e = tx.QueryRowContext(ctx, "INSERT INTO account_quality_policies(version,enabled,config) VALUES(1,$1,$2::jsonb) RETURNING id,created_at,updated_at", p.Enabled, encode(p)).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	} else {
		old, err := loadPolicy(ctx, tx, p.ID)
		if err != nil {
			return p, err
		}
		if old.Version != p.Version {
			return p, ErrConflict
		}
		p.Version++
		p.CreatedAt = old.CreatedAt
		p.UpdatedAt = time.Now().UTC()
		_, e = tx.ExecContext(ctx, "UPDATE account_quality_policies SET version=$2,enabled=$3,config=$4::jsonb,updated_at=NOW() WHERE id=$1", p.ID, p.Version, p.Enabled, encode(p))
		if e == nil {
			ids, e = resetPolicyBindings(ctx, tx, p)
		}
	}
	if e != nil {
		return p, e
	}
	if e = actionTx(ctx, tx, Action{Action: "policy_saved", Reason: fmt.Sprintf("策略 %s，版本 %d", p.Name, p.Version), ActorID: actor}); e != nil {
		return p, e
	}
	if e = tx.Commit(); e != nil {
		return p, e
	}
	s.refresh(ctx, ids)
	return p, nil
}
func resetPolicyBindings(ctx context.Context, tx *sql.Tx, p Policy) ([]int64, error) {
	rows, e := tx.QueryContext(ctx, "SELECT "+bindingColumns+" FROM account_quality_bindings b JOIN accounts a ON a.id=b.account_id WHERE b.policy_id=$1 AND a.deleted_at IS NULL ORDER BY a.id,b.id FOR UPDATE OF b,a", p.ID)
	if e != nil {
		return nil, e
	}
	bindings := []Binding{}
	for rows.Next() {
		b, e := scanBinding(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		bindings = append(bindings, b)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for _, b := range bindings {
		b.Version++
		b.PolicyVersion = p.Version
		b.PolicyName = p.Name
		b.Model = p.Model
		b.Failures = 0
		b.RecoveryPasses = 0
		b.Isolated = false
		b.OverrideUntil = nil
		b.State = "untested"
		b.IdentityVersion = ""
		b.NextRunAt = time.Now().UTC().Add(time.Duration(b.AccountID%30) * time.Second)
		if e = saveBinding(ctx, tx, b); e != nil {
			return nil, e
		}
		if e = gateTx(ctx, tx, b, p); e != nil {
			return nil, e
		}
		ids = append(ids, b.AccountID)
	}
	return ids, nil
}
func (s *Store) DeletePolicy(ctx context.Context, id, version, actor int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	p, e := loadPolicy(ctx, tx, id)
	if e != nil {
		return e
	}
	if p.Version != version {
		return ErrConflict
	}
	p.Version++
	p.Enabled = false
	ids, e := resetPolicyBindings(ctx, tx, p)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "UPDATE account_quality_policies SET deleted_at=NOW(),enabled=FALSE,version=$2 WHERE id=$1", id, p.Version)
	if e != nil {
		return e
	}
	if e = actionTx(ctx, tx, Action{Action: "policy_deleted", Reason: "停止策略并解除其拥有的质量门：" + p.Name, ActorID: actor}); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.refresh(ctx, ids)
	return nil
}

// BindingSummaries reads only the selected visible rows, keeping account table
// refresh independent of its normal ETag and avoiding one request per account.
func (s *Store) BindingSummaries(ctx context.Context, ids []int64) ([]Binding, error) {
	if len(ids) == 0 {
		return []Binding{}, nil
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT "+bindingColumns+" FROM account_quality_bindings b JOIN accounts a ON a.id=b.account_id JOIN account_quality_policies p ON p.id=b.policy_id WHERE b.account_id=ANY($1::bigint[]) AND a.deleted_at IS NULL AND p.deleted_at IS NULL ORDER BY b.id", pq.Array(ids))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		b, e := scanBinding(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) RecentSummaries(ctx context.Context, ids []int64) (map[int64][]Run, error) {
	out := map[int64][]Run{}
	if len(ids) == 0 {
		return out, nil
	}
	// PostgreSQL RIGHT counts Unicode characters. Project only the output suffix
	// before scanning, preserving full immutable evidence for the detail endpoint.
	rows, e := s.DB.QueryContext(ctx, `SELECT
		(evidence || jsonb_build_object('result_preview', RIGHT(COALESCE(evidence->>'response_text',''),240)))
		-'prompt'-'expected'-'response_text'-'config'-'identity_version',execution_status,lease_token
		FROM (
			SELECT binding_id,evidence,execution_status,lease_token,
			ROW_NUMBER() OVER(PARTITION BY binding_id ORDER BY created_at DESC,id DESC) n
			FROM account_quality_runs WHERE binding_id=ANY($1::bigint[])
		) recent WHERE n<=30 ORDER BY binding_id,n DESC`, pq.Array(ids))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		r, e := readRun(rows)
		if e != nil {
			return nil, e
		}
		out[r.BindingID] = append(out[r.BindingID], r)
	}
	return out, rows.Err()
}
