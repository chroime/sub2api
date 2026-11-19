package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorUsageMetricsIndexMatchesAggregateQuery(t *testing.T) {
	sql, err := FS.ReadFile("293_channel_monitor_usage_metrics_index_notx.sql")
	require.NoError(t, err)
	require.Contains(t, string(sql), "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_group_effective_model_created")
	require.Contains(t, string(sql), "group_id,")
	require.Contains(t, string(sql), "COALESCE(NULLIF(BTRIM(requested_model), ''), NULLIF(BTRIM(model), ''))")
	require.Contains(t, string(sql), "created_at")
	require.Contains(t, string(sql), "actual_cost > 0")
	require.True(t, strings.HasSuffix(strings.TrimSpace(string(sql)), ";"))
}
