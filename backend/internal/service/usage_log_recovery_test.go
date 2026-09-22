//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildUsageBillingCommandIncludesDurableUsageSnapshot(t *testing.T) {
	created := time.Date(2026, 9, 19, 1, 2, 3, 0, time.UTC)
	log := &UsageLog{
		UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "request-1",
		Model: "mapped-model", RequestedModel: "requested-model", InputTokens: 120,
		ActualCost: .0123, TotalCost: .0123, CreatedAt: created,
		User:    &User{Email: "sensitive-user@example.com"},
		APIKey:  &APIKey{Key: "secret-api-key"},
		Account: &Account{Credentials: map[string]any{"access_token": "secret-upstream-token"}},
		Group:   &Group{Name: "not-a-usage-field"}, Subscription: &UserSubscription{},
	}
	cmd := buildUsageBillingCommand("request-1", log, &postUsageBillingParams{
		Cost:   &CostBreakdown{ActualCost: .0123, TotalCost: .0123},
		APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3},
	})
	encoded, err := json.Marshal(cmd)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.Contains(t, fields, "UsageLogSnapshot", "billing must carry a replayable usage snapshot before committing money")
	var snapshot UsageLog
	require.NoError(t, json.Unmarshal(fields["UsageLogSnapshot"], &snapshot))
	require.Equal(t, created, snapshot.CreatedAt)
	require.Equal(t, log.ActualCost, snapshot.ActualCost)
	require.Equal(t, log.RequestedModel, snapshot.RequestedModel)
	require.Nil(t, snapshot.User)
	require.Nil(t, snapshot.APIKey)
	require.Nil(t, snapshot.Account)
	require.Nil(t, snapshot.Group)
	require.Nil(t, snapshot.Subscription)
	require.NotContains(t, string(encoded), "secret-")
	require.NotContains(t, string(encoded), "sensitive-user")
	require.NotContains(t, string(encoded), "not-a-usage-field")
}

func TestSanitizeUsageLogForRecoveryDoesNotAliasMutableFields(t *testing.T) {
	model := "upstream-model"
	input := &UsageLog{UpstreamModel: &model, ImageSizeBreakdown: map[string]int{"1024x1024": 1}, ActualCost: .01}
	snapshot := SanitizeUsageLogForRecovery(input)
	model = "changed"
	input.ImageSizeBreakdown["1024x1024"] = 4
	input.ActualCost = 0
	require.Equal(t, "upstream-model", *snapshot.UpstreamModel)
	require.Equal(t, 1, snapshot.ImageSizeBreakdown["1024x1024"])
	require.Equal(t, .01, snapshot.ActualCost)
}

type recoveryOutboxStub struct {
	mu                 sync.Mutex
	items              []UsageLogOutboxItem
	claimed            bool
	completed, retried int
	lastReason         string
	lastDelay          time.Duration
}

func (s *recoveryOutboxStub) ClaimUsageLogOutbox(context.Context, int, time.Duration) ([]UsageLogOutboxItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed {
		return nil, nil
	}
	s.claimed = true
	return s.items, nil
}
func (s *recoveryOutboxStub) CompleteUsageLogOutbox(context.Context, int64, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed++
	return true, nil
}
func (s *recoveryOutboxStub) RetryUsageLogOutbox(_ context.Context, _ int64, _ string, delay time.Duration, reason string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retried++
	s.lastReason, s.lastDelay = reason, delay
	return true, nil
}

type recoveryUsageWriter struct {
	UsageLogRepository
	mu           sync.Mutex
	err          error
	panicOnWrite bool
	writes       []*UsageLog
}

func (s *recoveryUsageWriter) Create(_ context.Context, log *UsageLog) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.panicOnWrite {
		panic("simulated usage writer failure")
	}
	if s.err != nil {
		return false, s.err
	}
	s.writes = append(s.writes, log)
	return true, nil
}

