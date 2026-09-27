package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

func (r *accountRepository) ReconcileGovernanceAccount(ctx context.Context, p gov.ManagedAccountPatch) (*gov.ManagedLocalAccount, error) {
	if p.Expected.ID <= 0 || p.Marker == "" || p.OperationID == "" || p.Expected.Identity == "" || p.BindingID <= 0 {
		return nil, gov.ErrInvalid
	}
	if p.Name != nil && (strings.TrimSpace(*p.Name) == "" || len(*p.Name) > 300) {
		return nil, gov.ErrInvalid
	}
	if p.Rate != nil && (math.IsNaN(*p.Rate) || math.IsInf(*p.Rate, 0) || *p.Rate < 0 || *p.Rate > 999999.9999 || math.Round(*p.Rate*10000)/10000 != *p.Rate) {
		return nil, gov.ErrInvalid
	}
	if p.RateOwner != nil && *p.RateOwner != "" && *p.RateOwner != p.Marker {
		return nil, gov.ErrInvalid
	}
	if p.Availability != "" && p.Availability != "pause" && p.Availability != "restore" {
		return nil, gov.ErrInvalid
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	client := tx.Client()
	var id int64
	if err = scanSingleRow(ctx, client, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{p.Expected.ID}, &id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = gov.ErrConflict
		}
		return nil, err
	}
	entity, err := client.Account.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	a := accountEntityToService(entity)
	current := service.GovernanceManagedAccount(a)
	if a.GetExtraString("upstream_governance_marker") != p.Marker || a.Type != service.AccountTypeAPIKey || a.ParentAccountID != nil || current.Identity != p.Expected.Identity {
		return nil, gov.ErrConflict
	}
	// A saved receipt recovers a committed account mutation after result storage
	// failed, but only while every desired field is still intact.
	if gov.ManagedPatchAlreadyApplied(current, p) {
		return &current, nil
	}
	if p.Name != nil && current.Name != p.Expected.Name {
		return nil, gov.ErrConflict
	}
	if (p.Name != nil || (p.RateOwner != nil && *p.RateOwner != "")) && current.Rate != p.Expected.Rate {
		return nil, gov.ErrConflict
	}
	if p.Rate != nil && (current.Rate != p.Expected.Rate || current.RateOwner != p.Expected.RateOwner || current.NativeRateSync != p.Expected.NativeRateSync) {
		return nil, gov.ErrConflict
	}
	if p.RateOwner != nil && (current.RateOwner != p.Expected.RateOwner || current.NativeRateSync != p.Expected.NativeRateSync) {
		return nil, gov.ErrConflict
	}
	if p.Availability != "" && (current.Status != p.Expected.Status || current.Schedulable != p.Expected.Schedulable || current.PauseToken != p.Expected.PauseToken) {
		return nil, gov.ErrConflict
	}
	if p.Availability == "pause" && (current.Status != service.StatusActive || !current.Schedulable) {
		return nil, gov.ErrConflict
	}
	if p.Availability == "restore" && (!current.CanRestore || current.Schedulable || current.PauseToken == "" || current.PauseMarker != p.Marker || current.PauseIdentity != current.Identity) {
		return nil, gov.ErrConflict
	}
	extra := copyJSONMap(normalizeJSONMap(a.Extra))
	extra[gov.GovernanceReceiptExtraKey] = p.OperationID
	update := client.Account.UpdateOneID(id)
	if p.Name != nil {
		update.SetName(*p.Name)
	}
	if p.Rate != nil {
		update.SetRateMultiplier(*p.Rate)
	}
	if p.RateOwner != nil {
		if *p.RateOwner == "" {
			delete(extra, gov.GovernanceRateOwnerExtraKey)
		} else {
			extra[gov.GovernanceRateOwnerExtraKey] = *p.RateOwner
		}
	}
	if p.Availability == "pause" {
		update.SetSchedulable(false)
		extra[gov.GovernancePauseExtraKey] = map[string]any{"token": p.OperationID, "marker": p.Marker, "identity": current.Identity}
	}
	if p.Availability == "restore" {
		update.SetSchedulable(true)
		delete(extra, gov.GovernancePauseExtraKey)
	}
	updated, err := update.SetExtra(extra).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	result := service.GovernanceManagedAccount(accountEntityToService(updated))
	return &result, nil
}

var _ interface {
	ReconcileGovernanceAccount(context.Context, gov.ManagedAccountPatch) (*gov.ManagedLocalAccount, error)
} = (*accountRepository)(nil)

func stripGovernanceRuntimeExtra(updates map[string]any) map[string]any {
	if len(updates) == 0 {
		return updates
	}
	clean := copyJSONMap(updates)
	for _, key := range []string{"upstream_governance_marker", gov.GovernanceRateOwnerExtraKey, gov.GovernancePauseExtraKey, gov.GovernanceReceiptExtraKey} {
		delete(clean, key)
	}
	return clean
}
