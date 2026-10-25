package accountquality

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid account quality configuration")
	ErrNotFound = errors.New("account quality item not found")
	ErrConflict = errors.New("account quality version changed; reload and retry")
	ErrBusy     = errors.New("an account quality check is already queued or running")
	ErrBudget   = errors.New("account quality diagnostic budget exhausted")
)

type Policy struct {
	ID                      int64     `json:"id"`
	Name                    string    `json:"name"`
	Version                 int64     `json:"version"`
	Enabled                 bool      `json:"enabled"`
	Platform                string    `json:"platform"`
	Model                   string    `json:"model"`
	APIMode                 string    `json:"api_mode"`
	Effort                  string    `json:"effort"`
	Templates               []string  `json:"templates"`
	MaxOutputTokens         int       `json:"max_output_tokens"`
	TimeoutSeconds          int       `json:"timeout_seconds"`
	IntervalSeconds         int       `json:"interval_seconds"`
	ConfirmSeconds          int       `json:"confirm_seconds"`
	FailureThreshold        int       `json:"failure_threshold"`
	RecoveryThreshold       int       `json:"recovery_threshold"`
	CooldownSeconds         int       `json:"cooldown_seconds"`
	RecoveryIntervalSeconds int       `json:"recovery_interval_seconds"`
	DailyRequestLimit       int       `json:"daily_request_limit"`
	ControlMode             string    `json:"control_mode"`
	IsolationScope          string    `json:"isolation_scope"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type Overrides struct {
	IntervalSeconds   int    `json:"interval_seconds,omitempty"`
	FailureThreshold  int    `json:"failure_threshold,omitempty"`
	RecoveryThreshold int    `json:"recovery_threshold,omitempty"`
	IsolationScope    string `json:"isolation_scope,omitempty"`
}

type Binding struct {
	ID                  int64      `json:"id"`
	Version             int64      `json:"version"`
	AccountID           int64      `json:"account_id"`
	AccountName         string     `json:"account_name"`
	AccountType         string     `json:"account_type"`
	Platform            string     `json:"platform"`
	PolicyID            int64      `json:"policy_id"`
	PolicyName          string     `json:"policy_name"`
	PolicyVersion       int64      `json:"policy_version"`
	Model               string     `json:"model"`
	ResolvedModel       string     `json:"resolved_model,omitempty"`
	AutoIsolate         bool       `json:"auto_isolate"`
	AutoControlEligible bool       `json:"auto_control_eligible"`
	ControlMode         string     `json:"control_mode"`
	IntervalSeconds     int        `json:"interval_seconds"`
	FailureThreshold    int        `json:"failure_threshold"`
	RecoveryThreshold   int        `json:"recovery_threshold"`
	IsolationScope      string     `json:"isolation_scope"`
	Source              string     `json:"source"`
	Overrides           Overrides  `json:"overrides"`
	State               string     `json:"state"`
	Failures            int        `json:"failures"`
	RecoveryPasses      int        `json:"recovery_passes"`
	NextRunAt           time.Time  `json:"next_run_at"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastDurationMS      int64      `json:"last_duration_ms"`
	QualityVerdict      string     `json:"quality_verdict"`
	ExecutionStatus     string     `json:"execution_status"`
	Reason              string     `json:"reason"`
	Isolated            bool       `json:"isolated"`
	OverrideUntil       *time.Time `json:"override_until,omitempty"`
	IdentityVersion     string     `json:"identity_version,omitempty"`
	RecentRuns          []Run      `json:"recent_runs"`
}