func recoveryItem(t *testing.T) UsageLogOutboxItem {
	t.Helper()
	cmd := &UsageBillingCommand{RequestID: "recovery-1", APIKeyID: 2, UserID: 1, AccountID: 3,
		UsageLogSnapshot: &UsageLog{RequestID: "recovery-1", APIKeyID: 2, UserID: 1, AccountID: 3, ActualCost: .25, CreatedAt: time.Now().UTC()},
	}
	payload, err := MarshalUsageLogRecoveryPayload(cmd)
	require.NoError(t, err)
	return UsageLogOutboxItem{ID: 1, RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, ClaimToken: "claim", Attempts: 1, Payload: payload}
}

func TestUsageLogRecovery_RetriesFailedWriteWithoutCompletingEvidence(t *testing.T) {
	outbox := &recoveryOutboxStub{items: []UsageLogOutboxItem{recoveryItem(t)}}
	writer := &recoveryUsageWriter{err: errors.New("usage database unavailable")}
	worker := NewUsageLogRecoveryService(outbox, writer)
	completed, err := worker.RunOnce(context.Background(), 1)
	require.Error(t, err)
	require.Zero(t, completed)
	require.Zero(t, outbox.completed)
	require.Equal(t, 1, outbox.retried)
	require.Equal(t, "usage_write_failed", outbox.lastReason)
	require.Positive(t, outbox.lastDelay)
	// A new worker has no in-memory knowledge of the original request, but the
	// same durable item is sufficient to finish after the database recovers.
	outbox.claimed = false
	writer.err = nil
	completed, err = NewUsageLogRecoveryService(outbox, writer).RunOnce(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	require.Equal(t, 1, outbox.completed)
	require.Len(t, writer.writes, 1)
	require.Equal(t, .25, writer.writes[0].ActualCost)
}

func TestUsageLogRecovery_InvalidPayloadRemainsDurable(t *testing.T) {
	item := recoveryItem(t)
	item.RequestID = "different-request"
	outbox := &recoveryOutboxStub{items: []UsageLogOutboxItem{item}}
	writer := &recoveryUsageWriter{}
	completed, err := NewUsageLogRecoveryService(outbox, writer).RunOnce(context.Background(), 1)
	require.Error(t, err)
	require.Zero(t, completed)
	require.Empty(t, writer.writes)
	require.Zero(t, outbox.completed)
	require.Equal(t, "invalid_payload", outbox.lastReason)
}

func TestUsageLogRecovery_WriterPanicIsRecoverable(t *testing.T) {
	outbox := &recoveryOutboxStub{items: []UsageLogOutboxItem{recoveryItem(t)}}
	writer := &recoveryUsageWriter{panicOnWrite: true}
	completed, err := NewUsageLogRecoveryService(outbox, writer).RunOnce(context.Background(), 1)
	require.Error(t, err)
	require.Zero(t, completed)
	require.Zero(t, outbox.completed)
	require.Equal(t, "usage_write_panicked", outbox.lastReason)
}

type recoveryAcknowledgingWriter struct {
	UsageLogRepository
	bestEffortErr, syncErr error
	acknowledged           int
}

func (w *recoveryAcknowledgingWriter) CreateBestEffort(context.Context, *UsageLog) error {
	return w.bestEffortErr
}
func (w *recoveryAcknowledgingWriter) Create(context.Context, *UsageLog) (bool, error) {
	return w.syncErr == nil, w.syncErr
}
func (w *recoveryAcknowledgingWriter) AcknowledgeUsageLogOutbox(context.Context, *UsageLog) error {
	w.acknowledged++
	return nil
}

func TestWriteUsageLogBestEffortAcknowledgesOnlySuccessfulWrites(t *testing.T) {
	for _, test := range []struct {
		name                   string
		bestEffortErr, syncErr error
		want                   int
	}{
		{name: "normal durable write", want: 1},
		{name: "sync fallback durable write", bestEffortErr: errors.New("batch error"), want: 1},
		{name: "both writes failed", bestEffortErr: errors.New("batch error"), syncErr: errors.New("sync error"), want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &recoveryAcknowledgingWriter{bestEffortErr: test.bestEffortErr, syncErr: test.syncErr}
			writeUsageLogBestEffort(context.Background(), writer, &UsageLog{RequestID: "request", APIKeyID: 1}, "test.usage-recovery")
			require.Equal(t, test.want, writer.acknowledged)
		})
	}
}
