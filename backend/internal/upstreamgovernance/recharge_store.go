package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func (s *sqlStore) GetRechargeRecord(ctx context.Context, siteID int64) (*RechargeRecord, error) {
	record := &RechargeRecord{SiteID: siteID}
	var policy, state []byte
	err := s.db.QueryRowContext(ctx, `SELECT version,policy,state,updated_at FROM upstream_governance_recharge_plans WHERE site_id=$1`, siteID).Scan(&record.Version, &policy, &state, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(policy, &record.Policy); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(state, &record.State); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *sqlStore) SaveRechargePolicy(ctx context.Context, record *RechargeRecord, version int64) error {
	policy, err := json.Marshal(record.Policy)
	if err != nil {
		return err
	}
	state, err := json.Marshal(record.State)
	if err != nil {
		return err
	}
	if version == 0 {
		err = s.db.QueryRowContext(ctx, `INSERT INTO upstream_governance_recharge_plans(site_id,policy,state) VALUES($1,$2::jsonb,$3::jsonb) ON CONFLICT(site_id) DO NOTHING RETURNING version,updated_at`, record.SiteID, string(policy), string(state)).Scan(&record.Version, &record.UpdatedAt)
	} else {
		err = s.db.QueryRowContext(ctx, `UPDATE upstream_governance_recharge_plans SET policy=$2::jsonb,state=$3::jsonb,version=version+1,updated_at=NOW() WHERE site_id=$1 AND version=$4 RETURNING version,updated_at`, record.SiteID, string(policy), string(state), version).Scan(&record.Version, &record.UpdatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	return err
}

func (s *sqlStore) SaveRechargeEvaluation(ctx context.Context, siteID, version int64, state RechargeState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_recharge_plans SET state=$3::jsonb,updated_at=NOW() WHERE site_id=$1 AND version=$2`, siteID, version, string(raw))
	return affected(result, err, ErrConflict)
}
