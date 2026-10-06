package upstreamgovernance

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) SetPricingCoordinator(coordinator *PricingCoordinator) {
	s.pricingCoordinator = coordinator
}

func (s *Service) pricingObservations(ctx context.Context, site Site, groups []RemoteGroup) ([]int64, []CostObservation, error) {
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return nil, nil, err
	}
	byRemote := make(map[string]RemoteGroup, len(groups))
	for _, group := range groups {
		byRemote[group.ID] = group
	}
	groupIDs := map[int64]bool{}
	observations := []CostObservation{}
	for _, binding := range bindings {
		remote, ok := byRemote[binding.RemoteGroupID]
		if !ok {
			continue
		}
		localIDs := binding.LocalGroupIDs
		if len(localIDs) == 0 && binding.LocalGroupID > 0 {
			localIDs = []int64{binding.LocalGroupID}
		}
		if len(localIDs) == 0 {
			continue
		}
		cost := 0.0
		if remote.ResolvedRateMultiplier != nil {
			cost = *remote.ResolvedRateMultiplier
		}
		// Sub2API rates are relative multipliers. New API numeric ratios retain
		// their native quota unit and are intentionally unknown to margin math.
		comparable := site.Platform == "sub2api" && remote.ResolvedRateMultiplier != nil && validCost(cost)
		unknown := !comparable
		for _, localID := range localIDs {
			if localID <= 0 {
				continue
			}
			groupIDs[localID] = true
			observations = append(observations, CostObservation{
				SourceID:     fmt.Sprintf("site:%d:group:%s:%s", site.ID, remote.ID, strings.TrimSpace(binding.Platform)),
				LocalGroupID: localID, Cost: cost, Unit: "multiplier", Currency: "relative",
				Comparable: comparable, Eligible: true, Unknown: unknown, SiteID: site.ID, BindingID: binding.ID,
			})
		}
	}
	ids := make([]int64, 0, len(groupIDs))
	for id := range groupIDs {
		ids = append(ids, id)
	}
	sortInt64s(ids)
	return ids, observations, nil
}

func (s *Service) recalculatePricingForObservation(ctx context.Context, site Site, groups []RemoteGroup) ([]PricingOperation, error) {
	if s.pricingCoordinator == nil || len(groups) == 0 {
		return nil, nil
	}
	ids, observations, err := s.pricingObservations(ctx, site, groups)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	operations, recalculateErr := s.pricingCoordinator.Recalculate(ctx, ids, observations)
	if recalculateErr != nil {
		return operations, recalculateErr
	}
	for _, operation := range operations {
		if !operation.Protected {
			continue
		}
		if protectErr := s.protectPricingGroupAccounts(ctx, site, operation); protectErr != nil {
			return operations, protectErr
		}
	}
	return operations, nil
}

func (s *Service) enqueuePricingNotices(ctx context.Context, site Site, operations []PricingOperation) {
	for _, operation := range operations {
		if operation.Status != "applied" && operation.Status != "protected" && operation.Status != "conflict" && operation.Status != "failed" {
			continue
		}
		notice := renderPricingOperationNotice(site, operation)
		notice.DedupKey = fmt.Sprintf("site:%d:pricing:%s", site.ID, operation.OperationID)
		notice.ObservedAt = s.now().UTC()
		_ = s.EnqueueChangeNotice(ctx, notice)
	}
}
