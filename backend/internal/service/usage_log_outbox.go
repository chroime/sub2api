package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const usageLogRecoveryPayloadVersion = 1

var ErrUsageLogOutboxConflict = errors.New("persisted usage record does not match committed billing snapshot")

// UsageLogOutboxItem is durable work with a fenced lease. Payload contains only
// the allowlisted usage fields; it never contains a billing command.
type UsageLogOutboxItem struct {
	ID         int64
	RequestID  string
	APIKeyID   int64
	ClaimToken string
	Attempts   int
	Payload    []byte
}

type UsageLogOutboxRepository interface {
	ClaimUsageLogOutbox(ctx context.Context, limit int, lease time.Duration) ([]UsageLogOutboxItem, error)
	CompleteUsageLogOutbox(ctx context.Context, id int64, claimToken string) (bool, error)
	RetryUsageLogOutbox(ctx context.Context, id int64, claimToken string, delay time.Duration, reason string) (bool, error)
}

type usageLogRecoveryEnvelope struct {
	Version int       `json:"version"`
	Usage   *UsageLog `json:"usage"`
}

// MarshalUsageLogRecoveryPayload applies the allowlist again at the transaction
// boundary so callers other than the normal gateway cannot retain credentials.
func MarshalUsageLogRecoveryPayload(cmd *UsageBillingCommand) ([]byte, error) {
	if cmd == nil || cmd.UsageLogSnapshot == nil {
		return nil, nil
	}
	log := SanitizeUsageLogForRecovery(cmd.UsageLogSnapshot)
	log.RequestID = strings.TrimSpace(log.RequestID)
	if log.RequestID == "" || log.RequestID != cmd.RequestID || log.APIKeyID != cmd.APIKeyID || log.UserID != cmd.UserID || log.AccountID != cmd.AccountID {
		return nil, errors.New("usage snapshot identity does not match billing command")
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	return json.Marshal(usageLogRecoveryEnvelope{Version: usageLogRecoveryPayloadVersion, Usage: log})
}

func decodeUsageLogRecoveryPayload(item UsageLogOutboxItem) (*UsageLog, error) {
	var envelope usageLogRecoveryEnvelope
	if err := json.Unmarshal(item.Payload, &envelope); err != nil {
		return nil, errors.New("invalid usage recovery payload")
	}
	log := envelope.Usage
	if envelope.Version != usageLogRecoveryPayloadVersion || log == nil || log.RequestID == "" || log.RequestID != item.RequestID || log.APIKeyID != item.APIKeyID || log.CreatedAt.IsZero() {
		return nil, errors.New("invalid usage recovery identity or version")
	}
	return SanitizeUsageLogForRecovery(log), nil
}
