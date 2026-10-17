package upstreamgovernance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) reconciliationCapabilities() (ReconciliationStore, ReconciliationLocal, error) {
	store, ok := s.store.(ReconciliationStore)
	if !ok {
		return nil, nil, ErrUnsupported
	}
	local, ok := s.local.(ReconciliationLocal)
	if !ok {
		return nil, nil, ErrUnsupported
	}
	return store, local, nil
}
func (s *Service) GetAutomation(ctx context.Context, id int64) (AutomationConfig, error) {
	if _, err := s.store.GetSite(ctx, id); err != nil {
		return AutomationConfig{}, err
	}
	store, ok := s.store.(ReconciliationStore)
	if !ok {
		return AutomationConfig{}, ErrUnsupported
	}
	return store.GetAutomation(ctx, id)
}
func (s *Service) ConfigureAutomation(ctx context.Context, id int64, value AutomationConfig) (AutomationConfig, error) {
	p := value.Policy
	if value.Version < 0 || p.MissingConfirmations < 2 || p.MissingConfirmations > 10 || math.IsNaN(p.MaxRateIncreasePercent) || math.IsInf(p.MaxRateIncreasePercent, 0) || p.MaxRateIncreasePercent < 0 || p.MaxRateIncreasePercent > 10000 {
		return AutomationConfig{}, ErrInvalid
	}
	_, release, err := s.remoteSiteLock(ctx, id)
	if err != nil {
		return AutomationConfig{}, err
	}
	defer release()
	store, ok := s.store.(ReconciliationStore)
	if !ok {
		return AutomationConfig{}, ErrUnsupported
	}
	return store.SaveAutomation(ctx, id, value)
}
func (s *Service) reconciliationLocked(ctx context.Context, site Site) (*Reconciliation, AutomationConfig, error) {
	store, local, err := s.reconciliationCapabilities()
	if err != nil {
		return nil, AutomationConfig{}, err
	}
	cfg, err := store.GetAutomation(ctx, site.ID)
	if err != nil {
		return nil, cfg, err
	}
	snapshot, err := s.store.LatestSnapshot(ctx, site.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, cfg, err
	}
	if snapshot == nil {
		snapshot = &Snapshot{}
	}
	result := &Reconciliation{SnapshotID: snapshot.ID, Rows: []ReconciliationRow{}}
	if snapshot.ID > 0 {
		stamp := snapshot.CreatedAt
		result.ObservedAt = &stamp
	}
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return nil, cfg, err
	}
	states, err := store.ReconciliationStates(ctx, site.ID)
	if err != nil {
		return nil, cfg, err
	}
	for _, binding := range bindings {
		account, inspectErr := s.inspectReconciliationAccount(ctx, local, site, binding)
		if inspectErr != nil && !errors.Is(inspectErr, ErrNotFound) && !errors.Is(inspectErr, ErrConflict) {
			return nil, cfg, inspectErr
		}
		if account == nil {
			account = &ManagedLocalAccount{}
		}
		state, ok := states[binding.ID]
		if !ok {
			state = newReconciliationState(binding, *account)
			for _, group := range snapshot.Catalog.Groups {
				if group.ID == binding.RemoteGroupID {
					state.RemoteName = group.Name
					break
				}
			}
		}
		row := planReconciliationRow(site, cfg, *snapshot, binding, state, *account, s.now())
		if errors.Is(inspectErr, ErrConflict) {
			row.State = "conflict"
			row.Reason = "account_identity_changed"
		}
		result.Rows = append(result.Rows, row)
	}
	return result, cfg, nil
}
func (s *Service) Reconciliation(ctx context.Context, id int64) (*Reconciliation, error) {
	site, err := s.store.GetSite(ctx, id)
	if err != nil {
		return nil, err
	}
	result, _, err := s.reconciliationLocked(ctx, *site)
	return result, err
}
func (s *Service) previewReconciliationLocked(ctx context.Context, site Site) (*ReconciliationPreview, error) {
	result, cfg, err := s.reconciliationLocked(ctx, site)
	if err != nil {
		return nil, err
	}
	p := &ReconciliationPreview{Reconciliation: *result, ID: uuid.NewString(), SiteVersion: site.Version, PolicyVersion: cfg.Version, ExpiresAt: s.now().Add(15 * time.Minute), Items: []ReconciliationFrozenItem{}}
	for _, row := range result.Rows {
		row.Patch.OperationID = p.ID + ":" + fmt.Sprint(row.BindingID)
		p.Items = append(p.Items, ReconciliationFrozenItem{Row: row, Patch: row.Patch, Management: row.Management})
	}
	store, _, _ := s.reconciliationCapabilities()
	if err = store.SaveReconciliationPreview(ctx, site.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}
func (s *Service) PreviewReconciliation(ctx context.Context, id int64) (*ReconciliationPreview, error) {
	site, release, err := s.remoteSiteLock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.previewReconciliationLocked(ctx, *site)
}
func validReconciliationSelection(ids []int64) bool {
	if len(ids) == 0 || len(ids) > 100 {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func (s *Service) ApplyReconciliation(ctx context.Context, id int64, previewID string, ids []int64) (*ReconciliationResult, error) {
	if !validReconciliationSelection(ids) {
		return nil, ErrInvalid
	}
	site, release, err := s.remoteSiteLock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	store, _, err := s.reconciliationCapabilities()
	if err != nil {
		return nil, err
	}
	p, err := store.GetReconciliationPreview(ctx, id, previewID)
	if err != nil {
		return nil, err
	}
	return s.applyReconciliationLocked(ctx, *site, p, ids, false)
}
func (s *Service) applyReconciliationLocked(ctx context.Context, site Site, p *ReconciliationPreview, ids []int64, automatic bool) (*ReconciliationResult, error) {
	store, local, err := s.reconciliationCapabilities()
	if err != nil {
		return nil, err
	}
	cfg, err := store.GetAutomation(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	selected := map[int64]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	frozen := map[int64]ReconciliationFrozenItem{}
	for _, item := range p.Items {
		frozen[item.Row.BindingID] = item
	}
	completed := map[int64]ReconciliationItemResult{}
	if p.Result != nil {
		for _, item := range p.Result.Items {
			if item.Status == "applied" {
				completed[item.BindingID] = item
			}
		}
	}
	remaining := false
	for _, id := range ids {
		item, ok := frozen[id]
		if !ok {
			return nil, ErrInvalid
		}
		if _, done := completed[id]; done {
			continue
		}
		remaining = true
		if item.Row.Action == "none" || (item.Row.State != "ready" && item.Row.State != "review") || (automatic && item.Row.State != "ready") {
			return nil, ErrConflict
		}
	}
	if remaining {
		latest, e := s.store.LatestSnapshot(ctx, site.ID)
		if e != nil {
			return nil, e
		}
		if !s.now().Before(p.ExpiresAt) || p.SiteVersion != site.Version || p.PolicyVersion != cfg.Version || latest.ID != p.SnapshotID || !reconciliationSnapshotFresh(site, *latest, s.now()) {
			return nil, ErrConflict
		}
	}
	result := &ReconciliationResult{PreviewID: p.ID, Items: []ReconciliationItemResult{}}
	observedStates, err := store.ReconciliationStates(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	// Retain completed unselected rows so later subset retries never redo them.
	if p.Result != nil {
		for _, item := range p.Result.Items {
			if !selected[item.BindingID] {
				result.Items = append(result.Items, item)
			}
		}
	}
	for _, id := range ids {
		if done, ok := completed[id]; ok {
			result.Items = append(result.Items, done)
			continue
		}
		item := frozen[id]
		out := ReconciliationItemResult{BindingID: id, AccountID: item.Row.AccountID, Status: "failed"}
		account, e := local.ApplyManagedPatch(ctx, item.Patch)
		if e == nil {
			state := managementAfterPatch(item.Management, *account, item.Patch)
			// Preserve observations made after the preview rather than replaying old counters.
			if current, ok := observedStates[id]; ok {
				state.LastSnapshotID = current.LastSnapshotID
				state.LastMissingAt = current.LastMissingAt
				state.MissingCount = current.MissingCount
			}
			e = store.SaveReconciliationState(ctx, site.ID, state)
		}
		if e == nil {
			out.Status = "applied"
		} else {
			out.Error = ErrorCode(e)
		}
		result.Items = append(result.Items, out)
		if e = store.SaveReconciliationResult(ctx, site.ID, p.ID, result); e != nil {
			return nil, e
		}
		if out.Status == "applied" {
			if e = s.store.AddEvent(ctx, &Event{SiteID: site.ID, Kind: "reconciliation_applied", Resource: fmt.Sprint(id), Before: item.Row.AccountName, After: item.Row.Action, CreatedAt: s.now()}); e != nil {
				return nil, e
			}
		}
	}
	p.Result = result
	return result, nil
}

// Called once after each successful collection, with the existing site lock held.
// Reads and preview creation never advance disappearance confirmations.
func (s *Service) reconcileSuccessfulSnapshot(ctx context.Context, site Site, snapshot *Snapshot) {
	if err := s.reconcileSnapshotLocked(ctx, site, snapshot); err != nil && !errors.Is(err, ErrUnsupported) {
		log.Printf("[UpstreamGovernance] automatic reconciliation: %s", ErrorCode(err))
	}
}
func (s *Service) reconcileSnapshotLocked(ctx context.Context, site Site, snapshot *Snapshot) error {
	store, local, err := s.reconciliationCapabilities()
	if err != nil {
		return err
	}
	if snapshot == nil || !snapshot.Catalog.GroupsComplete {
		return nil
	}
	cfg, err := store.GetAutomation(ctx, site.ID)
	if err != nil {
		return err
	}
	bindings, err := s.store.ListBindings(ctx, site.ID)
	if err != nil {
		return err
	}
	// Cost facts are persisted independently from account patches. This means a
	// credible increase is visible to the pricing coordinator even when the
	// legacy account-rate reconciliation correctly leaves the row in review.
	var pricingFacts PricingFactRecorder
	if recorder, ok := s.store.(PricingFactRecorder); ok && s.pricingCoordinator == nil {
		pricingFacts = recorder
	}
	states, err := store.ReconciliationStates(ctx, site.ID)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, g := range snapshot.Catalog.Groups {
		present[g.ID] = true
	}
	gapMinutes := max(site.IntervalMinutes, 1)
	for _, binding := range bindings {
		a, e := s.inspectReconciliationAccount(ctx, local, site, binding)
		if e != nil || a == nil {
			continue
		}
		if pricingFacts != nil {
			localGroupIDs := binding.LocalGroupIDs
			if len(localGroupIDs) == 0 && binding.LocalGroupID > 0 {
				localGroupIDs = []int64{binding.LocalGroupID}
			}
			for _, groupID := range localGroupIDs {
				if groupID <= 0 {
					continue
				}
				for _, remote := range snapshot.Catalog.Groups {
					if remote.ID != binding.RemoteGroupID || remote.ResolvedRateMultiplier == nil || !validCost(*remote.ResolvedRateMultiplier) {
						continue
					}
					fact := CostObservation{
						LocalGroupID: groupID,
						SiteID:       site.ID,
						BindingID:    binding.ID,
						SourceID:     fmt.Sprintf("site:%d/binding:%d/group:%s", site.ID, binding.ID, remote.ID),
						Cost:         *remote.ResolvedRateMultiplier,
						Comparable:   true,
						Eligible:     true,
						ObservedAt:   snapshot.CreatedAt,
					}
					if _, factErr := pricingFacts.RecordPricingCostFact(ctx, groupID, fact); factErr != nil && !errors.Is(factErr, ErrUnsupported) {
						log.Printf("[UpstreamGovernance] persist pricing cost fact: %s", ErrorCode(factErr))
					}
					break
				}
			}
		}
		state, ok := states[binding.ID]
		if !ok {
			state = newReconciliationState(binding, *a)
			for _, group := range snapshot.Catalog.Groups {
				if group.ID == binding.RemoteGroupID {
					state.RemoteName = group.Name
					break
				}
			}
		}
		state, err = s.recoverReconciliationReceipt(ctx, store, site.ID, binding, *a, state)
		if err != nil {
			return err
		}
		state = advanceReconciliationObservation(state, *snapshot, present[binding.RemoteGroupID], gapMinutes)
		if err = store.SaveReconciliationState(ctx, site.ID, state); err != nil {
			return err
		}
		// Stopping automation releases only this module's rate authority on the next
		// successful sync. Existing native switches determine subsequent behavior.
		if (!cfg.Policy.Enabled || !cfg.Policy.SyncRate) && a.RateOwner == binding.Marker {
			empty := ""
			_, e = local.ApplyManagedPatch(ctx, ManagedAccountPatch{BindingID: binding.ID, Marker: binding.Marker, OperationID: uuid.NewString(), Expected: *a, RateOwner: &empty})
			if e != nil && !errors.Is(e, ErrConflict) {
				return e
			}
		}
	}
	if !cfg.Policy.Enabled {
		return nil
	}
	current, _, err := s.reconciliationLocked(ctx, site)
	if err != nil {
		return err
	}
	hasReady := false
	for _, row := range current.Rows {
		if row.State == "ready" && row.Action != "none" {
			hasReady = true
			break
		}
	}
	if !hasReady {
		return nil
	}
	plan, err := s.previewReconciliationLocked(ctx, site)
	if err != nil {
		return err
	}
	ids := []int64{}
	for _, row := range plan.Rows {
		if row.State == "ready" && row.Action != "none" {
			ids = append(ids, row.BindingID)
		}
	}
	for len(ids) > 0 {
		size := len(ids)
		if size > 100 {
			size = 100
		}
		if _, err = s.applyReconciliationLocked(ctx, site, plan, ids[:size], true); err != nil {
			return err
		}
		ids = ids[size:]
	}
	return nil
}

// An account receipt proves a local transaction committed even when its plan
// result or binding baseline was lost. Recovery never changes account fields.
func (s *Service) recoverReconciliationReceipt(ctx context.Context, store ReconciliationStore, site int64, binding Binding, a ManagedLocalAccount, state ReconciliationState) (ReconciliationState, error) {
	parts := strings.SplitN(a.Receipt, ":", 2)
	if len(parts) != 2 {
		return state, nil
	}
	plan, err := store.GetReconciliationPreview(ctx, site, parts[0])
	if errors.Is(err, ErrNotFound) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	for _, item := range plan.Items {
		if item.Row.BindingID == binding.ID && ManagedPatchAlreadyApplied(a, item.Patch) {
			state = managementAfterPatch(state, a, item.Patch)
			state.RemoteName = item.Management.RemoteName
			result := plan.Result
			if result == nil {
				result = &ReconciliationResult{PreviewID: plan.ID, Items: []ReconciliationItemResult{}}
			}
			found := false
			for i := range result.Items {
				if result.Items[i].BindingID == binding.ID {
					result.Items[i] = ReconciliationItemResult{BindingID: binding.ID, AccountID: binding.AccountID, Status: "applied"}
					found = true
					break
				}
			}
			if !found {
				result.Items = append(result.Items, ReconciliationItemResult{BindingID: binding.ID, AccountID: binding.AccountID, Status: "applied"})
			}
			if err = store.SaveReconciliationResult(ctx, site, plan.ID, result); err != nil {
				return state, err
			}
			break
		}
	}
	return state, nil
}

// A newly confirmed full import establishes a fresh explicit management
// baseline, including intentional administrator changes to the imported account.
func (s *Service) adoptImportedReconciliationState(ctx context.Context, site int64, binding Binding, remoteName string) error {
	store, local, err := s.reconciliationCapabilities()
	if errors.Is(err, ErrUnsupported) {
		return nil
	}
	if err != nil {
		return err
	}
	origin, err := s.store.GetSite(ctx, site)
	if err != nil {
		return err
	}
	a, err := s.inspectReconciliationAccount(ctx, local, *origin, binding)
	if err != nil {
		return err
	}
	state := newReconciliationState(binding, *a)
	state.RemoteName = remoteName
	return store.SaveReconciliationState(ctx, site, state)
}
