package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func qualityGateTestAccount(scope, model string) *Account {
	return &Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"public-model": "upstream-model", "same-model": "upstream-model", "other-model": "other-upstream",
		}},
		Extra: map[string]any{"account_quality_gates": map[string]any{
			"17": map[string]any{"policy_id": float64(9), "policy_version": float64(1), "scope": scope, "model": model, "blocked": true},
		}},
	}
}

func TestAccountQualityGateAccountScopePreservesOtherControls(t *testing.T) {
	a := qualityGateTestAccount("account", "")
	require.False(t, a.IsSchedulable(), "account-wide isolation must veto business scheduling")
	require.True(t, a.Schedulable, "quality isolation does not change the administrator's switch")
	require.Nil(t, a.TempUnschedulableUntil)
	require.True(t, a.IsCredentialUsableForShadow(), "quality conclusions do not propagate through shared credentials")

	delete(a.Extra, "account_quality_gates")
	require.True(t, a.IsSchedulable())
	a.Schedulable = false
	require.False(t, a.IsSchedulable(), "releasing quality isolation cannot enable a manually disabled account")
	a.Schedulable = true
	until := time.Now().Add(time.Hour)
	a.TempUnschedulableUntil = &until
	require.False(t, a.IsSchedulable(), "releasing quality isolation cannot clear an independent cooldown")
}

func TestAccountQualityGateModelScopeResolvesAliases(t *testing.T) {
	for _, gateModel := range []string{"upstream-model", "public-model"} {
		t.Run(gateModel, func(t *testing.T) {
			a := qualityGateTestAccount("model", gateModel)
			require.True(t, a.IsSchedulable(), "model isolation must keep the rest of the account available")
			for _, model := range []string{"public-model", "same-model", "upstream-model"} {
				require.False(t, a.IsSchedulableForModelWithContext(context.Background(), model), model)
			}
			require.True(t, a.IsSchedulableForModel("other-model"))
		})
	}
}

func TestAccountQualityGatePassthroughUsesForwardedModel(t *testing.T) {
	a := qualityGateTestAccount("model", "upstream-model")
	a.Extra["openai_passthrough"] = true
	a.Extra["openai_responses_mode"] = "force_on"
	a.Extra["openai_responses_supported"] = true
	require.True(t, a.IsSchedulableForModel("public-model"), "passthrough does not apply the stale account mapping")
	require.False(t, a.IsSchedulableForModel("upstream-model"))
}

func TestAccountQualityGateOverrideIsPerOwnerAndExpires(t *testing.T) {
	for _, tt := range []struct {
		name, override string
		wantBlocked    bool
	}{
		{"future override", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), false},
		{"expired override", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), true},
		{"invalid override", "not-a-time", true},
		{"no override", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := qualityGateTestAccount("account", "")
			gates := a.Extra["account_quality_gates"].(map[string]any)
			gate := gates["17"].(map[string]any)
			gate["override_until"] = tt.override
			gate["next_probe_at"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			require.Equal(t, !tt.wantBlocked, a.IsSchedulable(), "next probe time never expires an isolation")
			gates["18"] = map[string]any{"scope": "account", "blocked": true}
			require.False(t, a.IsSchedulable(), "one owner's override cannot bypass another owner")
			delete(gates, "17")
			require.False(t, a.IsSchedulable(), "releasing one owner's gate preserves the other")
		})
	}
}

func TestAccountQualityGateAntigravityOveragesDoNotBypassIsolation(t *testing.T) {
	a := qualityGateTestAccount("model", "upstream-model")
	a.Platform = PlatformAntigravity
	a.Extra["allow_overages"] = true
	setAccountModelRateLimitSnapshot(a, "upstream-model", time.Now().Add(time.Hour), "fixture", time.Now())
	require.False(t, a.IsSchedulableForModel("public-model"))
}

func TestAccountQualityGateStickyAndOpenAIAdmission(t *testing.T) {
	a := qualityGateTestAccount("model", "upstream-model")
	require.True(t, shouldClearStickySession(a, "public-model"))
	require.False(t, shouldClearStickySession(a, "other-model"))
	require.False(t, isOpenAICompatibleAccountEligibleForRequest(context.Background(), a, PlatformOpenAI, "public-model", false, ""))
	scheduler := &defaultOpenAIAccountScheduler{}
	compatible, reason := scheduler.isAccountRequestCompatibleReason(context.Background(), a, OpenAIAccountScheduleRequest{RequestedModel: "public-model"})
	require.False(t, compatible)
	require.Equal(t, "quality_isolated", reason)
}

func TestAccountQualityGateDoesNotInvalidateGovernanceConfig(t *testing.T) {
	a := qualityGateTestAccount("model", "upstream-model")
	withGate := GovernanceAccountFingerprint(a)
	delete(a.Extra, "account_quality_gates")
	require.Equal(t, GovernanceAccountFingerprint(a), withGate)
}

func TestAccountQualityGateDuplicateKeepsIsolationOnOriginalOnly(t *testing.T) {
	a := qualityGateTestAccount("account", "")
	extra, err := duplicateAccountExtra(a.Extra)
	require.NoError(t, err)
	require.NotContains(t, extra, "account_quality_gates")
	require.False(t, a.IsSchedulable(), "duplicating configuration does not release the original account")
}

func TestAccountQualityGateInactiveAndMalformedValues(t *testing.T) {
	for _, raw := range []any{nil, []any{true}, "bad", map[string]any{
		"inactive": map[string]any{"scope": "account", "blocked": false},
		"invalid":  map[string]any{"scope": "account", "blocked": "true"},
		"model":    map[string]any{"scope": "model", "blocked": true, "model": ""},
	}} {
		a := qualityGateTestAccount("account", "")
		a.Extra["account_quality_gates"] = raw
		require.True(t, a.IsSchedulable())
		require.True(t, a.IsSchedulableForModel("public-model"))
	}
	var absent *Account
	require.False(t, absent.IsQualityBlocked())
	require.False(t, absent.IsQualityBlockedForModel("public-model"))
}

func TestAccountQualityGateOpenAISchedulersRejectIsolatedStickyModel(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			groupID := int64(78001)
			isolated := qualityGateTestAccount("model", "upstream-model")
			isolated.GroupIDs = []int64{groupID}
			isolated.Concurrency = 1
			healthy := *isolated
			healthy.ID = 43
			healthy.Extra = nil
			healthy.Priority = 9
			svc := newOpenAICompactionSchedulerTestService([]Account{*isolated, healthy}, advanced)
			ctx := context.Background()
			require.NoError(t, svc.setStickySessionAccountID(ctx, &groupID, "quality-session", isolated.ID, time.Hour))
			selected, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "quality-session", "public-model", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, false, true)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, healthy.ID, selected.Account.ID)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
		})
	}
}
