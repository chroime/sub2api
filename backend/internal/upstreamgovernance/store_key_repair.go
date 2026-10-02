package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const keyRepairColumns = `id,site_id,managed_key_id,binding_id,account_id,account_name,site_version,owner_user_id,marker,remote_group_id,platform,base_url,old_remote_key_id,old_key_cipher,old_creation_plan,expected_account_fingerprint,expected_account_identity,plan,idempotency_key,candidate_remote_key_id,candidate_key_cipher,stage,error_code,post_intent_at,created_at,updated_at,mode`

func scanKeyRepair(row rowScanner) (*KeyRepair, error) {
	var repair KeyRepair
	var oldPlanJSON, planJSON []byte
	var bindingID sql.NullInt64
	err := row.Scan(&repair.ID, &repair.SiteID, &repair.ManagedKeyID, &bindingID, &repair.AccountID, &repair.AccountName, &repair.SiteVersion, &repair.OwnerUserID, &repair.Marker, &repair.RemoteGroupID, &repair.Platform, &repair.BaseURL, &repair.OldRemoteKeyID, &repair.OldKeyCipher, &oldPlanJSON, &repair.ExpectedAccountFingerprint, &repair.ExpectedAccountIdentity, &planJSON, &repair.IdempotencyKey, &repair.CandidateRemoteKeyID, &repair.CandidateKeyCipher, &repair.Stage, &repair.ErrorCode, &repair.PostIntentAt, &repair.CreatedAt, &repair.UpdatedAt, &repair.Mode)
	if err != nil {
		return nil, storeError(err)
	}
	repair.BindingID = bindingID.Int64
	if !validRepairTarget(repair) {
		return nil, ErrInvalid
	}
	if len(oldPlanJSON) > 0 {
		if err := json.Unmarshal(oldPlanJSON, &repair.OldCreationPlan); err != nil || repair.OldCreationPlan == nil || validateStoredKeyCreationPlan(repair.OldCreationPlan) != nil {
			return nil, ErrInvalid
		}
	}
	if err := json.Unmarshal(planJSON, &repair.Plan); err != nil || validateRepairPlan(repair.Plan) != nil {
		return nil, ErrInvalid
	}
	return repairCapabilities(&repair), nil
}

func validateRepairPlan(plan KeyCreationPlan) error {
	if plan.ExistingIDs == nil {
		return ErrInvalid
	}
	return validateStoredKeyCreationPlan(&plan)
}

func (s *sqlStore) ReserveKeyRepair(ctx context.Context, repair *KeyRepair) error {
	if repair == nil || repair.ID == "" || repair.SiteID <= 0 || repair.ManagedKeyID <= 0 || !validRepairTarget(*repair) || repair.SiteVersion <= 0 || repair.OwnerUserID <= 0 || repair.Marker == "" || repair.RemoteGroupID == "" || repair.Platform == "" || repair.BaseURL == "" || repair.OldRemoteKeyID == "" || repair.OldKeyCipher == "" || repair.IdempotencyKey == "" || repair.Stage != KeyRepairPrepared || repair.CandidateRemoteKeyID != "" || repair.CandidateKeyCipher != "" || repair.PostIntentAt != nil || validateRepairPlan(repair.Plan) != nil {
		return ErrInvalid
	}
	planJSON, err := json.Marshal(repair.Plan)
	if err != nil {
		return ErrInvalid
	}
	var oldPlanJSON any
	if repair.OldCreationPlan != nil {
		if err := validateStoredKeyCreationPlan(repair.OldCreationPlan); err != nil {
			return err
		}
		raw, err := json.Marshal(repair.OldCreationPlan)
		if err != nil {
			return ErrInvalid
		}
		oldPlanJSON = string(raw)
	}
	repair.Mode = normalizedRepairMode(repair.Mode)
	r, err := s.db.ExecContext(ctx, `INSERT INTO upstream_governance_key_repairs (`+keyRepairColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16,$17,$18::jsonb,$19,$20,$21,$22,$23,$24,$25,$26,$27) ON CONFLICT DO NOTHING`, repair.ID, repair.SiteID, repair.ManagedKeyID, nullableRepairBindingID(repair.BindingID), repair.AccountID, repair.AccountName, repair.SiteVersion, repair.OwnerUserID, repair.Marker, repair.RemoteGroupID, repair.Platform, repair.BaseURL, repair.OldRemoteKeyID, repair.OldKeyCipher, oldPlanJSON, repair.ExpectedAccountFingerprint, repair.ExpectedAccountIdentity, string(planJSON), repair.IdempotencyKey, repair.CandidateRemoteKeyID, repair.CandidateKeyCipher, repair.Stage, repair.ErrorCode, repair.PostIntentAt, repair.CreatedAt, repair.UpdatedAt, repair.Mode)
	return affected(r, err, ErrConflict)
}

func (s *sqlStore) GetKeyRepair(ctx context.Context, siteID, keyID int64, repairID string) (*KeyRepair, error) {
	return scanKeyRepair(s.db.QueryRowContext(ctx, `SELECT `+keyRepairColumns+` FROM upstream_governance_key_repairs WHERE site_id=$1 AND managed_key_id=$2 AND id=$3`, siteID, keyID, repairID))
}

func (s *sqlStore) LatestKeyRepair(ctx context.Context, siteID, keyID int64) (*KeyRepair, error) {
	repair, err := scanKeyRepair(s.db.QueryRowContext(ctx, `SELECT `+keyRepairColumns+` FROM upstream_governance_key_repairs WHERE site_id=$1 AND managed_key_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, siteID, keyID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return repair, err
}

