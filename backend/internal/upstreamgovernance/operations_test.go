package upstreamgovernance

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func operationsFixtureSite(now time.Time) workbenchSite {
	return workbenchSite{ID: 1, Name: "Fixture", BaseURL: "https://upstream.example.test", Enabled: true, HasCredential: true, Status: "connected", Version: 3, FullSeconds: 60, SnapshotVersion: 3, SnapshotUserID: 1, SnapshotAt: &now, FastEnabled: true, FastSeconds: 5, FastStatus: "healthy", FastAt: &now, FastSourceUserID: 1, FastComplete: true, FastRevision: 1, BalanceEnabled: true, BalanceState: "healthy"}
}

func TestOperationsWorkbenchProjectsCurrentStateNotUnreadHistory(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	site := operationsFixtureSite(now)
	result := projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20, Kind: "all"})
	require.Empty(t, result.Items)
	require.Zero(t, result.Total)
	// This projection deliberately has no Event input: acknowledged/unread
	// historical records cannot resurrect an already-recovered incident.
	site.BalanceState = "low"
	result = projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
	require.Len(t, result.Items, 1)
	require.Equal(t, "balance_low", result.Items[0].Reason)
}

func TestOperationsWorkbenchUnknownAndStaleAreNotHealthy(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	site := operationsFixtureSite(now)
	site.SnapshotAt, site.FastAt = nil, nil
	result := projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
	reasons := []string{}
	for _, item := range result.Items {
		reasons = append(reasons, item.Reason)
	}
	require.ElementsMatch(t, []string{"collection_unknown", "observation_unknown", "balance_unknown"}, reasons)
	old := now.Add(-time.Hour)
	site.SnapshotAt, site.FastAt = &old, &old
	result = projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
	reasons = nil
	for _, item := range result.Items {
		reasons = append(reasons, item.Reason)
	}
	require.ElementsMatch(t, []string{"collection_stale", "observation_stale", "balance_stale"}, reasons)
	site.Enabled = false
	result = projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
	require.Empty(t, result.Items, "disabled collection must not preserve historical collection failures as current work")
}

func TestOperationsWorkbenchFiltersBeforePaginationAndCountsAllItems(t *testing.T) {
	now := time.Now().UTC()
	a, b := operationsFixtureSite(now), operationsFixtureSite(now)
	a.KeyIssues, a.BalanceState = 2, "low"
	b.ID, b.Name, b.KeyIssues = 2, "Other", 1
	result := projectWorkbench(now, []workbenchSite{a, b}, nil, nil, OperationsQuery{Page: 2, PageSize: 1})
	require.Equal(t, int64(3), result.Total)
	require.Equal(t, 2, result.Summary.Critical)
	require.Equal(t, 1, result.Summary.Warning)
	require.Len(t, result.Items, 1)
	require.Equal(t, int64(2), result.Items[0].SiteID)
	result = projectWorkbench(now, []workbenchSite{a, b}, nil, nil, OperationsQuery{SiteID: 2, Page: 1, PageSize: 1})
	require.Equal(t, int64(1), result.Total)
	require.Equal(t, "key", result.Items[0].Kind)
	require.Equal(t, 1, result.Summary.Critical)
}

func TestOperationsTimelineSanitizesEvidenceAndNeverClaimsResolution(t *testing.T) {
	site := operationsFixtureSite(time.Now().UTC())
	record := timelineRecord{ID: "7", Kind: "event", Reason: "rate_changed", Acknowledged: true, ResourceID: "upstream-group", Before: `{"Name":"Group A","Resolved":0.8,"token":"secret-canary"}`, After: `{"Name":"Group A","Resolved":0.9,"smtp":"secret-canary"}`}
	item := projectTimeline(site, record)
	require.Equal(t, "acknowledged", item.Status)
	require.Equal(t, .8, *item.BeforeRate)
	require.Equal(t, .9, *item.AfterRate)
	require.Equal(t, "Group A", item.ResourceName)
	require.Nil(t, item.RelatedRecordID)
	raw, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-canary")
	record.Kind, record.Status, record.HasError = "notification", "sent", true
	item = projectTimeline(site, record)
	require.Equal(t, "sent", item.Status)
	require.Equal(t, "notification_accepted", item.Reason)
	require.Nil(t, item.Acknowledged)
	require.Nil(t, item.BeforeRate)
	require.Nil(t, item.RelatedRecordID)
}

func TestOperationsTimelineUnknownReasonAndMalformedAmountsRemainUnknown(t *testing.T) {
	site := operationsFixtureSite(time.Now().UTC())
	item := projectTimeline(site, timelineRecord{ID: "8", Kind: "event", Reason: "secret-canary", Before: `{"Resolved":"secret-canary"}`, After: `not JSON`})
	require.Equal(t, "event_recorded", item.Reason)
	require.Nil(t, item.BeforeRate)
	require.Nil(t, item.AfterRate)
	raw, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-canary")
}

