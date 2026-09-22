package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingMaintenanceConfigDefaultsAndBounds(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, BillingMaintenanceConfig{IntervalSeconds: 30, BatchSize: 500, HotRetentionDays: 90}, cfg.BillingMaintenance)
	for _, test := range []struct {
		name  string
		value BillingMaintenanceConfig
		field string
	}{
		{"interval too short", BillingMaintenanceConfig{4, 500, 90}, "interval_seconds"},
		{"interval too long", BillingMaintenanceConfig{3601, 500, 90}, "interval_seconds"},
		{"empty batch", BillingMaintenanceConfig{30, 0, 90}, "batch_size"},
		{"oversized batch", BillingMaintenanceConfig{30, 1001, 90}, "batch_size"},
		{"retention too short", BillingMaintenanceConfig{30, 500, 6}, "hot_retention_days"},
		{"retention too long", BillingMaintenanceConfig{30, 500, 3651}, "hot_retention_days"},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := *cfg
			copy.BillingMaintenance = test.value
			require.ErrorContains(t, copy.Validate(), "billing_maintenance."+test.field)
		})
	}
}