type Run struct {
	AdapterVersion      string     `json:"adapter_version"`
	ActualAccountID     int64      `json:"actual_account_id"`
	CancelRequested     bool       `json:"cancel_requested,omitempty"`
	ID                  string     `json:"id"`
	RequestID           string     `json:"request_id"`
	BindingID           int64      `json:"binding_id"`
	AccountID           int64      `json:"account_id"`
	AccountName         string     `json:"account_name"`
	PolicyID            int64      `json:"policy_id"`
	PolicyVersion       int64      `json:"policy_version"`
	BindingVersion      int64      `json:"binding_version"`
	Model               string     `json:"model"`
	Template            string     `json:"template"`
	TemplateVersion     string     `json:"template_version"`
	Prompt              string     `json:"prompt"`
	Expected            string     `json:"expected"`
	ResponseText        string     `json:"response_text"`
	ResultPreview       string     `json:"result_preview,omitempty"`
	RequestedModel      string     `json:"requested_model"`
	MappedModel         string     `json:"mapped_model"`
	ReturnedModel       string     `json:"returned_model"`
	APIMode             string     `json:"api_mode"`
	Effort              string     `json:"effort"`
	MaxOutputTokens     int        `json:"max_output_tokens"`
	OutputLimitMode     string     `json:"output_limit_mode"`
	ExecutionStatus     string     `json:"execution_status"`
	QualityVerdict      string     `json:"quality_verdict"`
	Reason              string     `json:"reason"`
	FinishReason        string     `json:"finish_reason"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	DurationMS          int64      `json:"duration_ms"`
	InputTokens         int64      `json:"input_tokens"`
	OutputTokens        int64      `json:"output_tokens"`
	ReasoningTokens     int64      `json:"reasoning_tokens"`
	HTTPStatus          int        `json:"http_status"`
	ErrorClass          string     `json:"error_class"`
	AutoControlEligible bool       `json:"auto_control_eligible"`
	Stale               bool       `json:"stale"`
	Config              Policy     `json:"config"`
	IdentityVersion     string     `json:"identity_version"`
	CredentialAccountID int64      `json:"credential_account_id"`
	UpstreamRequestID   string     `json:"upstream_request_id"`
	CostUSD             *float64   `json:"cost_usd,omitempty"`
	ReservedCostUSD     float64    `json:"reserved_cost_usd"`
	ActorID             int64      `json:"actor_id"`
	Lease               string     `json:"-"`
}

type Action struct {
	ID        string    `json:"id"`
	AccountID int64     `json:"account_id"`
	BindingID int64     `json:"binding_id"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	ActorID   int64     `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

type NotificationSettings struct {
	EmailEnabled    bool     `json:"email_enabled"`
	EmailRecipients []string `json:"email_recipients"`
	QQEnabled       bool     `json:"qq_enabled"`
	QQTargets       []string `json:"qq_targets"`
	Events          []string `json:"events"`
	ReminderSeconds int      `json:"reminder_seconds"`
}

type Settings struct {
	Version           int64                `json:"version"`
	Concurrency       int                  `json:"concurrency"`
	DailyRequestLimit int                  `json:"daily_request_limit"`
	DailyCostLimitUSD float64              `json:"daily_cost_limit_usd"`
	Notifications     NotificationSettings `json:"notifications"`
}

func DefaultSettings() Settings {
	return Settings{Version: 1, Concurrency: 2, DailyRequestLimit: 2000, Notifications: NotificationSettings{EmailRecipients: []string{}, QQTargets: []string{}, Events: []string{"suspected", "quarantined", "recovered", "manual", "budget_paused"}, ReminderSeconds: 3600}}
}

type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}
type Capability struct {
	Platform         string   `json:"platform"`
	APIMode          string   `json:"api_mode"`
	AccountTypes     []string `json:"account_types"`
	AutomaticControl bool     `json:"automatic_control"`
	OutputLimitMode  string   `json:"output_limit_mode"`
	Reason           string   `json:"reason"`
}
type ProbeRequest struct {
	// Manual is derived from the authenticated run actor by the worker. It is
	// never accepted as a downstream account-selection or bypass parameter.
	Manual                          bool
	AccountID, BindingID            int64
	Model, APIMode, Prompt, Effort  string
	MaxOutputTokens, TimeoutSeconds int
}
type ProbeResult struct {
	AccountID, CredentialAccountID                                                                                                                                                  int64
	IdentityVersion, RequestedModel, MappedModel, ReturnedModel, APIMode, Effort, OutputLimitMode, Text, FinishReason, ExecutionStatus, ErrorClass, ErrorMessage, UpstreamRequestID string
	MaxOutputTokens, HTTPStatus                                                                                                                                                     int
	InputTokens, OutputTokens, ReasoningTokens, LatencyMS                                                                                                                           int64
	CostUSD                                                                                                                                                                         *float64
	AutoControlEligible                                                                                                                                                             bool
}
type Prober interface {
	ProbeAccountText(context.Context, ProbeRequest) (ProbeResult, error)
}
type ControlInput struct {
	Version         int64  `json:"version"`
	Action          string `json:"action"`
	Reason          string `json:"reason"`
	DurationMinutes int    `json:"duration_minutes"`
	AutoIsolate     *bool  `json:"auto_isolate"`
}
type BindInput struct {
	PolicyID    int64     `json:"policy_id"`
	AccountIDs  []int64   `json:"account_ids"`
	AutoIsolate bool      `json:"auto_isolate"`
	Source      string    `json:"source"`
	Overrides   Overrides `json:"overrides"`
}
type Overview struct {
	Policies     []Policy     `json:"policies"`
	Bindings     []Binding    `json:"bindings"`
	Templates    []Template   `json:"templates"`
	Settings     Settings     `json:"settings"`
	Capabilities []Capability `json:"capabilities"`
}
type AccountDetail struct {
	Bindings []Binding `json:"bindings"`
	Runs     []Run     `json:"runs"`
	Actions  []Action  `json:"actions"`
}
