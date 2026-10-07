package upstreamgovernance

import (
	"context"
	"encoding/json"
	"time"
)

// ModelTestConfig is snapshotted on every batch; credentials are referenced, never copied.
type ModelTestConfig struct {
	// TargetType selects the credential source for a model test. An omitted
	// value keeps the historical upstream-governance target behavior.
	TargetType                 string   `json:"target_type,omitempty"`
	ManagedKeyID               int64    `json:"managed_key_id"`
	LocalGroupID               int64    `json:"local_group_id,omitempty"`
	LocalAPIKeyID              int64    `json:"local_api_key_id,omitempty"`
	TargetOwnerUserID          int64    `json:"target_owner_user_id,omitempty"`
	Platform                   string   `json:"platform"`
	Model                      string   `json:"model"`
	APIMode                    string   `json:"api_mode"`
	Efforts                    []string `json:"efforts"`
	Templates                  []string `json:"templates"`
	Samples                    int      `json:"samples"`
	Concurrency                int      `json:"concurrency"`
	MaxOutputTokens            int      `json:"max_output_tokens"`
	TimeoutSeconds             int      `json:"timeout_seconds"`
	FirstContentTimeoutSeconds int      `json:"first_content_timeout_seconds,omitempty"`
	IdleTimeoutSeconds         int      `json:"idle_timeout_seconds,omitempty"`
	InputTokens                int      `json:"input_tokens"`
	Tokenizer                  string   `json:"tokenizer"`
	TokenTolerancePercent      float64  `json:"token_tolerance_percent"`
}

