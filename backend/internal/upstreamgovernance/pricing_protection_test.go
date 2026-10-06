package upstreamgovernance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPricingProtectionPausesGovernanceAccountOnUnsafeIncrease(t *testing.T) {
	s, store, _, local, _ := reconciliationFixture(t)

	err := s.protectPricingGroupAccounts(t.Context(), store.site, PricingOperation{
		LocalGroupID: 7,
		Status:       "protected",
		Protected:    true,
		Reason:       "increase_review",
	})
	require.NoError(t, err)
	require.False(t, local.account.Schedulable)
	require.Equal(t, "pricing_protection", local.account.PauseReason)
	require.NotEmpty(t, local.account.PauseToken)
	found := false
	for _, event := range store.events {
		if event.SiteID == store.site.ID && event.Kind == "pricing_account_paused" && event.Resource == "7" && event.After == "increase_review" {
			found = true
			break
		}
	}
	require.True(t, found, "pricing protection must leave an auditable pause event")
}

func TestPricingProtectionDoesNotOverrideExistingPauseOwner(t *testing.T) {
	s, store, _, local, _ := reconciliationFixture(t)
	local.account.Schedulable = false
	local.account.PauseToken = "manual-pause"
	local.account.PauseReason = "manual"

	err := s.protectPricingGroupAccounts(t.Context(), store.site, PricingOperation{
		LocalGroupID: 7,
		Status:       "protected",
		Protected:    true,
		Reason:       "increase_review",
	})
	require.NoError(t, err)
	require.Equal(t, "manual-pause", local.account.PauseToken)
	require.Equal(t, "manual", local.account.PauseReason)
	require.Empty(t, store.events)
}

func TestPricingProtectionDoesNotPauseManualRateOwner(t *testing.T) {
	s, store, _, local, _ := reconciliationFixture(t)

	err := s.protectPricingGroupAccounts(t.Context(), store.site, PricingOperation{
		LocalGroupID: 7,
		Status:       "protected",
		Protected:    true,
		Reason:       "manual_owner",
	})
	require.NoError(t, err)
	require.True(t, local.account.Schedulable)
	require.Empty(t, store.events)
}
