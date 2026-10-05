package service

import (
	"encoding/json"
	"testing"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

func TestGovernanceReconciliationIdentityExcludesUnownedSettings(t *testing.T) {
	rate := 0.8
	a := &Account{ID: 3, Name: "import", Platform: "openai", Type: "apikey", Credentials: map[string]any{"api_key": "secret-canary", "base_url": "https://up.example"}, Extra: map[string]any{governanceMarkerKey: "owned"}, RateMultiplier: &rate, Status: StatusActive, Schedulable: true}
	before := GovernanceManagedAccount(a)
	a.Priority = 8
	a.Concurrency = 5000
	a.GroupIDs = []int64{7, 9}
	a.Extra["quota_used"] = 42
	a.Credentials["model_mapping"] = map[string]any{"old": "new"}
	require.Equal(t, before.Identity, GovernanceManagedAccount(a).Identity)
	raw, err := json.Marshal(GovernanceManagedAccount(a))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-canary")
	a.Credentials["api_key"] = "changed"
	require.NotEqual(t, before.Identity, GovernanceManagedAccount(a).Identity)
}
func TestGovernanceCatalogRateExcludesNativeSnapshotFromScheduling(t *testing.T) {
	now := time.Now()
	rate := 0.5
	a := &Account{ID: 3, Platform: "openai", Type: "apikey", RateMultiplier: &rate, Extra: map[string]any{gov.GovernanceRateOwnerExtraKey: "owned", UpstreamBillingProbeExtraKey: &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusOK, ReceivedAt: probeTimePtr(now.Add(-time.Minute)), FreshUntil: probeTimePtr(now.Add(time.Minute)), Data: map[string]any{"billing_scope": "token", "peak_rate_enabled": false, "resolved_rate_multiplier": 9.0}}}}
	require.True(t, GovernanceCatalogOwnsRate(a))
	_, ok := openAIFreshUpstreamBillingRate(a, now)
	require.False(t, ok)
	delete(a.Extra, gov.GovernanceRateOwnerExtraKey)
	value, ok := openAIFreshUpstreamBillingRate(a, now)
	require.True(t, ok)
	require.Equal(t, 9.0, value)
}

func TestGovernanceCatalogOwnershipKeepsNativeProbeObservational(t *testing.T) {
	rate := 0.25
	a := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, RateMultiplier: &rate, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://upstream.example"}, Extra: map[string]any{gov.GovernanceRateOwnerExtraKey: "owned", UpstreamBillingProbeEnabledExtraKey: true, UpstreamBillingRateSyncEnabledExtraKey: true}}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{a.ID: a}}
	svc := newUpstreamBillingProbeTestService(repo, &upstreamBillingProbeHTTPStub{}, &upstreamBillingProbeSettingRepo{})
	snapshot, err := svc.ProbeAccount(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Nil(t, snapshot.SyncedRateMultiplier)
	require.Equal(t, 0.25, a.BillingRateMultiplier())
	require.Contains(t, a.Extra, UpstreamBillingProbeExtraKey)
}
