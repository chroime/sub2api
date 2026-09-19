package config

import "fmt"

// Financial evidence is archived rather than purged. Leases and pending work
// are independent of this retention setting and always remain protected.
type BillingMaintenanceConfig struct {
	IntervalSeconds  int `mapstructure:"interval_seconds"`
	BatchSize        int `mapstructure:"batch_size"`
	HotRetentionDays int `mapstructure:"hot_retention_days"`
}

func (c BillingMaintenanceConfig) Validate() error {
	if c.IntervalSeconds < 5 || c.IntervalSeconds > 3600 {
		return fmt.Errorf("billing_maintenance.interval_seconds must be between 5 and 3600")
	}
	if c.BatchSize < 1 || c.BatchSize > 1000 {
		return fmt.Errorf("billing_maintenance.batch_size must be between 1 and 1000")
	}
	if c.HotRetentionDays < 7 || c.HotRetentionDays > 3650 {
		return fmt.Errorf("billing_maintenance.hot_retention_days must be between 7 and 3650")
	}
	return nil
}
