package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func (s *sqlStore) GetAutomation(ctx context.Context, site int64) (AutomationConfig, error) {
	value := DefaultAutomationConfig()
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT version,policy FROM upstream_governance_automation WHERE site_id=$1`, site).Scan(&value.Version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(raw, &value.Policy)
	return value, err
}
func (s *sqlStore) SaveAutomation(ctx context.Context, site int64, value AutomationConfig) (AutomationConfig, error) {
	raw, err := json.Marshal(value.Policy)
	if err != nil {
		return value, err
	}
	if value.Version == 0 {
		err = s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_automation(site_id,policy) VALUES($1,$2::jsonb) ON CONFLICT(site_id) DO NOTHING RETURNING version`, site, string(raw)).Scan(&value.Version)
	} else {
		err = s.db.QueryRowContext(ctx, `UPDATE upstream_governance_automation SET policy=$2::jsonb,version=version+1,updated_at=NOW() WHERE site_id=$1 AND version=$3 RETURNING version`, site, string(raw), value.Version).Scan(&value.Version)
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConflict
	}
	return value, err
}
func (s *sqlStore) ReconciliationStates(ctx context.Context, site int64) (map[int64]ReconciliationState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM upstream_governance_reconciliation_state WHERE site_id=$1`, site)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int64]ReconciliationState{}
	for rows.Next() {
		var raw []byte
		var v ReconciliationState
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		result[v.BindingID] = v
	}
	return result, rows.Err()
}
func (s *sqlStore) SaveReconciliationState(ctx context.Context, site int64, v ReconciliationState) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO upstream_governance_reconciliation_state(site_id,binding_id,payload) VALUES($1,$2,$3::jsonb) ON CONFLICT(binding_id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW() WHERE upstream_governance_reconciliation_state.site_id=EXCLUDED.site_id`, site, v.BindingID, string(raw))
	return err
}

type persistedReconciliationPreview struct {
	Public        ReconciliationPreview      `json:"public"`
	PolicyVersion int64                      `json:"policy_version"`
	Items         []ReconciliationFrozenItem `json:"items"`
}

func (s *sqlStore) SaveReconciliationPreview(ctx context.Context, site int64, p *ReconciliationPreview) error {
	raw, err := json.Marshal(persistedReconciliationPreview{Public: *p, PolicyVersion: p.PolicyVersion, Items: p.Items})
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_reconcile_previews(id,site_id,payload,expires_at) VALUES($1,$2,$3::jsonb,$4)`, p.ID, site, string(raw), p.ExpiresAt); err != nil {
		return err
	}
	// Keep the latest 100 plans plus at most one recovery source per live bound
	// account. A receipt can outlive the preview's application deadline.
	if _, err = tx.ExecContext(ctx, `DELETE FROM upstream_governance_reconcile_previews p WHERE p.site_id=$1 AND p.id NOT IN (SELECT id FROM upstream_governance_reconcile_previews WHERE site_id=$1 ORDER BY expires_at DESC,id DESC LIMIT 100) AND NOT EXISTS (SELECT 1 FROM upstream_governance_bindings b JOIN accounts a ON a.id=b.account_id AND a.deleted_at IS NULL WHERE b.site_id=p.site_id AND a.extra->>'upstream_governance_marker'=b.marker AND a.extra->>'upstream_governance_reconcile_receipt'=p.id || ':' || b.id::text)`, site); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *sqlStore) GetReconciliationPreview(ctx context.Context, site int64, id string) (*ReconciliationPreview, error) {
	var raw, result []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload,result FROM upstream_governance_reconcile_previews WHERE site_id=$1 AND id=$2`, site, id).Scan(&raw, &result)
	if err != nil {
		return nil, storeError(err)
	}
	var persisted persistedReconciliationPreview
	if err = json.Unmarshal(raw, &persisted); err != nil {
		return nil, err
	}
	p := &persisted.Public
	p.PolicyVersion = persisted.PolicyVersion
	p.Items = persisted.Items
	if len(result) > 0 {
		if err = json.Unmarshal(result, &p.Result); err != nil {
			return nil, err
		}
	}
	return p, nil
}
func (s *sqlStore) SaveReconciliationResult(ctx context.Context, site int64, id string, result *ReconciliationResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	r, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_reconcile_previews SET result=$3::jsonb WHERE site_id=$1 AND id=$2`, site, id, string(raw))
	return affected(r, err, ErrNotFound)
}
