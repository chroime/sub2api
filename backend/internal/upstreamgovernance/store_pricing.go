package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var _ PricingPersistence = (*sqlStore)(nil)

// LoadChangeNotificationPolicy reads only the site-level delivery policy.
// Missing rows retain the safe default (disabled) while older deployments
// without migration 264 fall back in Service.EnqueueChangeNotice to the
// system administrator recipient resolver.
func (s *sqlStore) LoadChangeNotificationPolicy(ctx context.Context, siteID int64) (PricingNotificationPolicy, error) {
	policy := PricingNotificationPolicy{Recipients: []string{}, GroupChanges: true, RateChanges: true, PricingChanges: true, ProtectionChanges: true}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT policy FROM upstream_governance_pricing_notifications WHERE site_id=$1`, siteID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		policy.Enabled = false
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if err = json.Unmarshal(raw, &policy); err != nil {
		return policy, err
	}
	return policy, nil
}

var _ PricingPolicyStore = (*sqlStore)(nil)

const pricingPolicyColumns = `local_group_id,enabled,mode,baseline_cost,baseline_sale,ratio,min_margin,safety_buffer,decrease_stability_seconds,max_increase_percent,version,manual_owner,manual_version,last_automatic_sale,last_automatic_cost,active_cost,active_cost_source,protected,protection_reason,cost_fact_revision,decrease_observed_at`

func scanPricingPolicy(row interface{ Scan(...any) error }) (PricingPolicy, error) {
	var p PricingPolicy
	var mode string
	var decreaseObservedAt sql.NullTime
	err := row.Scan(&p.LocalGroupID, &p.Enabled, &mode, &p.BaselineCost, &p.BaselineSale, &p.Ratio, &p.MinMargin, &p.SafetyBuffer, &p.DecreaseStabilitySeconds, &p.MaxIncreasePercent, &p.Version, &p.ManualOwner, &p.ManualVersion, &p.LastAutomaticSale, &p.LastAutomaticCost, &p.ActiveCost, &p.ActiveCostSource, &p.Protected, &p.ProtectionReason, &p.CostFactRevision, &decreaseObservedAt)
	p.Mode = PricingMode(mode)
	if decreaseObservedAt.Valid {
		p.DecreaseObservedAt = decreaseObservedAt.Time
	}
	return p, storeError(err)
}

func (s *sqlStore) LoadPricingState(ctx context.Context, groupID int64) (PricingState, error) {
	if groupID <= 0 {
		return PricingState{}, ErrPricingInvalidGroup
	}
	var sale float64
	if err := s.db.QueryRowContext(ctx, `SELECT rate_multiplier FROM groups WHERE id=$1 AND deleted_at IS NULL`, groupID).Scan(&sale); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PricingState{}, ErrPricingGroupNotFound
		}
		return PricingState{}, err
	}
	policy := PricingPolicy{LocalGroupID: groupID, Mode: PricingModeKeepMargin, Version: 1, DecreaseStabilitySeconds: 60, MaxIncreasePercent: 20}
	if loaded, err := scanPricingPolicy(s.db.QueryRowContext(ctx, `SELECT `+pricingPolicyColumns+` FROM upstream_governance_pricing_policies WHERE local_group_id=$1`, groupID)); err == nil {
		policy = loaded
	} else if !errors.Is(err, ErrNotFound) {
		return PricingState{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_id,cost,unit,currency,comparable,eligible,unknown,observed_at FROM upstream_governance_pricing_cost_facts WHERE local_group_id=$1 ORDER BY source_id`, groupID)
	if err != nil {
		return PricingState{}, err
	}
	defer rows.Close()
	observations := []CostObservation{}
	for rows.Next() {
		var v CostObservation
		v.LocalGroupID = groupID
		if err := rows.Scan(&v.SourceID, &v.Cost, &v.Unit, &v.Currency, &v.Comparable, &v.Eligible, &v.Unknown, &v.ObservedAt); err != nil {
			return PricingState{}, err
		}
		observations = append(observations, v)
	}
	if err := rows.Err(); err != nil {
		return PricingState{}, err
	}
	return PricingState{Policy: policy, CurrentSale: sale, Observations: observations}, nil
}