func TestOperationsWorkbenchSharedProtectionDeduplicatesAndIgnoresDisabledPolicies(t *testing.T) {
	now := time.Now().UTC()
	a, b := operationsFixtureSite(now), operationsFixtureSite(now)
	b.ID = 2
	rows := []pricingWorkbenchRow{{GroupID: 12, GroupName: "Shared", Reason: "unknown_cost", Enabled: true, Protected: true, SiteIDs: []int64{2, 1}, UpdatedAt: now}, {GroupID: 13, GroupName: "Disabled", Enabled: false, Protected: true, SiteIDs: []int64{1}, UpdatedAt: now}}
	result := projectWorkbench(now, []workbenchSite{a, b}, rows, nil, OperationsQuery{Page: 1, PageSize: 20})
	require.Len(t, result.Items, 1)
	require.True(t, result.Items[0].Shared)
	require.Equal(t, 2, result.Items[0].ImpactCount)
	require.Equal(t, int64(1), result.Items[0].SiteID)
	result = projectWorkbench(now, []workbenchSite{b}, rows, nil, OperationsQuery{SiteID: 2, Page: 1, PageSize: 20})
	require.Len(t, result.Items, 1)
	require.True(t, result.Items[0].Shared)
	require.Equal(t, int64(2), result.Items[0].SiteID)
}

func TestOperationsSanitizesURLAndNamedGroupEvidence(t *testing.T) {
	site := operationsFixtureSite(time.Now().UTC())
	site.BaseURL = "https://user:secret-canary@example.test/api?key=secret-canary#secret-canary"
	item := projectTimeline(site, timelineRecord{ID: "9", Kind: "event", Reason: "group_added", ResourceID: "r", After: `{"id":"r","name":"Named group","token":"secret-canary"}`})
	require.Equal(t, "Named group", item.ResourceName)
	require.Equal(t, "https://example.test/api", item.BaseURL)
	raw, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-canary")
}

func TestOperationsFastObservationCannotProveCurrentIdentityAfterConfigurationChange(t *testing.T) {
	now := time.Now().UTC()
	site := operationsFixtureSite(now)
	site.Version++
	result := projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
	found := false
	for _, item := range result.Items {
		if item.Kind == "fast_observation" {
			found = true
			require.Equal(t, "observation_unknown", item.Reason)
		}
	}
	require.True(t, found, "fresh timestamp from an earlier authorization/configuration is not current evidence")
}

func TestOperationsKeyUncertaintyDoesNotDisappearOrClaimConfirmedFailure(t *testing.T) {
	now := time.Now().UTC()
	site := operationsFixtureSite(now)
	site.KeyPending = 1
	site.KeyUnknown = 2
	site.KeyProtectionFailed = 1
	result := projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
	require.Len(t, result.Items, 3)
	reasons := []string{}
	seen := map[string]bool{}
	for _, item := range result.Items {
		require.Equal(t, "key", item.Kind)
		require.NotEqual(t, "critical", item.Severity)
		require.False(t, seen[item.ID])
		seen[item.ID] = true
		reasons = append(reasons, item.Reason)
	}
	require.ElementsMatch(t, []string{"key_verification_pending", "key_verification_unknown", "key_protection_failed"}, reasons)
}

func TestOperationsSentNotificationHasNoFutureRetry(t *testing.T) {
	now := time.Now().UTC()
	item := projectTimeline(operationsFixtureSite(now), timelineRecord{ID: "1", Kind: "notification", Status: "sent", NextAttemptAt: &now, SentAt: &now})
	require.Nil(t, item.NextAttemptAt)
	require.Equal(t, &now, item.SentAt)
}

func TestOperationsFastObservationRequiresCompleteMatchingIdentityEvidence(t *testing.T) {
	now := time.Now().UTC()
	for name, change := range map[string]func(*workbenchSite){
		"source mismatch":           func(s *workbenchSite) { s.FastSourceUserID = 2 },
		"incomplete":                func(s *workbenchSite) { s.FastComplete = false },
		"revision missing":          func(s *workbenchSite) { s.FastRevision = 0 },
		"predates current snapshot": func(s *workbenchSite) { at := now.Add(-time.Second); s.FastAt = &at },
		"unknown snapshot user":     func(s *workbenchSite) { s.SnapshotUserID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			site := operationsFixtureSite(now)
			change(&site)
			result := projectWorkbench(now, []workbenchSite{site}, nil, nil, OperationsQuery{Page: 1, PageSize: 20})
			require.Len(t, result.Items, 1)
			require.Equal(t, "observation_unknown", result.Items[0].Reason)
		})
	}
}
