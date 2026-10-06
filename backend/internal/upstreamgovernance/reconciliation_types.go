package upstreamgovernance

import (
	"context"
	"time"
)

const (
	GovernanceRateOwnerExtraKey = "upstream_governance_rate_owner"
	GovernancePauseExtraKey     = "upstream_governance_pause"
	GovernanceReceiptExtraKey   = "upstream_governance_reconcile_receipt"
)

type AutomationPolicy struct {
	Enabled                bool    `json:"enabled"`
	SyncRate               bool    `json:"sync_rate"`
	SyncName               bool    `json:"sync_name"`
	PauseMissing           bool    `json:"pause_missing"`
	RestoreReturned        bool    `json:"restore_returned"`
	MissingConfirmations   int     `json:"missing_confirmations"`
	MaxRateIncreasePercent float64 `json:"max_rate_increase_percent"`
}
type AutomationConfig struct {
	Version int64            `json:"version"`
	Policy  AutomationPolicy `json:"policy"`
}

func DefaultAutomationConfig() AutomationConfig {
	return AutomationConfig{Version: 0, Policy: AutomationPolicy{SyncRate: true, SyncName: true, PauseMissing: true, RestoreReturned: true, MissingConfirmations: 2, MaxRateIncreasePercent: 20}}
}

type ReconciliationChange struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
type ReconciliationRow struct {
	BindingID       int64                  `json:"binding_id"`
	AccountID       int64                  `json:"account_id"`
	RemoteGroupID   string                 `json:"remote_group_id"`
	RemoteGroupName string                 `json:"remote_group_name"`
	AccountName     string                 `json:"account_name"`
	Action          string                 `json:"action"`
	State           string                 `json:"state"`
	Reason          string                 `json:"reason"`
	Changes         []ReconciliationChange `json:"changes"`
	Patch           ManagedAccountPatch    `json:"-"`
	Management      ReconciliationState    `json:"-"`
}
type Reconciliation struct {
	SnapshotID int64               `json:"snapshot_id"`
	ObservedAt *time.Time          `json:"observed_at"`
	Rows       []ReconciliationRow `json:"rows"`
}
type ReconciliationPreview struct {
	Reconciliation
	ID            string                     `json:"id"`
	SiteVersion   int64                      `json:"site_version"`
	ExpiresAt     time.Time                  `json:"expires_at"`
	PolicyVersion int64                      `json:"-"`
	Items         []ReconciliationFrozenItem `json:"-"`
	Result        *ReconciliationResult      `json:"-"`
}

// Private frozen data is persisted separately from the public response.
type ReconciliationFrozenItem struct {
	Row        ReconciliationRow   `json:"row"`
	Patch      ManagedAccountPatch `json:"patch"`
	Management ReconciliationState `json:"management"`
}
type ReconciliationItemResult struct {
	BindingID int64  `json:"binding_id"`
	AccountID int64  `json:"account_id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}
type ReconciliationResult struct {
	PreviewID string                     `json:"preview_id"`
	Items     []ReconciliationItemResult `json:"items"`
}
type ReconciliationState struct {
	BindingID      int64      `json:"binding_id"`
	Identity       string     `json:"identity"`
	Name           string     `json:"name"`
	Rate           float64    `json:"rate"`
	NativeRateSync bool       `json:"native_rate_sync"`
	RemoteName     string     `json:"remote_name"`
	LastSnapshotID int64      `json:"last_snapshot_id"`
	LastMissingAt  *time.Time `json:"last_missing_at,omitempty"`
	MissingCount   int        `json:"missing_count"`
}

// Only non-secret identity hashes and managed fields leave the native adapter.
type ManagedLocalAccount struct {
	ID             int64   `json:"id"`
	Identity       string  `json:"identity"`
	Name           string  `json:"name"`
	Rate           float64 `json:"rate"`
	Status         string  `json:"status"`
	Schedulable    bool    `json:"schedulable"`
	CanRestore     bool    `json:"can_restore"`
	NativeRateSync bool    `json:"native_rate_sync"`
	RateOwner      string  `json:"rate_owner"`
	PauseToken     string  `json:"pause_token"`
	PauseMarker    string  `json:"pause_marker"`
	PauseIdentity  string  `json:"pause_identity"`
	PauseReason    string  `json:"pause_reason"`
	Receipt        string  `json:"receipt"`
}
type ManagedAccountPatch struct {
	BindingID    int64               `json:"binding_id"`
	Marker       string              `json:"marker"`
	OperationID  string              `json:"operation_id"`
	Expected     ManagedLocalAccount `json:"expected"`
	Name         *string             `json:"name,omitempty"`
	Rate         *float64            `json:"rate,omitempty"`
	RateOwner    *string             `json:"rate_owner,omitempty"`
	Availability string              `json:"availability,omitempty"` // pause | restore
	PauseReason  string              `json:"pause_reason,omitempty"`
}
type ReconciliationLocal interface {
	InspectManagedAccount(context.Context, Binding) (*ManagedLocalAccount, error)
	ApplyManagedPatch(context.Context, ManagedAccountPatch) (*ManagedLocalAccount, error)
}
type ReconciliationStore interface {
	GetAutomation(context.Context, int64) (AutomationConfig, error)
	SaveAutomation(context.Context, int64, AutomationConfig) (AutomationConfig, error)
	ReconciliationStates(context.Context, int64) (map[int64]ReconciliationState, error)
	SaveReconciliationState(context.Context, int64, ReconciliationState) error
	SaveReconciliationPreview(context.Context, int64, *ReconciliationPreview) error
	GetReconciliationPreview(context.Context, int64, string) (*ReconciliationPreview, error)
	SaveReconciliationResult(context.Context, int64, string, *ReconciliationResult) error
}
