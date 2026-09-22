package config

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingStorageMonitorDefaultsAndEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, BillingStorageMonitorConfig{
		Enabled: true, IntervalSeconds: 300, ProbeTimeoutSeconds: 5,
		LocalUsedPercent: 85, LocalMinFreeBytes: 2 << 30, DatabaseWarnBytes: 20 << 30,
	}, cfg.BillingStorageMonitor)

	t.Setenv("BILLING_STORAGE_MONITOR_ENABLED", "false")
	t.Setenv("BILLING_STORAGE_MONITOR_INTERVAL_SECONDS", "600")
	t.Setenv("BILLING_STORAGE_MONITOR_PROBE_TIMEOUT_SECONDS", "8")
	t.Setenv("BILLING_STORAGE_MONITOR_LOCAL_USED_PERCENT", "91.5")
	t.Setenv("BILLING_STORAGE_MONITOR_LOCAL_MIN_FREE_BYTES", "4294967296")
	t.Setenv("BILLING_STORAGE_MONITOR_DATABASE_WARN_BYTES", "32212254720")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, BillingStorageMonitorConfig{
		Enabled: false, IntervalSeconds: 600, ProbeTimeoutSeconds: 8,
		LocalUsedPercent: 91.5, LocalMinFreeBytes: 4 << 30, DatabaseWarnBytes: 30 << 30,
	}, cfg.BillingStorageMonitor)
}

func TestBillingStorageMonitorConfigRejectsInvalidValues(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	for _, test := range []struct {
		name, field string
		change      func(*BillingStorageMonitorConfig)
	}{
		{"interval too short", "interval_seconds", func(c *BillingStorageMonitorConfig) { c.IntervalSeconds = 29 }},
		{"interval too long", "interval_seconds", func(c *BillingStorageMonitorConfig) { c.IntervalSeconds = 86401 }},
		{"timeout zero", "probe_timeout_seconds", func(c *BillingStorageMonitorConfig) { c.ProbeTimeoutSeconds = 0 }},
		{"timeout too long", "probe_timeout_seconds", func(c *BillingStorageMonitorConfig) { c.ProbeTimeoutSeconds = 61 }},
		{"timeout exceeds interval", "probe_timeout_seconds", func(c *BillingStorageMonitorConfig) { c.IntervalSeconds = 30; c.ProbeTimeoutSeconds = 31 }},
		{"percent zero", "local_used_percent", func(c *BillingStorageMonitorConfig) { c.LocalUsedPercent = 0 }},
		{"percent excessive", "local_used_percent", func(c *BillingStorageMonitorConfig) { c.LocalUsedPercent = 100.1 }},
		{"percent NaN", "local_used_percent", func(c *BillingStorageMonitorConfig) { c.LocalUsedPercent = math.NaN() }},
		{"percent infinite", "local_used_percent", func(c *BillingStorageMonitorConfig) { c.LocalUsedPercent = math.Inf(1) }},
		{"free bytes zero", "local_min_free_bytes", func(c *BillingStorageMonitorConfig) { c.LocalMinFreeBytes = 0 }},
		{"free bytes negative", "local_min_free_bytes", func(c *BillingStorageMonitorConfig) { c.LocalMinFreeBytes = -1 }},
		{"database bytes zero", "database_warn_bytes", func(c *BillingStorageMonitorConfig) { c.DatabaseWarnBytes = 0 }},
		{"database bytes negative", "database_warn_bytes", func(c *BillingStorageMonitorConfig) { c.DatabaseWarnBytes = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := *cfg
			test.change(&copy.BillingStorageMonitor)
			require.ErrorContains(t, copy.Validate(), "billing_storage_monitor."+test.field)
		})
	}
	t.Setenv("BILLING_STORAGE_MONITOR_DATABASE_WARN_BYTES", "-1")
	_, err = Load()
	require.ErrorContains(t, err, "billing_storage_monitor.database_warn_bytes")
}