func (s *sqlStore) RecordPricingCostFact(ctx context.Context, groupID int64, fact CostObservation) (int64, error) {
	if groupID <= 0 || fact.SourceID == "" {
		return 0, ErrPricingInvalidGroup
	}
	var revision int64
	_, err := s.db.ExecContext(ctx, `
INSERT INTO upstream_governance_pricing_cost_facts
 (local_group_id,source_id,site_id,binding_id,cost,unit,currency,comparable,eligible,unknown,observed_at,revision,updated_at)
 VALUES ($1,$2,NULLIF($3,0),NULLIF($4,0),$5,$6,$7,$8,$9,$10,COALESCE($11,NOW()),1,NOW())
 ON CONFLICT(local_group_id,source_id) DO UPDATE SET
  site_id=EXCLUDED.site_id,cost=EXCLUDED.cost,unit=EXCLUDED.unit,currency=EXCLUDED.currency,
  comparable=EXCLUDED.comparable,eligible=EXCLUDED.eligible,unknown=EXCLUDED.unknown,
  observed_at=EXCLUDED.observed_at,revision=upstream_governance_pricing_cost_facts.revision+1,updated_at=NOW()
	`, groupID, fact.SourceID, fact.SiteID, fact.BindingID, fact.Cost, fact.Unit, fact.Currency, fact.Comparable, fact.Eligible, fact.Unknown, fact.ObservedAt)
	if err == nil {
		err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM upstream_governance_pricing_cost_facts WHERE local_group_id=$1`, groupID).Scan(&revision)
	}
	return revision, err
}

// CommitPricing updates the group's sale multiplier and policy state under one
// row lock. A stale policy version or sale value yields ErrPricingVersionConflict.
func (s *sqlStore) CommitPricing(ctx context.Context, commit PricingCommit) error {
	if commit.LocalGroupID <= 0 || commit.OperationID == "" {
		return ErrPricingInvalidGroup
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	var sale float64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM upstream_governance_pricing_policies WHERE local_group_id=$1 FOR UPDATE`, commit.LocalGroupID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		// A policy row is created lazily by the first coordinator commit. This
		// keeps existing groups untouched while auto-pricing remains disabled.
		version = 1
		_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_pricing_policies(local_group_id,version) VALUES($1,1) ON CONFLICT(local_group_id) DO NOTHING`, commit.LocalGroupID)
		if err == nil {
			err = tx.QueryRowContext(ctx, `SELECT version FROM upstream_governance_pricing_policies WHERE local_group_id=$1 FOR UPDATE`, commit.LocalGroupID).Scan(&version)
		}
	}
	if err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT rate_multiplier FROM groups WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, commit.LocalGroupID).Scan(&sale); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPricingGroupNotFound
		}
		return err
	}
	if version != commit.ExpectedPolicyVersion || sale != commit.ExpectedSale {
		return ErrPricingVersionConflict
	}
	if commit.ApplySale {
		if _, err = tx.ExecContext(ctx, `UPDATE groups SET rate_multiplier=$2,updated_at=NOW() WHERE id=$1 AND rate_multiplier=$3`, commit.LocalGroupID, commit.Decision.TargetSale, commit.ExpectedSale); err != nil {
			return err
		}
	}
	newVersion := version + 1
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_pricing_policies SET
 active_cost=$2,active_cost_source=$3,protected=$4,protection_reason=$5,cost_fact_revision=$6,
 last_automatic_cost=CASE WHEN $7 THEN $2 ELSE last_automatic_cost END,
 last_automatic_sale=CASE WHEN $7 THEN $8 ELSE last_automatic_sale END,
 decrease_observed_at=CASE WHEN $9='decrease_stability' THEN COALESCE(decrease_observed_at,$10) ELSE NULL END,
 version=$11,updated_at=NOW()
 WHERE local_group_id=$1 AND version=$12`, commit.LocalGroupID, commit.Decision.Cost, commit.Decision.SourceID, commit.Protected, commit.Reason, commit.CostFactRevision, commit.ApplySale, commit.Decision.TargetSale, commit.Reason, commit.Now, newVersion, version); err != nil {
		return err
	}
	status := "prepared"
	if commit.ApplySale {
		status = "applied"
	} else if commit.Protected {
		status = "protected"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_pricing_operations(operation_id,local_group_id,policy_version,cost_fact_revision,source_id,before_cost,after_cost,before_sale,target_sale,status,protected,reason,idempotency_key,created_at,applied_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$1,NOW(),CASE WHEN $10='applied' THEN NOW() ELSE NULL END)
	 ON CONFLICT(operation_id) DO NOTHING`, commit.OperationID, commit.LocalGroupID, newVersion, commit.CostFactRevision, commit.Decision.SourceID, commit.PreviousCost, commit.Decision.Cost, commit.ExpectedSale, commit.Decision.TargetSale, status, commit.Protected, commit.Reason)
	if err != nil {
		return fmt.Errorf("save pricing operation: %w", err)
	}
	return tx.Commit()
}

func pricingViewFromState(groupID int64, groupName string, state PricingState) PricingPolicyView {
	view := PricingPolicyView{PricingPolicy: state.Policy, LocalGroupName: groupName, Sources: append([]CostObservation{}, state.Observations...), Status: "disabled"}
	currentCost := state.Policy.ActiveCost
	if currentCost > 0 {
		view.CurrentCost = &currentCost
	}
	currentSale := state.CurrentSale
	view.CurrentSale = &currentSale
	if state.Policy.Enabled {
		view.Status = "managed"
		if state.Policy.ManualOwner {
			view.Status = "manual"
		}
		if state.Policy.Protected {
			view.Status = "protected"
		}
		if currentCost > 0 {
			if decision, err := CalculatePricingTarget(state.Policy, currentCost); err == nil {
				view.TargetSale = &decision.TargetSale
			}
		}
	}
	return view
}

func (s *sqlStore) ListPricingPolicies(ctx context.Context, siteID int64) (PricingPoliciesConfiguration, error) {
	if siteID <= 0 {
		return PricingPoliciesConfiguration{}, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT g.id,g.name FROM groups g JOIN upstream_governance_bindings b ON b.site_id=$1 AND (b.local_group_id=g.id OR COALESCE(b.local_group_ids,'[]'::jsonb) @> jsonb_build_array(g.id)) WHERE g.deleted_at IS NULL ORDER BY g.id`, siteID)
	if err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	defer rows.Close()
	result := PricingPoliciesConfiguration{Version: 1, Policies: []PricingPolicyView{}, Notifications: PricingNotificationPolicy{Recipients: []string{}, GroupChanges: true, RateChanges: true, PricingChanges: true, ProtectionChanges: true}}
	if notificationRows, queryErr := s.db.QueryContext(ctx, `SELECT version,policy FROM upstream_governance_pricing_notifications WHERE site_id=$1`, siteID); queryErr == nil {
		defer notificationRows.Close()
		var raw []byte
		if notificationRows.Next() {
			if scanErr := notificationRows.Scan(&result.Version, &raw); scanErr == nil {
				_ = json.Unmarshal(raw, &result.Notifications)
			}
		}
	}
	for rows.Next() {
		var groupID int64
		var groupName string
		if err := rows.Scan(&groupID, &groupName); err != nil {
			return PricingPoliciesConfiguration{}, err
		}
		state, err := s.LoadPricingState(ctx, groupID)
		if err != nil {
			return PricingPoliciesConfiguration{}, err
		}
		result.Policies = append(result.Policies, pricingViewFromState(groupID, groupName, state))
	}
	if err := rows.Err(); err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	return result, nil
}

