//go:build unit

package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserMonitorPublicProbeValidityProjection(t *testing.T) {
	deadline := time.Date(2026, 11, 14, 9, 2, 0, 0, time.UTC)
	raw, err := json.Marshal(userMonitorViewToItem(&service.UserMonitorView{ID: 4, ProbeValidUntil: &deadline}, false))
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	require.Equal(t, deadline.Format(time.RFC3339), data["probe_valid_until"])
	for _, key := range []string{"api_key", "api_key_ciphertext", "credentials", "account_id"} {
		require.NotContains(t, data, key)
	}
	raw, err = json.Marshal(userMonitorViewToItem(&service.UserMonitorView{ID: 4}, false))
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"probe_valid_until"`)
}
