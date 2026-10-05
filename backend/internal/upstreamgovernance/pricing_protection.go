package upstreamgovernance

import (
	"context"
	"errors"
	"fmt"
	"log"
)

// protectPricingGroupAccounts pauses only governance-owned, currently
// schedulable accounts routed through a local group whose upstream cost was
// protected. Manual pauses and accounts with another pause owner are left
// untouched. The caller normally holds the site's advisory lock.
func (s *Service) protectPricingGroupAccounts(ctx context.Context, site Site, operation PricingOperation) error {
	if !operation.Protected || operation.LocalGroupID <= 0 || operation.Reason == "manual_owner" {
		return nil
	}
	local, ok := s.local.(ReconciliationLocal)
	if !ok {
		return nil
	}
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, binding := range bindings {
		if binding.AccountID <= 0 || binding.Marker == "" || !bindingTargetsLocalGroup(binding, operation.LocalGroupID) {
			continue
		}
		account, inspectErr := s.inspectReconciliationAccount(ctx, local, site, binding)
		if errors.Is(inspectErr, ErrNotFound) || errors.Is(inspectErr, ErrConflict) {
			continue
		}
		if inspectErr != nil {
			if firstErr == nil {
				firstErr = inspectErr
			}
			continue
		}
		if account == nil || account.Status != "active" || !account.Schedulable || account.PauseToken != "" {
			continue
		}
		patch := ManagedAccountPatch{
			BindingID:    binding.ID,
			Marker:       binding.Marker,
			OperationID:  fmt.Sprintf("pricing-protection-%d-%s", operation.LocalGroupID, operation.OperationID),
			Expected:     *account,
			Availability: "pause",
			PauseReason:  "pricing_protection",
		}
		if _, applyErr := local.ApplyManagedPatch(ctx, patch); applyErr != nil {
			if errors.Is(applyErr, ErrConflict) {
				continue
			}
			if firstErr == nil {
				firstErr = applyErr
			}
			continue
		}
		if eventErr := s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "pricing_account_paused", Resource: fmt.Sprint(operation.LocalGroupID), Before: account.Status, After: operation.Reason, CreatedAt: s.now().UTC()}); eventErr != nil {
			// The account pause is the safety boundary. Keep it durable even if
			// the audit event store is temporarily unavailable.
			log.Printf("[UpstreamGovernance] pricing protection event: %s", ErrorCode(eventErr))
		}
	}
	return firstErr
}

func bindingTargetsLocalGroup(binding Binding, groupID int64) bool {
	if binding.LocalGroupID == groupID {
		return true
	}
	for _, id := range binding.LocalGroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}
