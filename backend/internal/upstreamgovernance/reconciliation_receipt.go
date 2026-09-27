package upstreamgovernance

func ManagedPatchAlreadyApplied(a ManagedLocalAccount, p ManagedAccountPatch) bool {
	if a.Receipt != p.OperationID || a.Identity != p.Expected.Identity {
		return false
	}
	if p.Name != nil && a.Name != *p.Name {
		return false
	}
	rate := p.Expected.Rate
	if p.Rate != nil {
		rate = *p.Rate
	}
	if (p.Rate != nil || (p.RateOwner != nil && *p.RateOwner != "")) && a.Rate != rate {
		return false
	}
	owner := p.Expected.RateOwner
	if p.RateOwner != nil {
		owner = *p.RateOwner
	}
	if (p.Rate != nil || p.RateOwner != nil) && a.RateOwner != owner {
		return false
	}
	if (p.Rate != nil || p.RateOwner != nil) && a.NativeRateSync != p.Expected.NativeRateSync {
		return false
	}
	if p.Availability == "pause" {
		return a.Status == p.Expected.Status && !a.Schedulable && a.PauseToken == p.OperationID && a.PauseMarker == p.Marker && a.PauseIdentity == a.Identity
	}
	if p.Availability == "restore" {
		return a.Status == p.Expected.Status && a.Schedulable && a.PauseToken == ""
	}
	return true
}

func managementAfterPatch(state ReconciliationState, a ManagedLocalAccount, p ManagedAccountPatch) ReconciliationState {
	if p.Name != nil {
		state.Name = a.Name
	}
	if p.Rate != nil || (p.RateOwner != nil && *p.RateOwner != "") {
		state.Rate = a.Rate
		state.NativeRateSync = a.NativeRateSync
	}
	return state
}
