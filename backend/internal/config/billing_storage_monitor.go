package config

import (
	"fmt"
	"math"

	"github.com/spf13/viper"
)

// BillingStorageMonitorConfig controls informational capacity warnings only.
// DatabaseWarnBytes measures the current database, not the DB host's free disk.
type BillingStorageMonitorConfig struct {
	Enabled             bool    `mapstructure:"enabled"`
	IntervalSeconds     int     `mapstructure:"interval_seconds"`
	ProbeTimeoutSeconds int     `mapstructure:"probe_timeout_seconds"`
	LocalUsedPercent    float64 `mapstructure:"local_used_percent"`
	LocalMinFreeBytes   int64   `mapstructure:"local_min_free_bytes"`
	DatabaseWarnBytes   int64   `mapstructure:"database_warn_bytes"`
}

func DefaultBillingStorageMonitorConfig() BillingStorageMonitorConfig {
	return BillingStorageMonitorConfig{
		Enabled: true, IntervalSeconds: 300, ProbeTimeoutSeconds: 5,
		LocalUsedPercent: 85, LocalMinFreeBytes: 2 << 30, DatabaseWarnBytes: 20 << 30,
	}
}

func setBillingStorageMonitorDefaults() {
	c := DefaultBillingStorageMonitorConfig()
	// Viper needs a known key for AutomaticEnv to participate in Unmarshal.
	viper.SetDefault("billing_storage_monitor.enabled", c.Enabled)
	viper.SetDefault("billing_storage_monitor.interval_seconds", c.IntervalSeconds)
	viper.SetDefault("billing_storage_monitor.probe_timeout_seconds", c.ProbeTimeoutSeconds)
	viper.SetDefault("billing_storage_monitor.local_used_percent", c.LocalUsedPercent)
	viper.SetDefault("billing_storage_monitor.local_min_free_bytes", c.LocalMinFreeBytes)
	viper.SetDefault("billing_storage_monitor.database_warn_bytes", c.DatabaseWarnBytes)
}

func (c BillingStorageMonitorConfig) Validate() error {
	if c.IntervalSeconds < 30 || c.IntervalSeconds > 86400 {
		return fmt.Errorf("billing_storage_monitor.interval_seconds must be between 30 and 86400")
	}
	if c.ProbeTimeoutSeconds < 1 || c.ProbeTimeoutSeconds > 60 || c.ProbeTimeoutSeconds > c.IntervalSeconds {
		return fmt.Errorf("billing_storage_monitor.probe_timeout_seconds must be between 1 and 60 and no greater than interval_seconds")
	}
	if math.IsNaN(c.LocalUsedPercent) || math.IsInf(c.LocalUsedPercent, 0) || c.LocalUsedPercent <= 0 || c.LocalUsedPercent > 100 {
		return fmt.Errorf("billing_storage_monitor.local_used_percent must be finite and greater than 0 and at most 100")
	}
	if c.LocalMinFreeBytes <= 0 {
		return fmt.Errorf("billing_storage_monitor.local_min_free_bytes must be greater than 0")
	}
	if c.DatabaseWarnBytes <= 0 {
		return fmt.Errorf("billing_storage_monitor.database_warn_bytes must be greater than 0")
	}
	return nil
}
