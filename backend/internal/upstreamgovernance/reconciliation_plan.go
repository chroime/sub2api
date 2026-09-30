package upstreamgovernance

import (
	"math"
	"strconv"
	"strings"
	"time"
)

func newReconciliationState(binding Binding, a ManagedLocalAccount) ReconciliationState {
	return ReconciliationState{BindingID: binding.ID, Identity: a.Identity, Name: a.Name, Rate: a.Rate, NativeRateSync: a.NativeRateSync}
}
func reconciliationSnapshotFresh(site Site, snapshot Snapshot, now time.Time) bool {
	windowMinutes := max(2*int64(site.IntervalMinutes), 10)
	return snapshot.ID > 0 && snapshot.SiteVersion == site.Version && snapshot.Catalog.GroupsComplete && !snapshot.CreatedAt.IsZero() && !snapshot.CreatedAt.After(now.Add(time.Minute)) && !now.After(addMinutes(snapshot.CreatedAt, windowMinutes))
}
func advanceReconciliationObservation(state ReconciliationState, snapshot Snapshot, present bool, gapMinutes int) ReconciliationState {
	if !snapshot.Catalog.GroupsComplete || snapshot.ID <= state.LastSnapshotID {
		return state
	}
	state.LastSnapshotID = snapshot.ID
	if present {
		state.MissingCount = 0
		state.LastMissingAt = nil
		return state
	}
	if state.LastMissingAt == nil || !snapshot.CreatedAt.Before(addMinutes(*state.LastMissingAt, int64(gapMinutes))) {
		state.MissingCount++
		stamp := snapshot.CreatedAt
		state.LastMissingAt = &stamp
	}
	return state
}
func reconciliationName(origin string, rate float64) string {
	return origin + "--" + strconv.FormatFloat(rate, 'f', -1, 64)
}
func isTemplateName(origin, name string) bool {
	suffix, ok := strings.CutPrefix(name, origin+"--")
	if !ok {
		return false
	}
	rate, err := strconv.ParseFloat(suffix, 64)
	return err == nil && validCost(rate) && reconciliationName(origin, rate) == name
}
func planReconciliationRow(site Site, config AutomationConfig, snapshot Snapshot, b Binding, state ReconciliationState, a ManagedLocalAccount, now time.Time) ReconciliationRow {
	row := ReconciliationRow{BindingID: b.ID, AccountID: b.AccountID, RemoteGroupID: b.RemoteGroupID, RemoteGroupName: state.RemoteName, AccountName: a.Name, Action: "none", State: "ready", Reason: "up_to_date", Changes: []ReconciliationChange{}, Management: state, Patch: ManagedAccountPatch{BindingID: b.ID, Marker: b.Marker, Expected: a}}
	stop := func(status, reason string) ReconciliationRow { row.State = status; row.Reason = reason; return row }
	if b.AccountID <= 0 || a.ID != b.AccountID || a.Identity == "" {
		return stop("unavailable", "local_account_missing")
	}
	if snapshot.ID == 0 || snapshot.SiteVersion != site.Version || !snapshot.Catalog.GroupsComplete {
		return stop("unavailable", "catalog_incomplete")
	}
	if !reconciliationSnapshotFresh(site, snapshot, now) {
		return stop("unavailable", "catalog_stale")
	}
	if state.Identity != "" && state.Identity != a.Identity {
		return stop("conflict", "account_identity_changed")
	}
	if a.RateOwner != "" && a.RateOwner != b.Marker {
		return stop("conflict", "rate_owned_elsewhere")
	}
	if a.PauseReason == "upstream_key_missing" {
		return stop("review", "upstream_key_missing")
	}
	var remote *RemoteGroup
	for i := range snapshot.Catalog.Groups {
		if snapshot.Catalog.Groups[i].ID == b.RemoteGroupID {
			remote = &snapshot.Catalog.Groups[i]
			break
		}
	}
	if remote == nil {
		if !config.Policy.PauseMissing {
			return stop("ready", "missing_observed")
		}
		if state.MissingCount < config.Policy.MissingConfirmations {
			return stop("review", "missing_confirmation_pending")
		}
		if !a.Schedulable || a.Status != "active" {
			return stop("ready", "already_paused")
		}
		row.Action = "pause"
		row.Reason = "upstream_group_missing"
		row.Patch.Availability = "pause"
		row.Changes = append(row.Changes, ReconciliationChange{Field: "schedulable", Before: true, After: false})
		return row
	}
	row.RemoteGroupName = remote.Name
	if remote.Name != state.RemoteName {
		row.Action = "update"
		row.Reason = "upstream_group_changed"
		row.Changes = append(row.Changes, ReconciliationChange{Field: "remote_group_name", Before: state.RemoteName, After: remote.Name})
		row.Management.RemoteName = remote.Name
	}
	if config.Policy.SyncRate {
		if remote.ResolvedRateMultiplier == nil {
			return stop("unavailable", "unknown_upstream_rate")
		}
		rate := math.Round(*remote.ResolvedRateMultiplier*10000) / 10000
		if !validCost(*remote.ResolvedRateMultiplier) || !validCost(rate) {
			return stop("unavailable", "invalid_upstream_rate")
		}
		if (a.RateOwner == b.Marker && a.Rate != state.Rate) || (!a.NativeRateSync && (state.NativeRateSync || a.Rate != state.Rate)) {
			return stop("conflict", "managed_rate_changed")
		}
		if rate != a.Rate {
			row.Action = "update"
			row.Reason = "upstream_rate_changed"
			row.Patch.Rate = &rate
			row.Changes = append(row.Changes, ReconciliationChange{Field: "rate_multiplier", Before: a.Rate, After: rate})
			if rate > a.Rate && (a.Rate == 0 || (rate/a.Rate-1)*100 > config.Policy.MaxRateIncreasePercent+1e-9) {
				row.State = "review"
				row.Reason = "rate_increase_review"
			}
		}
		// Ownership changes only through a reviewed/automatic native patch.
		if config.Policy.Enabled && a.RateOwner != b.Marker {
			owner := b.Marker
			row.Patch.RateOwner = &owner
			row.Action = "update"
			row.Changes = append(row.Changes, ReconciliationChange{Field: "rate_source", Before: "native", After: "governance"})
		}
		if !config.Policy.Enabled && a.NativeRateSync && row.State == "ready" && row.Patch.Rate != nil {
			row.Reason = "native_rate_sync_active"
		}
	}
	if config.Policy.SyncName && isTemplateName(site.BaseURL, state.Name) {
		if a.Name != state.Name {
			return stop("conflict", "managed_name_changed")
		}
		finalRate := a.Rate
		if row.Patch.Rate != nil {
			finalRate = *row.Patch.Rate
		}
		name := reconciliationName(site.BaseURL, finalRate)
		if name != a.Name {
			row.Patch.Name = &name
			row.Action = "update"
			row.Changes = append(row.Changes, ReconciliationChange{Field: "name", Before: a.Name, After: name})
		}
	}
	if (!config.Policy.Enabled || !config.Policy.SyncRate) && a.RateOwner == b.Marker {
		empty := ""
		row.Patch.RateOwner = &empty
		row.Action = "update"
		row.Changes = append(row.Changes, ReconciliationChange{Field: "rate_source", Before: "governance", After: "native"})
	}
	if config.Policy.RestoreReturned && a.PauseToken != "" {
		if a.PauseMarker != b.Marker || a.PauseIdentity != a.Identity {
			return stop("conflict", "pause_owner_changed")
		}
		if a.Schedulable || a.Status != "active" || !a.CanRestore {
			return stop("conflict", "account_unavailable_for_restore")
		}
		row.Action = "restore"
		row.Patch.Availability = "restore"
		row.Changes = append(row.Changes, ReconciliationChange{Field: "schedulable", Before: false, After: true})
		if row.State == "ready" {
			row.Reason = "upstream_group_returned"
		}
	}
	return row
}
