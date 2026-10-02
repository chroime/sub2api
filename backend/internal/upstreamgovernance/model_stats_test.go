package upstreamgovernance

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelStatsSeparatesEffortVersionsAndUsesWholeTimeWindow(t *testing.T) {
	f := newModelEngineFixture(t)
	create := func(requestID, effort, version string, values []int64, age time.Duration) {
		t.Helper()
		c := f.config
		c.Samples = len(values)
		c.Efforts = []string{effort}
		batch := f.batch(t, requestID, c)
		for _, value := range values {
			claim, err := f.store.claim(t.Context(), "stats-owner", time.Now())
			require.NoError(t, err)
			require.NotNil(t, claim)
			result := ModelRunResult{Success: true, TemplateVersion: version, AdapterVersion: "adapter-1", TTFTMS: &value, DurationMS: value + 100, ResponseText: "original should not appear in statistics"}
			require.NoError(t, f.store.finish(t.Context(), claim.Run.ID, "stats-owner", "succeeded", result, time.Now()))
		}
		if age > 0 {
			_, err := f.db.Exec(`UPDATE upstream_governance_model_runs SET created_at=$2 WHERE batch_id=$1`, batch.ID, time.Now().Add(-age))
			require.NoError(t, err)
		}
	}
	create("same-baseline", "medium", "candy-v1", []int64{100, 200, 300, 400, 500}, 0)
	create("other-effort", "high", "candy-v1", []int64{900}, 0)
	create("older-version", "medium", "candy-v0", []int64{800}, 2*24*time.Hour)
	create("unversioned", "medium", "", []int64{700}, 0)
	create("outside-window", "low", "candy-v1", []int64{600}, 31*24*time.Hour)
	stats, err := f.service.ModelStats(t.Context(), f.site.ID, 7)
	require.NoError(t, err)
	require.Len(t, stats.Groups, 4)
	var all int64
	for _, group := range stats.Groups {
		all += group.Samples
		require.Len(t, group.Points, 1)
		require.Equal(t, group.Samples, group.Points[0].Samples)
		if group.TemplateVersion == "candy-v1" && group.Effort == "medium" {
			require.EqualValues(t, 5, group.TTFTSamples)
			require.NotNil(t, group.P50TTFTMS)
			require.InDelta(t, 300, *group.P50TTFTMS, 0.001)
			require.InDelta(t, 480, *group.P95TTFTMS, 0.001)
		} else {
			require.Nil(t, group.P50TTFTMS)
			require.Nil(t, group.P95TTFTMS)
		}
		if group.TemplateVersion == "" {
			require.False(t, group.Comparable)
			require.Nil(t, group.Points[0].TTFTMS)
		}
	}
	require.EqualValues(t, 8, all)
	raw, err := json.Marshal(stats)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "original should not appear")
	require.NotContains(t, string(raw), "key_hash")
	day, err := f.service.ModelStats(t.Context(), f.site.ID, 1)
	require.NoError(t, err)
	require.Len(t, day.Groups, 3)
	_, err = f.service.ModelStats(t.Context(), f.site.ID, 31)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = f.service.ModelStats(t.Context(), 999, 1)
	require.ErrorIs(t, err, ErrNotFound)
}
