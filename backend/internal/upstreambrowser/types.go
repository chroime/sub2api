// Package upstreambrowser provides an optional, isolated upstream login browser.
package upstreambrowser

import (
	"context"
	"errors"
	"time"
)

const (
	Width  = 1024
	Height = 720
)

var (
	ErrUnavailable = errors.New("browser_unavailable")
	ErrInvalid     = errors.New("browser_invalid_input")
	ErrClosed      = errors.New("browser_closed")
	ErrProtocol    = errors.New("browser_protocol_error")
	ErrCleanup     = errors.New("browser_cleanup_failed")
)

type Options struct {
	NodePath       string
	ScriptPath     string
	ExecutablePath string
	TempDir        string
}

type Input struct {
	BaseURL  string `json:"base_url"`
	Platform string `json:"platform"`
	ProxyURL string `json:"-"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Driver interface {
	Snapshot(context.Context) (View, error)
	Action(context.Context, Action) error
	Result(context.Context) (*Capture, error)
	Close() error
}

type View struct {
	Status    string `json:"status"`
	Image     string `json:"image,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ErrorCode string `json:"error_code,omitempty"`
}

type Action struct {
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DeltaX float64 `json:"delta_x,omitempty"`
	DeltaY float64 `json:"delta_y,omitempty"`
	Text   string  `json:"text,omitempty"`
	Key    string  `json:"key,omitempty"`
}

type VerifiedLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Capture is a candidate for the caller's native identity verification, not an
// authorization decision. Never serialize this structure to the admin client.
type Capture struct {
	AccessToken   string            `json:"access_token,omitempty"`
	RefreshToken  string            `json:"refresh_token,omitempty"`
	ExpiresIn     int64             `json:"expires_in,omitempty"`
	IssuedAt      time.Time         `json:"issued_at"`
	ExpiresAt     *time.Time        `json:"expires_at,omitempty"`
	UserID        int64             `json:"user_id,omitempty"`
	Cookies       map[string]string `json:"cookies,omitempty"`
	UserAgent     string            `json:"user_agent"`
	AuthVariant   string            `json:"auth_variant"`
	VerifiedLogin *VerifiedLogin    `json:"verified_login,omitempty"`
}

// Code deliberately excludes subprocess errors, URLs, credentials and responses.
func Code(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrUnavailable):
		return "browser_unavailable"
	case errors.Is(err, ErrInvalid):
		return "browser_invalid_input"
	case errors.Is(err, ErrCleanup):
		return "browser_cleanup_failed"
	case errors.Is(err, ErrClosed), errors.Is(err, context.Canceled):
		return "browser_closed"
	case errors.Is(err, context.DeadlineExceeded):
		return "browser_timeout"
	default:
		return "browser_protocol_error"
	}
}
