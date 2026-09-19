package service

import (
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func ProvideBillingMaintenanceService(repo UsageBillingRepository, cfg *config.Config) *BillingMaintenanceService {
	service := NewBillingMaintenanceService(repo, cfg)
	service.Start()
	return service
}

func ProvideUsageLogRecoveryService(repo UsageLogOutboxRepository, usage UsageLogRepository) *UsageLogRecoveryService {
	service := NewUsageLogRecoveryService(repo, usage)
	service.Start()
	return service
}

func ProvideBillingStorageMonitor(db *sql.DB, cfg *config.Config) *BillingStorageMonitor {
	monitor := NewBillingStorageMonitor(db, cfg)
	monitor.Start()
	return monitor
}