type ModelPolicy struct {
	ID                int64           `json:"id"`
	SiteID            int64           `json:"site_id"`
	Name              string          `json:"name"`
	Config            ModelTestConfig `json:"config"`
	Enabled           bool            `json:"enabled"`
	IntervalMinutes   int             `json:"interval_minutes"`
	DailyRequestLimit int             `json:"daily_request_limit"`
	NotifyEnabled     bool            `json:"notify_enabled"`
	Recipients        []string        `json:"recipients"`
	FailureThreshold  int             `json:"failure_threshold"`
	TakeOverLegacy    bool            `json:"take_over_legacy"`
	Version           int64           `json:"version"`
	NextRunAt         time.Time       `json:"next_run_at"`
	LastError         string          `json:"last_error"`
	NotifyError       string          `json:"notify_error"`
	NotifyAt          *time.Time      `json:"notify_at"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type ModelBatchInput struct {
	RequestID string          `json:"request_id"`
	PolicyID  int64           `json:"policy_id"`
	Config    ModelTestConfig `json:"config"`
}

type ModelBatch struct {
	ID          string `json:"id"`
	Total       int    `json:"total"`
	Concurrency int    `json:"concurrency"`
}

type ModelRunRequest struct {
	Config   ModelTestConfig `json:"config"`
	Template string          `json:"template"`
	Effort   string          `json:"effort"`
	Sample   int             `json:"sample"`
}

type ModelUsage struct {
	InputTokens         *int64 `json:"input_tokens"`
	OutputTokens        *int64 `json:"output_tokens"`
	CachedTokens        *int64 `json:"cached_tokens"`
	CacheCreationTokens *int64 `json:"cache_creation_tokens"`
	ReasoningTokens     *int64 `json:"reasoning_tokens"`
}

type ModelMarkerResult struct {
	Position string `json:"position"`
	Label    string `json:"label"`
	Expected string `json:"expected"`
	Found    bool   `json:"found"`
}

type ModelTokenAudit struct {
	Tokenizer             string              `json:"tokenizer"`
	Source                string              `json:"source"`
	InputLocal            *int64              `json:"input_local"`
	InputDeclaredTotal    *int64              `json:"input_declared_total"`
	InputOverheadMargin   int64               `json:"input_overhead_margin"`
	OutputLocal           *int64              `json:"output_local"`
	OutputDeclaredVisible *int64              `json:"output_declared_visible"`
	InputState            string              `json:"input_state"`
	OutputState           string              `json:"output_state"`
	InputDeltaPercent     *float64            `json:"input_delta_percent"`
	OutputDeltaPercent    *float64            `json:"output_delta_percent"`
	InputHash             string              `json:"input_hash"`
	OutputHash            string              `json:"output_hash"`
	InputBytes            int                 `json:"input_bytes"`
	OutputBytes           int                 `json:"output_bytes"`
	InputChars            int                 `json:"input_chars"`
	OutputChars           int                 `json:"output_chars"`
	MarkersPassed         *bool               `json:"markers_passed,omitempty"`
	Markers               []ModelMarkerResult `json:"markers,omitempty"`
	EchoExpected          string              `json:"echo_expected,omitempty"`
	EchoPassed            *bool               `json:"echo_passed,omitempty"`
	InputRequestedTokens  int                 `json:"input_requested_tokens,omitempty"`
	TokenizerVersion      string              `json:"tokenizer_version,omitempty"`
	Note                  string              `json:"note"`
}

// ModelSecuritySummary is a compact, credential-free assessment of a model
// run. It deliberately contains states and measurements rather than raw
// prompts, responses, request bodies, usage payloads or HTML artifacts.
type ModelSecuritySummary struct {
	TrustState           string   `json:"trust_state"`
	TrustLabel           string   `json:"trust_label"`
	Summary              string   `json:"summary"`
	TokenDifference      bool     `json:"token_difference"`
	TokenInputState      string   `json:"token_input_state,omitempty"`
	TokenOutputState     string   `json:"token_output_state,omitempty"`
	InputDeltaPercent    *float64 `json:"input_delta_percent,omitempty"`
	OutputDeltaPercent   *float64 `json:"output_delta_percent,omitempty"`
	CandyVerdict         string   `json:"candy_verdict,omitempty"`
	CandyVerdictLabel    string   `json:"candy_verdict_label,omitempty"`
	PelicanHTMLAvailable bool     `json:"pelican_html_available"`
	PelicanHTMLBytes     int      `json:"pelican_html_bytes,omitempty"`
}

type ModelRunResult struct {
	TemplateVersion string               `json:"template_version"`
	AdapterVersion  string               `json:"adapter_version"`
	Success         bool                 `json:"success"`
	ErrorCode       string               `json:"error_code"`
	HTTPStatus      int                  `json:"http_status"`
	ResponseModel   string               `json:"response_model"`
	EffortSupport   string               `json:"effort_support"`
	SentParameters  map[string]any       `json:"sent_parameters,omitempty"`
	RequestBody     json.RawMessage      `json:"request_body,omitempty"`
	InputText       string               `json:"input_text,omitempty"`
	ResponseText    string               `json:"response_text,omitempty"`
	HTML            string               `json:"html,omitempty"`
	FinishReason    string               `json:"finish_reason"`
	Streamed        bool                 `json:"streamed"`
	Completed       bool                 `json:"completed"`
	CandyAnswer     *int                 `json:"candy_answer,omitempty"`
	TTFTMS          *int64               `json:"ttft_ms"`
	DurationMS      int64                `json:"duration_ms"`
	Usage           ModelUsage           `json:"usage"`
	RawUsage        json.RawMessage      `json:"raw_usage,omitempty"`
	Tokens          ModelTokenAudit      `json:"tokens"`
	CandyVerdict    string               `json:"candy_verdict"`
	Security        ModelSecuritySummary `json:"security"`
}

type ModelRun struct {
	ID            string          `json:"id"`
	BatchID       string          `json:"batch_id"`
	SiteID        int64           `json:"site_id"`
	PolicyID      *int64          `json:"policy_id"`
	Sequence      int             `json:"sequence"`
	Request       ModelRunRequest `json:"request"`
	Status        string          `json:"status"`
	Result        *ModelRunResult `json:"result,omitempty"`
	Review        string          `json:"review"`
	ReviewNote    string          `json:"review_note"`
	ReviewerID    *int64          `json:"reviewer_id"`
	ReviewedAt    *time.Time      `json:"reviewed_at"`
	ReviewVersion int64           `json:"review_version"`
	CreatedAt     time.Time       `json:"created_at"`
	StartedAt     *time.Time      `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

type ModelRunPage struct {
	Items    []ModelRun       `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Counts   map[string]int64 `json:"counts"`
}

type ModelRunner interface {
	RunModel(context.Context, Site, RemoteKey, ModelRunRequest) (ModelRunResult, error)
}

// LocalModelTarget is a server-side snapshot of a local gateway API key and
// its group. Key is intentionally omitted from JSON responses and only lives
// in memory for the duration of a worker request.
type LocalModelTarget struct {
	GroupID          int64  `json:"group_id"`
	GroupName        string `json:"group_name"`
	Platform         string `json:"platform"`
	APIKeyID         int64  `json:"api_key_id"`
	APIKeyName       string `json:"api_key_name"`
	OwnerUserID      int64  `json:"owner_user_id,omitempty"`
	Key              string `json:"-"`
	GroupFingerprint string `json:"-"`
	KeyFingerprint   string `json:"-"`
}

// LocalModelTargetReader resolves only the current administrator's own local
// API keys. Implementations must never return keys belonging to another user.
type LocalModelTargetReader interface {
	ListLocalModelTargets(context.Context, int64) ([]LocalModelTarget, error)
	ResolveLocalModelTarget(context.Context, int64, int64, int64) (LocalModelTarget, error)
}

// LocalModelRunner sends a model probe through the application's own gateway,
// so group routing, billing and response normalization are exercised exactly
// as they are for a real user request.
type LocalModelRunner interface {
	RunLocalModel(context.Context, LocalModelTarget, ModelRunRequest) (ModelRunResult, error)
}

type ModelNotice struct {
	SiteID                                             int64
	SiteName, BaseURL, PolicyName, Model, Kind, Detail string
	ObservedAt                                         time.Time
}

type ModelNotifier interface {
	Recipients(context.Context, []string) ([]string, error)
	SendModel(context.Context, string, ModelNotice) error
}
