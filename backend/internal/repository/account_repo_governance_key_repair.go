package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

// CommitKeyRepair promotes a remotely verified candidate without exposing a
// partially changed account, binding, or managed key to routing.
func (r *accountRepository) CommitKeyRepair(ctx context.Context, request gov.KeyRepairCommitRequest) error {
	p := request.Repair
	if p.ID == "" || p.SiteID <= 0 || p.ManagedKeyID <= 0 || p.BindingID <= 0 || p.AccountID <= 0 || p.SiteVersion <= 0 ||
		p.OwnerUserID <= 0 || p.Marker == "" || p.RemoteGroupID == "" || p.Platform == "" || p.BaseURL == "" ||
		p.OldRemoteKeyID == "" || p.OldKeyCipher == "" || p.ExpectedAccountFingerprint == "" || p.ExpectedAccountIdentity == "" ||
		p.Plan.Name == "" || p.Plan.ExistingIDs == nil || p.IdempotencyKey == "" || p.CandidateRemoteKeyID == "" || p.CandidateKeyCipher == "" ||
		p.Stage != gov.KeyRepairCandidateReady || request.OldKey.ID != p.OldRemoteKeyID || request.OldKey.Key == "" ||
		request.CandidateKey.ID != p.CandidateRemoteKeyID || request.CandidateKey.Key == "" ||
		request.CandidateKey.ID == request.OldKey.ID || p.CandidateKeyCipher == p.OldKeyCipher || request.CandidateVerifiedAt.IsZero() ||
		gov.ManagedAccountIdentity(p.AccountID, p.Marker, p.Platform, p.BaseURL, request.OldKey.Key) != p.ExpectedAccountIdentity {
		return gov.ErrInvalid
	}
	planJSON, err := json.Marshal(p.Plan)
	if err != nil {
		return gov.ErrInvalid
	}
	var oldPlanJSON any
	if p.OldCreationPlan != nil {
		data, err := json.Marshal(p.OldCreationPlan)
		if err != nil {
			return gov.ErrInvalid
		}
		oldPlanJSON = string(data)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	client := tx.Client()
	var stage string
	var postIntentAt time.Time
	err = scanSingleRow(ctx, client, `SELECT stage,post_intent_at FROM upstream_governance_key_repairs
WHERE id=$1 AND site_id=$2 AND managed_key_id=$3 AND binding_id=$4 AND account_id=$5 AND account_name=$6
AND site_version=$7 AND owner_user_id=$8 AND marker=$9 AND remote_group_id=$10 AND platform=$11 AND base_url=$12
AND old_remote_key_id=$13 AND old_key_cipher=$14 AND old_creation_plan IS NOT DISTINCT FROM $15::jsonb
AND expected_account_fingerprint=$16 AND expected_account_identity=$17 AND plan=$18::jsonb AND idempotency_key=$19
AND candidate_remote_key_id=$20 AND candidate_key_cipher=$21 AND stage IN ('candidate_ready','committed') FOR UPDATE`,
		[]any{p.ID, p.SiteID, p.ManagedKeyID, p.BindingID, p.AccountID, p.AccountName, p.SiteVersion, p.OwnerUserID, p.Marker, p.RemoteGroupID, p.Platform, p.BaseURL, p.OldRemoteKeyID, p.OldKeyCipher, oldPlanJSON, p.ExpectedAccountFingerprint, p.ExpectedAccountIdentity, string(planJSON), p.IdempotencyKey, p.CandidateRemoteKeyID, p.CandidateKeyCipher}, &stage, &postIntentAt)
	if err != nil {
		return repairConflict(err)
	}
	if stage == gov.KeyRepairCommitted {
		return committedKeyRepairMatches(ctx, client, request)
	}
	if request.CandidateVerifiedAt.Before(postIntentAt) {
		return gov.ErrConflict
	}
	var siteVersion int64
	var siteBaseURL string
	var siteEnabled bool
	err = scanSingleRow(ctx, client, `SELECT version,base_url,enabled FROM upstream_governance_sites WHERE id=$1 FOR UPDATE`, []any{p.SiteID}, &siteVersion, &siteBaseURL, &siteEnabled)
	if err != nil {
		return repairConflict(err)
	}
	if !siteEnabled || siteVersion != p.SiteVersion || siteBaseURL != p.BaseURL {
		return gov.ErrConflict
	}
	if err = checkGovernanceAccountCAS(ctx, client, p.AccountID, p.ExpectedAccountFingerprint); err != nil {
		return err
	}
	entity, err := client.Account.Get(ctx, p.AccountID)
	if err != nil {
		return repairConflict(err)
	}
	account := accountEntityToService(entity)
	if account.Type != service.AccountTypeAPIKey || account.ParentAccountID != nil || account.Platform != p.Platform ||
		account.Name != p.AccountName || account.GetExtraString("upstream_governance_marker") != p.Marker ||
		account.GetCredential("api_key") != request.OldKey.Key || account.GetCredential("base_url") != p.BaseURL || account.Schedulable {
		return gov.ErrConflict
	}
	managedAccount := service.GovernanceManagedAccount(account)
	if managedAccount.Identity != p.ExpectedAccountIdentity {
		return gov.ErrConflict
	}
	if managedAccount.PauseReason == "upstream_key_missing" &&
		(managedAccount.PauseToken == "" || managedAccount.PauseMarker != p.Marker || managedAccount.PauseIdentity != managedAccount.Identity) {
		return gov.ErrConflict
	}
	var bindingID int64
	err = scanSingleRow(ctx, client, `SELECT id FROM upstream_governance_bindings
WHERE id=$1 AND site_id=$2 AND remote_group_id=$3 AND platform=$4 AND account_id=$5 AND marker=$6 AND key_cipher=$7 FOR UPDATE`,
		[]any{p.BindingID, p.SiteID, p.RemoteGroupID, p.Platform, p.AccountID, p.Marker, p.OldKeyCipher}, &bindingID)
	if err != nil {
		return repairConflict(err)
	}
	var healthJSON []byte
	err = scanSingleRow(ctx, client, `SELECT key_health FROM upstream_governance_keys
WHERE id=$1 AND site_id=$2 AND remote_group_id=$3 AND platform=$4 AND remote_key_id=$5 AND marker=$6
AND owner_user_id=$7 AND key_cipher=$8 AND creation_plan IS NOT DISTINCT FROM $9::jsonb FOR UPDATE`,
		[]any{p.ManagedKeyID, p.SiteID, p.RemoteGroupID, p.Platform, p.OldRemoteKeyID, p.Marker, p.OwnerUserID, p.OldKeyCipher, oldPlanJSON}, &healthJSON)
	if err != nil {
		return repairConflict(err)
	}
	var health gov.KeyHealth
	if json.Unmarshal(healthJSON, &health) != nil || health.MissingCount < 2 ||
		(health.Status != gov.KeyHealthConfirmedMissing && health.Status != gov.KeyHealthGroupChanged) {
		return gov.ErrConflict
	}
	credentials := copyJSONMap(account.Credentials)
	credentials["api_key"] = request.CandidateKey.Key
	extra := copyJSONMap(account.Extra)
	if managedAccount.PauseReason == "upstream_key_missing" {
		pause, ok := extra[gov.GovernancePauseExtraKey].(map[string]any)
		if !ok {
			return gov.ErrConflict
		}
		updatedPause := copyJSONMap(pause)
		updatedPause["identity"] = gov.ManagedAccountIdentity(p.AccountID, p.Marker, p.Platform, p.BaseURL, request.CandidateKey.Key)
		extra[gov.GovernancePauseExtraKey] = updatedPause
	}
	update := client.Account.UpdateOneID(p.AccountID).SetCredentials(credentials)
	if account.Notes != nil && *account.Notes == request.OldKey.Key {
		update.SetNotes(request.CandidateKey.Key)
	}
	if managedAccount.PauseReason == "upstream_key_missing" {
		update.SetExtra(extra)
	}
	if _, err = update.Save(ctx); err != nil {
		return err
	}
	if err = repairAffected(ctx, client, `UPDATE upstream_governance_bindings SET key_cipher=$1
WHERE id=$2 AND site_id=$3 AND account_id=$4 AND marker=$5 AND key_cipher=$6`,
		[]any{p.CandidateKeyCipher, p.BindingID, p.SiteID, p.AccountID, p.Marker, p.OldKeyCipher}); err != nil {
		return err
	}
	verifiedAt := request.CandidateVerifiedAt.UTC()
	present := gov.KeyHealth{Status: gov.KeyHealthPresent, LastCheckedAt: &verifiedAt, LastVerifiedAt: &verifiedAt}
	if health.Status == gov.KeyHealthConfirmedMissing {
		present.NotificationKind = "recovered"
		present.NotificationStatus = "pending"
	}
	presentJSON, err := json.Marshal(present)
	if err != nil {
		return err
	}
	if err = repairAffected(ctx, client, `UPDATE upstream_governance_keys
SET remote_key_id=$1,key_cipher=$2,creation_plan=$3::jsonb,key_health=$4::jsonb,updated_at=NOW()
WHERE id=$5 AND site_id=$6 AND remote_group_id=$7 AND platform=$8 AND remote_key_id=$9 AND marker=$10 AND owner_user_id=$11 AND key_cipher=$12`,
		[]any{p.CandidateRemoteKeyID, p.CandidateKeyCipher, string(planJSON), string(presentJSON), p.ManagedKeyID, p.SiteID, p.RemoteGroupID, p.Platform, p.OldRemoteKeyID, p.Marker, p.OwnerUserID, p.OldKeyCipher}); err != nil {
		return err
	}
	if err = repairAffected(ctx, client, `UPDATE upstream_governance_key_repairs SET stage='committed',error_code='',updated_at=NOW()
WHERE id=$1 AND stage='candidate_ready' AND candidate_remote_key_id=$2 AND candidate_key_cipher=$3`,
		[]any{p.ID, p.CandidateRemoteKeyID, p.CandidateKeyCipher}); err != nil {
		return err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &p.AccountID, nil, map[string]any{"repair_id": p.ID}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.syncSchedulerAccountSnapshot(ctx, p.AccountID)
	return nil
}

func committedKeyRepairMatches(ctx context.Context, q sqlQueryer, request gov.KeyRepairCommitRequest) error {
	p := request.Repair
	var matched bool
	err := scanSingleRow(ctx, q, `SELECT EXISTS (
SELECT 1 FROM accounts a
JOIN upstream_governance_bindings b ON b.id=$2 AND b.account_id=a.id
JOIN upstream_governance_keys k ON k.id=$3 AND k.site_id=b.site_id
WHERE a.id=$1 AND a.deleted_at IS NULL AND a.type='apikey' AND a.platform=$4
AND a.extra->>'upstream_governance_marker'=$5 AND a.credentials->>'api_key'=$6 AND a.credentials->>'base_url'=$7
AND a.schedulable=false AND b.site_id=$8 AND b.remote_group_id=$9 AND b.platform=$4 AND b.marker=$5 AND b.key_cipher=$10
AND k.remote_group_id=$9 AND k.platform=$4 AND k.marker=$5 AND k.owner_user_id=$11
AND k.remote_key_id=$12 AND k.key_cipher=$10
AND (a.extra->'upstream_governance_pause'->>'reason' IS DISTINCT FROM 'upstream_key_missing'
  OR (a.extra->'upstream_governance_pause'->>'marker'=$5 AND a.extra->'upstream_governance_pause'->>'identity'=$13)))`,
		[]any{p.AccountID, p.BindingID, p.ManagedKeyID, p.Platform, p.Marker, request.CandidateKey.Key, p.BaseURL, p.SiteID, p.RemoteGroupID, p.CandidateKeyCipher, p.OwnerUserID, p.CandidateRemoteKeyID, gov.ManagedAccountIdentity(p.AccountID, p.Marker, p.Platform, p.BaseURL, request.CandidateKey.Key)}, &matched)
	if err != nil {
		return err
	}
	if !matched {
		return gov.ErrConflict
	}
	return nil
}

func repairAffected(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args []any) error {
	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return gov.ErrConflict
	}
	return nil
}

func repairConflict(err error) error {
	if errors.Is(err, sql.ErrNoRows) || dbent.IsNotFound(err) {
		return gov.ErrConflict
	}
	return err
}

var _ gov.KeyRepairCommitter = (*accountRepository)(nil)
