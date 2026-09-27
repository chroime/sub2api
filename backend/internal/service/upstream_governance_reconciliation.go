package service

import (
	"context"
	"errors"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

type governancePauseRevokedKey struct{}

func withGovernancePauseRevoked(ctx context.Context) context.Context {
	return context.WithValue(ctx, governancePauseRevokedKey{}, true)
}
func GovernancePauseRevoked(ctx context.Context) bool {
	value, _ := ctx.Value(governancePauseRevokedKey{}).(bool)
	return value
}
func GovernanceCatalogOwnsRate(a *Account) bool {
	return a != nil && a.GetExtraString(gov.GovernanceRateOwnerExtraKey) != ""
}

// Reserved automation fields are merged from the locked database row. A stale
// administrator payload must not erase or forge a pause/price ownership claim.
func MergeGovernanceRuntimeExtra(ctx context.Context, extra, current map[string]any) {
	for _, key := range []string{governanceMarkerKey, gov.GovernanceRateOwnerExtraKey, gov.GovernancePauseExtraKey, gov.GovernanceReceiptExtraKey} {
		delete(extra, key)
		if value, ok := current[key]; ok {
			extra[key] = value
		}
	}
	if GovernancePauseRevoked(ctx) {
		delete(extra, gov.GovernancePauseExtraKey)
		delete(extra, gov.GovernanceReceiptExtraKey)
	}
	if _, _, importing := GovernanceMutationFromContext(ctx); importing {
		delete(extra, gov.GovernanceReceiptExtraKey)
	}
}

func GovernanceManagedAccount(a *Account) gov.ManagedLocalAccount {
	if a == nil {
		return gov.ManagedLocalAccount{}
	}
	key, _ := a.Credentials["api_key"].(string)
	origin, _ := a.Credentials["base_url"].(string)
	identity := gov.ManagedAccountIdentity(a.ID, a.GetExtraString(governanceMarkerKey), a.Platform, origin, key)
	if a.Type != AccountTypeAPIKey || a.ParentAccountID != nil {
		identity = ""
	}
	var pause struct {
		Token    string `json:"token"`
		Marker   string `json:"marker"`
		Identity string `json:"identity"`
	}
	// Decode through the existing canonical JSON helper to support both typed
	// test values and maps returned from JSONB.
	decodeGovernanceValue(a.Extra[gov.GovernancePauseExtraKey], &pause)
	return gov.ManagedLocalAccount{ID: a.ID, Identity: identity, Name: a.Name, Rate: a.BillingRateMultiplier(), Status: a.Status, Schedulable: a.Schedulable, CanRestore: a.Status == StatusActive && (!a.AutoPauseOnExpired || a.ExpiresAt == nil || time.Now().Before(*a.ExpiresAt)), NativeRateSync: upstreamBillingRateSyncEnabled(a), RateOwner: a.GetExtraString(gov.GovernanceRateOwnerExtraKey), PauseToken: pause.Token, PauseMarker: pause.Marker, PauseIdentity: pause.Identity, Receipt: a.GetExtraString(gov.GovernanceReceiptExtraKey)}
}
func (l *governanceLocalAccounts) InspectManagedAccount(ctx context.Context, b gov.Binding) (*gov.ManagedLocalAccount, error) {
	if b.AccountID <= 0 {
		return nil, gov.ErrNotFound
	}
	a, err := l.find(ctx, b.Marker)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, gov.ErrNotFound
		}
		return nil, err
	}
	if a == nil {
		return nil, gov.ErrNotFound
	}
	if a.ID != b.AccountID || a.Type != AccountTypeAPIKey || a.Platform != b.Platform || a.ParentAccountID != nil || a.GetCredential("api_key") == "" || a.GetCredential("base_url") == "" {
		return nil, gov.ErrConflict
	}
	value := GovernanceManagedAccount(a)
	return &value, nil
}

type governanceAccountReconciler interface {
	ReconcileGovernanceAccount(context.Context, gov.ManagedAccountPatch) (*gov.ManagedLocalAccount, error)
}

func (l *governanceLocalAccounts) ApplyManagedPatch(ctx context.Context, patch gov.ManagedAccountPatch) (*gov.ManagedLocalAccount, error) {
	reconciler, ok := l.admin.(governanceAccountReconciler)
	if !ok {
		return nil, gov.ErrUnsupported
	}
	return reconciler.ReconcileGovernanceAccount(ctx, patch)
}
func (s *adminServiceImpl) ReconcileGovernanceAccount(ctx context.Context, patch gov.ManagedAccountPatch) (*gov.ManagedLocalAccount, error) {
	repo, ok := s.accountRepo.(governanceAccountReconciler)
	if !ok {
		return nil, gov.ErrUnsupported
	}
	return repo.ReconcileGovernanceAccount(ctx, patch)
}
