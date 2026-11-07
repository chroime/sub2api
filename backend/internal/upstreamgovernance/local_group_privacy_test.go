package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalGroupQueueSuppressesExclusiveSnapshots(t *testing.T) {
	public := `{"name":"Public","status":"active","description":"old","rate_multiplier":1,"is_exclusive":false}`
	private := `{"name":"Private","status":"active","description":"secret","rate_multiplier":2,"is_exclusive":true}`
	tests := []struct {
		name  string
		event LocalGroupChangeEvent
	}{
		{"created", LocalGroupChangeEvent{Kind: LocalGroupChangeCreated, After: private}},
		{"deleted", LocalGroupChangeEvent{Kind: LocalGroupChangeDeleted, Before: private}},
		{"public to exclusive", LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated, Before: public, After: private}},
		{"exclusive to public", LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated, Before: private, After: public}},
		{"soft deleted while becoming exclusive", LocalGroupChangeEvent{Kind: LocalGroupChangeDeleted, Before: public, After: private}},
		{"legacy field spelling", LocalGroupChangeEvent{Kind: LocalGroupChangeCreated, After: `{"Name":"Private","Status":"active","IsExclusive":true}`}},
	}
	for _, change := range []struct {
		name, field string
		value       any
	}{
		{"name", "name", "Secret renamed"},
		{"description", "description", "Secret description"},
		{"rate", "rate_multiplier", 3},
		{"offline", "status", "disabled"},
		{"models", "model_routing", map[string]any{"secret-model": []int{5}}},
		{"pricing", "image_price_1k", 0.5},
	} {
		var after map[string]any
		require.NoError(t, json.Unmarshal([]byte(private), &after))
		after[change.field] = change.value
		raw, err := json.Marshal(after)
		require.NoError(t, err)
		tests = append(tests, struct {
			name  string
			event LocalGroupChangeEvent
		}{change.name, LocalGroupChangeEvent{Kind: LocalGroupChangeUpdated, Before: private, After: string(raw)}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, sender, queue, now := localGroupPrivacyFixture(tt.event)
			notice := RenderLocalGroupChangeNotice(store.events[0])
			require.Empty(t, notice.Subject, "private names must not enter a notification projection")
			require.Empty(t, notice.AstrBotText)
			require.Empty(t, notice.Body)
			require.Empty(t, notice.GroupChanges)
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Zero(t, sender.delivered)
			require.Zero(t, sender.recipientCalls)
			require.Equal(t, localGroupChangeSent, store.events[0].Status, "suppression must consume the event instead of postponing it")
			require.Equal(t, now, *store.events[0].ProcessedAt)
			store.allowed = true
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Zero(t, sender.delivered, "making the group public later must not replay private changes")
		})
	}
}

func TestLocalGroupQueueSuppressesSnapshotsWithUnknownVisibility(t *testing.T) {
	for _, marker := range []string{"", `,"is_exclusive":null`, `,"is_exclusive":"false"`, `,"is_exclusive":0`, `,"is_exclusive":false,"IsExclusive":true`} {
		t.Run(marker, func(t *testing.T) {
			store, sender, queue, _ := localGroupPrivacyFixture(LocalGroupChangeEvent{
				Kind: LocalGroupChangeDeleted, Before: `{"name":"Unknown old group","status":"active"` + marker + `}`,
			})
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Zero(t, sender.delivered)
			require.Equal(t, localGroupChangeSent, store.events[0].Status)
		})
	}
}

func TestLocalGroupQueueConsumesOldPublicEventWhenVisibilityChanges(t *testing.T) {
	store, sender, queue, _ := localGroupPrivacyFixture(publicLocalGroupPrivacyEvent())
	store.allowed = false // Current state or later private audit history rejects this old event.
	require.NoError(t, queue.Dispatch(t.Context(), 20))
	require.Zero(t, sender.delivered)
	require.Zero(t, sender.recipientCalls)
	require.Equal(t, localGroupChangeSent, store.events[0].Status)
	store.allowed = true
	require.NoError(t, queue.Dispatch(t.Context(), 20))
	require.Zero(t, sender.delivered)
}

