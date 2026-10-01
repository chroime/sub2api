package upstreamgovernance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var _ PricingPersistence = (*sqlStore)(nil)

const pricingPolicyColumns = `local_group_id,enabled,mode,baseline_cost,baseline_sale,ratio,min_margin,safety_buffer,decrease_stability_seconds,max_increase_percent,version,manual_owner,manual_version,last_automatic_sale,last_automatic_cost,active_cost,active_cost_source,protected,protection_reason,cost_fact_revision,decrease_observed_at`

func scanPricingPolicy(row interface{ Scan(...any) error }) (PricingPolicy, error) {
	var p PricingPolicy
	var mode string
	err := row.Scan(&p.LocalGroupID, &p.Enabled, &mode, &p.BaselineCost, &p.BaselineSale, &p.Ratio, &p.MinMargin, &p.SafetyBuffer, &p.DecreaseStabilitySeconds, &p.MaxIncreasePercent, &p.Version, &p.ManualOwner, &p.ManualVersion, &p.LastAutomaticSale, &p.LastAutomaticCost, &p.ActiveCost, &p.ActiveCostSource, &p.Protected, &p.ProtectionReason, &p.CostFactRevision, &p.DecreaseObservedAt)
	p.Mode = PricingMode(mode)
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
	policy := PricingPolicy{LocalGroupID: groupID, Mode: PricingModeKeepMargin, Version: 1}
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
	decreaseAt := any(nil)
	if commit.Reason == "decrease_stability" {
		decreaseAt = commit.Now
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_pricing_policies SET
 active_cost=$2,active_cost_source=$3,protected=$4,protection_reason=$5,cost_fact_revision=$6,
 last_automatic_cost=CASE WHEN $7 THEN $2 ELSE last_automatic_cost END,
 last_automatic_sale=CASE WHEN $7 THEN $8 ELSE last_automatic_sale END,
 decrease_observed_at=$9,version=$10,updated_at=NOW()
 WHERE local_group_id=$1 AND version=$11`, commit.LocalGroupID, commit.Decision.Cost, commit.Decision.SourceID, commit.Protected, commit.Reason, commit.CostFactRevision, commit.ApplySale, commit.Decision.TargetSale, decreaseAt, newVersion, version); err != nil {
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

// Keep time imported in older generated builds where the compiler otherwise
// reports it as unused after SQL driver substitutions.
var _ = time.Time{}
