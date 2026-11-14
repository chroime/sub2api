//go:build unit

package handler

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMonitorIntelligencePublicProjectionHasOnlyAggregates(t *testing.T) {
	now := time.Now().UTC()
	item := userMonitorViewToItem(&service.UserMonitorView{ID: 4, Intelligence: &service.MonitorIntelligence{
		WindowEnd: now, Series: []service.MonitorIntelligenceSeries{{Model: "local-model", Effort: "medium", Points: []service.MonitorIntelligencePoint{{ID: "opaque-check-id", Status: "pending", CheckedAt: now}}}},
	}}, false)
	raw, err := json.Marshal(item)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	intel := data["intelligence"].(map[string]any)
	require.Len(t, intel, 2)
	require.Contains(t, intel, "window_end")
	series := intel["series"].([]any)[0].(map[string]any)
	require.Len(t, series, 3)
	require.Equal(t, "local-model", series["model"])
	require.Equal(t, "medium", series["effort"])
	points := series["points"].([]any)
	require.Len(t, points[0].(map[string]any), 3)
	require.Equal(t, "opaque-check-id", points[0].(map[string]any)["id"])
	for _, key := range []string{"account_id", "account_name", "prompt", "response_text", "credentials", "api_key"} {
		require.NotContains(t, string(raw), `"`+key+`"`)
	}
}

func TestMonitorIntelligenceUnconfiguredIsOmitted(t *testing.T) {
	raw, err := json.Marshal(userMonitorViewToItem(&service.UserMonitorView{ID: 4}, false))
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"intelligence"`)
}