func TestLocalGroupQueueChecksVisibilityImmediatelyBeforeEachRecipient(t *testing.T) {
	for _, afterRecipients := range []bool{true, false} {
		t.Run(map[bool]string{true: "changed while resolving recipients", false: "changed after first recipient"}[afterRecipients], func(t *testing.T) {
			store, sender, queue, _ := localGroupPrivacyFixture(publicLocalGroupPrivacyEvent())
			transport := &localGroupPrivacySender{localGroupPolicySender: sender}
			queue.Sender = transport
			if afterRecipients {
				transport.onRecipients = func() { store.allowed = false }
			} else {
				transport.onSend = func() { store.allowed = false }
			}
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			if afterRecipients {
				require.Zero(t, sender.delivered)
			} else {
				require.Equal(t, 1, sender.delivered)
			}
			require.Equal(t, localGroupChangeSent, store.events[0].Status)
		})
	}
}

func TestLocalGroupQueueDoesNotSendWhenVisibilityLookupFails(t *testing.T) {
	store, sender, queue, now := localGroupPrivacyFixture(publicLocalGroupPrivacyEvent())
	store.lookupErr = errors.New("visibility lookup unavailable")
	require.NoError(t, queue.Dispatch(t.Context(), 20))
	require.Zero(t, sender.delivered)
	require.Equal(t, localGroupChangePending, store.events[0].Status)
	require.Contains(t, store.events[0].LastError, "visibility lookup")
	require.True(t, store.events[0].NextAttemptAt.After(now))
}

func TestLocalGroupQueueRetainsPublicNotifications(t *testing.T) {
	public := `{"name":"Public","status":"active","description":"old","rate_multiplier":1,"is_exclusive":false}`
	for _, event := range []LocalGroupChangeEvent{
		{Kind: LocalGroupChangeCreated, After: public},
		{Kind: LocalGroupChangeDeleted, Before: public},
		{Kind: LocalGroupChangeUpdated, Before: public, After: `{"name":"Renamed","status":"disabled","description":"new","rate_multiplier":2,"is_exclusive":false}`},
		{Kind: LocalGroupChangeCreated, After: `{"Name":"Legacy public","Status":"active","IsExclusive":false}`},
	} {
		store, sender, queue, _ := localGroupPrivacyFixture(event)
		require.NoError(t, queue.Dispatch(t.Context(), 20))
		require.Equal(t, 1, sender.delivered)
		require.NotEmpty(t, sender.notices[0].AstrBotText)
		require.Equal(t, localGroupChangeSent, store.events[0].Status)
	}
}

func publicLocalGroupPrivacyEvent() LocalGroupChangeEvent {
	return LocalGroupChangeEvent{Kind: LocalGroupChangeCreated, After: `{"name":"Public","status":"active","is_exclusive":false}`}
}

func localGroupPrivacyFixture(event LocalGroupChangeEvent) (*localGroupPrivacyStore, *localGroupPolicySender, *LocalGroupChangeQueue, time.Time) {
	now := time.Date(2026, 11, 7, 6, 0, 0, 0, time.UTC)
	event.ID, event.GroupID, event.Status = 1, 42, localGroupChangePending
	event.CreatedAt, event.NextAttemptAt = now, now
	store := &localGroupPrivacyStore{localGroupPollMemoryStore: &localGroupPollMemoryStore{events: []LocalGroupChangeEvent{event}}, allowed: true}
	sender := &localGroupPolicySender{}
	return store, sender, NewLocalGroupChangeQueue(store, sender, func() time.Time { return now }), now
}

type localGroupPrivacyStore struct {
	*localGroupPollMemoryStore
	allowed   bool
	lookupErr error
}

func (s *localGroupPrivacyStore) LocalGroupChangeIsPublic(context.Context, LocalGroupChangeEvent) (bool, error) {
	return s.allowed, s.lookupErr
}

type localGroupPrivacySender struct {
	*localGroupPolicySender
	onRecipients func()
	onSend       func()
}

func (s *localGroupPrivacySender) Recipients(context.Context) ([]string, error) {
	s.recipientCalls++
	if s.onRecipients != nil {
		s.onRecipients()
	}
	return []string{"qq:bot:GroupMessage:1", "qq:bot:GroupMessage:2"}, nil
}

func (s *localGroupPrivacySender) SendLocalGroupChange(ctx context.Context, recipient string, notice LocalGroupChangeNotice) error {
	err := s.localGroupPolicySender.SendLocalGroupChange(ctx, recipient, notice)
	if s.onSend != nil {
		s.onSend()
	}
	return err
}