// SavePricingPolicies retains the legacy batch endpoint, but uses only the
// editable draft fields. Even old clients cannot replace baseline/runtime or
// ownership facts, write an unrelated group, or bypass version checks.
func (s *sqlStore) SavePricingPolicies(ctx context.Context, siteID int64, input PricingPoliciesConfiguration) (result PricingPoliciesConfiguration, err error) {
	defer func() { err = pricingAdminError(err) }()
	if siteID <= 0 || input.Version <= 0 {
		return result, ErrInvalid
	}
	recipients, err := normalizeBalanceRecipients(input.Notifications.Recipients)
	if err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	input.Notifications.Recipients = recipients
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	defer tx.Rollback()
	views := append([]PricingPolicyView(nil), input.Policies...)
	sort.Slice(views, func(i, j int) bool { return views[i].LocalGroupID < views[j].LocalGroupID })
	for i, view := range views {
		if view.LocalGroupID <= 0 || i > 0 && views[i-1].LocalGroupID == view.LocalGroupID {
			return result, ErrInvalid
		}
		state, exists, loadErr := loadPricingAdminState(ctx, tx, view.LocalGroupID, true)
		if loadErr != nil {
			return result, loadErr
		}
		preview, policy, prepareErr := preparePricingAdmin(siteID, view.LocalGroupID, pricingDraftFromPolicy(view.PricingPolicy), state)
		if prepareErr != nil {
			return result, prepareErr
		}
		if view.Version != state.State.Policy.Version {
			return result, ErrConflict
		}
		if preview.Blocked {
			return result, ErrPricingUnknownCost
		}
		if err = writePricingDraft(ctx, tx, policy, exists); err != nil {
			return result, err
		}
	}
	if err = savePricingNotificationsTx(ctx, tx, siteID, input.Version, input.Notifications); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return PricingPoliciesConfiguration{}, err
	}
	return s.ListPricingPolicies(ctx, siteID)
}

// Keep time imported in older generated builds where the compiler otherwise
// reports it as unused after SQL driver substitutions.
var _ = time.Time{}
