package upstreamgovernance

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/lib/pq"
)

var _ PricingAdminStore = (*sqlStore)(nil)

// A preview sees one committed snapshot without taking write locks or creating
// policy rows. Saves lock the same authoritative inputs in a serializable
// transaction and repeat the exact preparation before writing editable fields.
func loadPricingAdminState(ctx context.Context, tx *sql.Tx, groupID int64, lock bool) (pricingAdminState, bool, error) {
	input := pricingAdminState{State: PricingState{Policy: PricingPolicy{LocalGroupID: groupID, Mode: PricingModeKeepMargin, Version: 1, DecreaseStabilitySeconds: 60, MaxIncreasePercent: 20}}, Bindings: []PricingBindingImpact{}}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	policy, err := scanPricingPolicy(tx.QueryRowContext(ctx, `SELECT `+pricingPolicyColumns+` FROM upstream_governance_pricing_policies WHERE local_group_id=$1`+suffix, groupID))
	exists := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return input, false, err
	}
	if exists {
		input.State.Policy = policy
	}
	if err = tx.QueryRowContext(ctx, `SELECT rate_multiplier FROM groups WHERE id=$1 AND deleted_at IS NULL`+suffix, groupID).Scan(&input.State.CurrentSale); err != nil {
		return input, false, storeError(err)
	}
	share := ""
	if lock {
		share = " FOR SHARE OF b,s"
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.site_id,s.name,b.id,b.remote_group_id,b.account_id FROM upstream_governance_bindings b JOIN upstream_governance_sites s ON s.id=b.site_id WHERE b.local_group_id=$1 OR COALESCE(b.local_group_ids,'[]'::jsonb) @> jsonb_build_array($1::bigint) ORDER BY b.id`+share, groupID)
	if err != nil {
		return input, false, err
	}
	for rows.Next() {
		var b PricingBindingImpact
		if err = rows.Scan(&b.SiteID, &b.SiteName, &b.BindingID, &b.RemoteGroupID, &b.AccountID); err != nil {
			rows.Close()
			return input, false, err
		}
		input.Bindings = append(input.Bindings, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return input, false, err
	}
	share = ""
	if lock {
		share = " FOR SHARE"
	}
	rows, err = tx.QueryContext(ctx, `SELECT source_id,COALESCE(site_id,0),COALESCE(binding_id,0),cost,unit,currency,comparable,eligible,unknown,observed_at FROM upstream_governance_pricing_cost_facts WHERE local_group_id=$1 ORDER BY source_id`+share, groupID)
	if err != nil {
		return input, false, err
	}
	defer rows.Close()
	input.State.Observations = []CostObservation{}
	for rows.Next() {
		f := CostObservation{LocalGroupID: groupID}
		if err = rows.Scan(&f.SourceID, &f.SiteID, &f.BindingID, &f.Cost, &f.Unit, &f.Currency, &f.Comparable, &f.Eligible, &f.Unknown, &f.ObservedAt); err != nil {
			return input, false, err
		}
		input.State.Observations = append(input.State.Observations, f)
	}
	return input, exists, rows.Err()
}

func pricingAdminError(err error) error {
	var pg *pq.Error
	if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01" || pg.Code == "23505") {
		return ErrConflict
	}
	return err
}

func (s *sqlStore) PreviewPricingPolicy(ctx context.Context, siteID, groupID int64, draft PricingPolicyDraft) (result PricingPolicyPreview, err error) {
	if siteID <= 0 || groupID <= 0 {
		return result, ErrInvalid
	}
	if err = draft.validate(); err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	input, _, err := loadPricingAdminState(ctx, tx, groupID, false)
	if err != nil {
		return result, err
	}
	result, _, err = preparePricingAdmin(siteID, groupID, draft, input)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func writePricingDraft(ctx context.Context, tx *sql.Tx, policy PricingPolicy, exists bool) error {
	if exists {
		result, err := tx.ExecContext(ctx, `UPDATE upstream_governance_pricing_policies SET enabled=$2,mode=$3,min_margin=$4,safety_buffer=$5,decrease_stability_seconds=$6,max_increase_percent=$7,baseline_cost=$8,baseline_sale=$9,ratio=$10,version=version+1,updated_at=NOW() WHERE local_group_id=$1 AND version=$11`, policy.LocalGroupID, policy.Enabled, policy.Mode, policy.MinMargin, policy.SafetyBuffer, policy.DecreaseStabilitySeconds, policy.MaxIncreasePercent, policy.BaselineCost, policy.BaselineSale, policy.Ratio, policy.Version)
		return affected(result, err, ErrConflict)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO upstream_governance_pricing_policies(local_group_id,enabled,mode,min_margin,safety_buffer,decrease_stability_seconds,max_increase_percent,baseline_cost,baseline_sale,ratio,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,2)`, policy.LocalGroupID, policy.Enabled, policy.Mode, policy.MinMargin, policy.SafetyBuffer, policy.DecreaseStabilitySeconds, policy.MaxIncreasePercent, policy.BaselineCost, policy.BaselineSale, policy.Ratio)
	return err
}

func (s *sqlStore) SavePricingPolicy(ctx context.Context, siteID, groupID int64, draft PricingPolicyDraft, fingerprint string) (result PricingPoliciesConfiguration, err error) {
	defer func() { err = pricingAdminError(err) }()
	if siteID <= 0 || groupID <= 0 {
		return result, ErrInvalid
	}
	if err = draft.validate(); err != nil {
		return result, err
	}
	if len(fingerprint) != 64 {
		return result, ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	input, exists, err := loadPricingAdminState(ctx, tx, groupID, true)
	if err != nil {
		return result, err
	}
	preview, policy, err := preparePricingAdmin(siteID, groupID, draft, input)
	if err != nil {
		return result, err
	}
	if subtle.ConstantTimeCompare([]byte(preview.Fingerprint), []byte(fingerprint)) != 1 {
		return result, ErrConflict
	}
	if preview.Blocked {
		return result, ErrPricingUnknownCost
	}
	if err = writePricingDraft(ctx, tx, policy, exists); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return s.ListPricingPolicies(ctx, siteID)
}

func savePricingNotificationsTx(ctx context.Context, tx *sql.Tx, siteID, version int64, notifications PricingNotificationPolicy) error {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM upstream_governance_sites WHERE id=$1 FOR UPDATE`, siteID).Scan(&id); err != nil {
		return storeError(err)
	}
	current := int64(1)
	err := tx.QueryRowContext(ctx, `SELECT version FROM upstream_governance_pricing_notifications WHERE site_id=$1 FOR UPDATE`, siteID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current != version {
		return ErrConflict
	}
	raw, err := json.Marshal(notifications)
	if err != nil {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_pricing_notifications(site_id,version,policy) VALUES($1,2,$2::jsonb) ON CONFLICT(site_id) DO UPDATE SET version=upstream_governance_pricing_notifications.version+1,policy=EXCLUDED.policy,updated_at=NOW()`, siteID, string(raw))
	return err
}

func (s *sqlStore) SavePricingNotifications(ctx context.Context, siteID, version int64, notifications PricingNotificationPolicy) (result PricingPoliciesConfiguration, err error) {
	defer func() { err = pricingAdminError(err) }()
	if siteID <= 0 || version <= 0 {
		return result, ErrInvalid
	}
	notifications.Recipients, err = normalizeBalanceRecipients(notifications.Recipients)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = savePricingNotificationsTx(ctx, tx, siteID, version, notifications); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return s.ListPricingPolicies(ctx, siteID)
}
