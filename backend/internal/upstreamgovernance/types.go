// Package upstreamgovernance manages remote user-visible resources separately from local routing.
package upstreamgovernance

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var (
	ErrNotFound    = errors.New("governance resource not found")
	ErrConflict    = errors.New("governance preview or resource is stale")
	ErrSiteInUse   = errors.New("governance site has active account bindings, pending imports, or managed keys")
	ErrBusy        = errors.New("an operation is already running for this site")
	ErrInvalid     = errors.New("invalid governance input")
	ErrReauth      = errors.New("upstream authorization required")
	ErrUnsupported = errors.New("upstream contract is not supported")
	ErrEncryption  = errors.New("a persistent encryption key is required")
)

type Site struct {
	ID                   int64                `json:"id"`
	Name                 string               `json:"name"`
	Platform             string               `json:"platform"`
	BaseURL              string               `json:"base_url"`
	ProxyID              *int64               `json:"proxy_id"`
	Enabled              bool                 `json:"enabled"`
	IntervalMinutes      int                  `json:"interval_minutes"`
	Version              int64                `json:"version"`
	HasCredential        bool                 `json:"has_credential"`
	SessionCipher        string               `json:"-"`
	LoginCipher          string               `json:"-"`
	Status               string               `json:"status"`
	LastError            string               `json:"last_error"`
	LastSyncAt           *time.Time           `json:"last_sync_at"`
	NextSyncAt           time.Time            `json:"next_sync_at"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
	BalanceMonitor       BalanceMonitorConfig `json:"balance_monitor"`
	BalanceMonitorStatus BalanceMonitorStatus `json:"balance_monitor_status"`
	balanceState         BalanceMonitorState
}

type LoginInput struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	OTP            string `json:"otp"`
	ChallengeToken string `json:"challenge_token"`
	CaptchaToken   string `json:"captcha_token"`
	SessionToken   string `json:"session_token"`
	UserID         int64  `json:"user_id"`
}
type LoginCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type LoginCredentialsResult struct {
	LoginCredentials
	Version int64 `json:"version"`
}
type Session struct {
	AccessToken string            `json:"access_token,omitempty"`
	Cookies     map[string]string `json:"cookies,omitempty"`
	UserID      int64             `json:"user_id,omitempty"`
	AuthVariant string            `json:"auth_variant,omitempty"`
}
type Challenge struct {
	Kind  string `json:"kind"`
	Token string `json:"token,omitempty"`
}
type ConnectResult struct {
	Site      *Site      `json:"site,omitempty"`
	Challenge *Challenge `json:"challenge,omitempty"`
}
type RemotePrice struct {
	Model      string         `json:"model"`
	Platform   string         `json:"platform"`
	Unit       string         `json:"unit"`
	Input      *float64       `json:"input"`
	Output     *float64       `json:"output"`
	PerRequest *float64       `json:"per_request"`
	Details    map[string]any `json:"details,omitempty"`
}
type RemoteGroup struct {
	ID                     string        `json:"id"`
	Name                   string        `json:"name"`
	Platform               string        `json:"platform"`
	RateMultiplier         *float64      `json:"rate_multiplier"`
	UserRateMultiplier     *float64      `json:"user_rate_multiplier"`
	ResolvedRateMultiplier *float64      `json:"resolved_rate_multiplier"`
	PeakRateEnabled        bool          `json:"peak_rate_enabled"`
	PeakStart              string        `json:"peak_start,omitempty"`
	PeakEnd                string        `json:"peak_end,omitempty"`
	PeakRateMultiplier     *float64      `json:"peak_rate_multiplier,omitempty"`
	Models                 []string      `json:"models"`
	Prices                 []RemotePrice `json:"prices"`
	Source                 string        `json:"source"`
}
type RemoteChannel struct {
	Name     string   `json:"name"`
	GroupIDs []string `json:"group_ids"`
	Models   []string `json:"models"`
}
type Catalog struct {
	Groups   []RemoteGroup   `json:"groups"`
	Channels []RemoteChannel `json:"channels"`
	Warnings []string        `json:"warnings"`
	Account  *RemoteAccount  `json:"account,omitempty"`
}

// RemoteAccount contains only user-visible accounting fields. A nil amount is
// unknown, not zero; New API quotas retain their native unit.
type RemoteAccount struct {
	UserID        int64    `json:"user_id"`
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	Balance       *float64 `json:"balance"`
	FrozenBalance *float64 `json:"frozen_balance"`
	UsedBalance   *float64 `json:"used_balance"`
	Unit          string   `json:"unit"`
	Source        string   `json:"source"`
}
type Snapshot struct {
	ID          int64     `json:"id"`
	SiteID      int64     `json:"site_id"`
	SiteVersion int64     `json:"site_version"`
	Catalog     Catalog   `json:"catalog"`
	CreatedAt   time.Time `json:"created_at"`
}
type RemoteKey struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}
type Binding struct {
	ID                   int64     `json:"id"`
	SiteID               int64     `json:"site_id"`
	RemoteGroupID        string    `json:"remote_group_id"`
	Platform             string    `json:"platform"`
	LocalGroupID         int64     `json:"local_group_id"`
	LocalGroupIDs        []int64   `json:"local_group_ids"`
	AccountID            int64     `json:"account_id"`
	Marker               string    `json:"marker"`
	KeyCipher            string    `json:"-"`
	ProbeEnabled         bool      `json:"probe_enabled"`
	ProbeModel           string    `json:"probe_model"`
	ProbeIntervalMinutes int       `json:"probe_interval_minutes"`
	NextProbeAt          time.Time `json:"next_probe_at"`
}
type Selection struct {
	RemoteGroupID  string         `json:"remote_group_id"`
	Platform       string         `json:"platform"`
	LocalGroupID   int64          `json:"local_group_id"`
	LocalGroupIDs  []int64        `json:"local_group_ids"`
	AccountName    string         `json:"account_name"`
	CostMultiplier float64        `json:"cost_multiplier"`
	AccountConfig  *AccountConfig `json:"account_config,omitempty"`
}
type AccountConfig struct {
	Concurrency                     int               `json:"concurrency"`
	Priority                        *int              `json:"priority,omitempty"`
	ModelMapping                    map[string]string `json:"model_mapping"`
	UpstreamBillingRateSyncEnabled  bool              `json:"upstream_billing_rate_sync_enabled"`
	QuotaDailyLimit                 float64           `json:"quota_daily_limit"`
	QuotaWeeklyLimit                float64           `json:"quota_weekly_limit"`
	QuotaLimit                      float64           `json:"quota_limit"`
	OpenAILongContextBillingEnabled bool              `json:"openai_long_context_billing_enabled"`
}
type LocalAccount struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	GroupIDs            []int64        `json:"group_ids"`
	CostMultiplier      float64        `json:"cost_multiplier"`
	Fingerprint         string         `json:"fingerprint"`
	AccountConfig       *AccountConfig `json:"account_config,omitempty"`
	NotesMatchAPIKey    bool           `json:"notes_match_api_key"`
	BillingProbeEnabled bool           `json:"upstream_billing_probe_enabled"`
}
type LocalTarget struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	SaleMultiplier float64 `json:"sale_multiplier"`
	Fingerprint    string  `json:"fingerprint"`
}
type PreviewRow struct {
	Selection     Selection     `json:"selection"`
	RemoteGroup   RemoteGroup   `json:"remote_group"`
	Target        LocalTarget   `json:"target"`
	Targets       []LocalTarget `json:"targets"`
	Existing      *LocalAccount `json:"existing"`
	Marker        string        `json:"marker"`
	WillCreateKey bool          `json:"will_create_key"`
}
type ItemResult struct {
	RemoteGroupID string `json:"remote_group_id"`
	Platform      string `json:"platform"`
	AccountID     int64  `json:"account_id,omitempty"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}