func (s *sqlStore) SaveKeyRepairProgress(ctx context.Context, repair *KeyRepair, previousStage string) error {
	if repair == nil || repair.ID == "" || repair.SiteID <= 0 || repair.ManagedKeyID <= 0 || !validRepairTarget(*repair) || repair.OwnerUserID <= 0 || repair.Marker == "" || repair.OldRemoteKeyID == "" || repair.OldKeyCipher == "" || !validRepairTransition(previousStage, repair.Stage) || len(repair.ErrorCode) > 64 {
		return ErrInvalid
	}
	if repair.Stage != KeyRepairPrepared && repair.Stage != KeyRepairConflict && repair.PostIntentAt == nil {
		return ErrInvalid
	}
	if repair.Stage == KeyRepairCandidateReady && (repair.CandidateRemoteKeyID == "" || repair.CandidateKeyCipher == "" || repair.CandidateRemoteKeyID == repair.OldRemoteKeyID) {
		return ErrInvalid
	}
	if repair.Stage == KeyRepairAbandoned && (repair.PostIntentAt == nil || repair.CandidateRemoteKeyID != "" || repair.CandidateKeyCipher != "") {
		return ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE upstream_governance_key_repairs SET stage=$1,error_code=$2,post_intent_at=$3,candidate_remote_key_id=$4,candidate_key_cipher=$5,updated_at=$6
WHERE id=$7 AND site_id=$8 AND managed_key_id=$9 AND binding_id IS NOT DISTINCT FROM $10 AND account_id=$11 AND site_version=$12 AND owner_user_id=$13 AND marker=$14 AND remote_group_id=$15 AND platform=$16 AND base_url=$17 AND old_remote_key_id=$18 AND old_key_cipher=$19 AND stage=$20 AND mode=$21`, repair.Stage, repair.ErrorCode, repair.PostIntentAt, repair.CandidateRemoteKeyID, repair.CandidateKeyCipher, repair.UpdatedAt, repair.ID, repair.SiteID, repair.ManagedKeyID, nullableRepairBindingID(repair.BindingID), repair.AccountID, repair.SiteVersion, repair.OwnerUserID, repair.Marker, repair.RemoteGroupID, repair.Platform, repair.BaseURL, repair.OldRemoteKeyID, repair.OldKeyCipher, previousStage, normalizedRepairMode(repair.Mode))
	return affected(r, err, ErrConflict)
}

func validRepairTarget(repair KeyRepair) bool {
	switch normalizedRepairMode(repair.Mode) {
	case KeyRepairModeAccount:
		return repair.BindingID > 0 && repair.AccountID > 0 && repair.ExpectedAccountFingerprint != "" && repair.ExpectedAccountIdentity != ""
	case KeyRepairModeKeyOnly:
		return repair.BindingID >= 0 && repair.AccountID >= 0 && (repair.BindingID > 0 || repair.AccountID == 0) && repair.ExpectedAccountFingerprint == "" && repair.ExpectedAccountIdentity == ""
	default:
		return false
	}
}

func nullableRepairBindingID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func validRepairTransition(previous, next string) bool {
	switch previous {
	case KeyRepairPrepared:
		return next == KeyRepairPostIntent || next == KeyRepairConflict
	case KeyRepairPostIntent:
		return next == KeyRepairAwaitingVisibility || next == KeyRepairCandidateReady || next == KeyRepairConflict || next == KeyRepairAbandoned
	case KeyRepairAwaitingVisibility:
		return next == KeyRepairAwaitingVisibility || next == KeyRepairCandidateReady || next == KeyRepairConflict || next == KeyRepairAbandoned
	case KeyRepairCandidateReady:
		return next == KeyRepairCandidateReady || next == KeyRepairConflict
	case KeyRepairConflict:
		return next == KeyRepairAbandoned
	default:
		return false
	}
}

var _ KeyRepairStore = (*sqlStore)(nil)