type ApplyResult struct {
	PreviewID string       `json:"preview_id"`
	Items     []ItemResult `json:"items"`
}
type Preview struct {
	ID          string       `json:"id"`
	SiteID      int64        `json:"site_id"`
	SiteVersion int64        `json:"site_version"`
	SnapshotID  int64        `json:"snapshot_id"`
	Rows        []PreviewRow `json:"rows"`
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	Result      *ApplyResult `json:"result,omitempty"`
}
type Event struct {
	ID           int64     `json:"id"`
	SiteID       int64     `json:"site_id"`
	Kind         string    `json:"kind"`
	Resource     string    `json:"resource"`
	Before       string    `json:"before"`
	After        string    `json:"after"`
	Acknowledged bool      `json:"acknowledged"`
	CreatedAt    time.Time `json:"created_at"`
}
type ProbeResult struct {
	Success   bool   `json:"success"`
	LatencyMS int64  `json:"latency_ms"`
	ErrorCode string `json:"error_code,omitempty"`
}
type Check struct {
	ID        int64  `json:"id"`
	SiteID    int64  `json:"site_id"`
	BindingID int64  `json:"binding_id"`
	Model     string `json:"model"`
	ProbeResult
	CreatedAt time.Time `json:"created_at"`
}
type AccountChange struct {
	Marker, Name, Platform, BaseURL, APIKey, ExpectedFingerprint string
	ExpectedTargetFingerprint                                    string
	GroupID                                                      int64
	GroupIDs                                                     []int64
	ExpectedTargetFingerprints                                   map[int64]string
	CostMultiplier                                               float64
	ProxyID                                                      *int64
	AccountConfig                                                *AccountConfig
}
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type ClientFactory func(context.Context, Site) (HTTPDoer, error)
type Connector interface {
	Login(context.Context, Site, LoginInput) (Session, *Challenge, error)
	Discover(context.Context, Site, Session) (Catalog, error)
	EnsureKey(context.Context, Site, Session, RemoteGroup, string) (RemoteKey, error)
	Probe(context.Context, Site, RemoteKey, string, string) (ProbeResult, error)
}
type Encryptor interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}
type LocalAccounts interface {
	Target(context.Context, int64, string) (LocalTarget, error)
	FindAccount(context.Context, string) (*LocalAccount, error)
	ApplyAccount(context.Context, AccountChange) (*LocalAccount, error)
}
type Store interface {
	ListSites(context.Context) ([]Site, error)
	CreateSite(context.Context, *Site) error
	GetSite(context.Context, int64) (*Site, error)
	UpdateSite(context.Context, *Site, int64) error
	StageLoginChallenge(context.Context, int64, int64, string) error
	DeleteSite(context.Context, int64) error
	LockSite(context.Context, int64) (func(), bool, error)
	ObserveSite(context.Context, int64, string, string, time.Time, time.Time) error
	SaveBalanceMonitorState(context.Context, int64, BalanceMonitorState, []Event) error
	DueSites(context.Context, time.Time, int) ([]Site, error)
	LatestSnapshot(context.Context, int64) (*Snapshot, error)
	SaveSnapshot(context.Context, *Snapshot, []Event) error
	ListEvents(context.Context, int64, int, int) ([]Event, int64, error)
	AddEvent(context.Context, *Event) error
	AckEvent(context.Context, int64, int64) error
	SavePreview(context.Context, *Preview) error
	GetPreview(context.Context, int64, string) (*Preview, error)
	SavePreviewResult(context.Context, int64, string, *ApplyResult) error
	ListBindings(context.Context, int64) ([]Binding, error)
	SaveBinding(context.Context, *Binding) error
	ListManagedKeys(context.Context, int64) ([]ManagedKey, error)
	GetManagedKey(context.Context, int64, int64) (*ManagedKey, error)
	SaveManagedKey(context.Context, *ManagedKey) error
	AddCheck(context.Context, *Check) error
	LatestCheck(context.Context, int64, int64) (*Check, error)
	ListChecks(context.Context, int64, int, int) ([]Check, int64, error)
}
