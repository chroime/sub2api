package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalGroupDispatchUsesOnlyAstrBotPolicy(t *testing.T) {
	tests := []struct {
		name          string
		mailEnabled   bool
		astrbot       bool
		localEvent    bool
		onlineEvent   bool
		monitor       bool
		wantDelivered int
		wantCompleted int
	}{
		{name: "QQ works without email or legacy event flag", astrbot: true, onlineEvent: true, monitor: true, wantDelivered: 1, wantCompleted: 1},
		{name: "legacy flag cannot override disabled group event", astrbot: true, localEvent: true, monitor: true, wantCompleted: 1},
		{name: "email cannot override QQ event", mailEnabled: true, astrbot: true, monitor: true, wantCompleted: 1},
		{name: "email cannot override QQ disabled", mailEnabled: true, localEvent: true, onlineEvent: true, monitor: true},
		{name: "local monitor can pause dispatch", mailEnabled: true, astrbot: true, localEvent: true, onlineEvent: true},
		{name: "both channels enabled", mailEnabled: true, astrbot: true, localEvent: true, onlineEvent: true, monitor: true, wantDelivered: 1, wantCompleted: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := defaultGlobalSettings()
			settings.Notifications.Enabled = tt.mailEnabled
			settings.Notifications.Events.LocalGroupChange = true
			settings.AstrBot.Enabled = tt.astrbot
			settings.AstrBot.Events = localGroupTestEvents(t, map[string]bool{
				"local_group_change": tt.localEvent,
				"group_online":       tt.onlineEvent,
			})
			settings.LocalMonitor.Enabled = tt.monitor
			raw, err := json.Marshal(settings)
			require.NoError(t, err)
			store := &localGroupPolicyStore{}
			sender := &localGroupPolicySender{}
			svc := NewService(store, nil, nil, nil, false)
			svc.SetGlobalSettingsStore(&globalSettingsMemoryStore{values: map[string]string{globalSettingsKey: string(raw)}})
			require.NoError(t, svc.DispatchLocalGroupChangeNotifications(t.Context(), sender, 20))
			require.Equal(t, tt.wantDelivered, sender.delivered)
			require.Equal(t, tt.wantCompleted, store.completed)
			require.NoError(t, store.completionErr)
			require.Equal(t, tt.wantDelivered, sender.recipientCalls)
		})
	}
}

type localGroupPolicyStore struct {
	Store
	events        []LocalGroupChangeEvent
	completed     int
	completionErr error
	nextAttempt   time.Time
}

func (*localGroupPolicyStore) EnqueueLocalGroupChange(context.Context, LocalGroupChangeEvent) error {
	return nil
}

func (s *localGroupPolicyStore) DueLocalGroupChanges(context.Context, time.Time, int) ([]LocalGroupChangeEvent, error) {
	if s.events != nil {
		return s.events, nil
	}
	return []LocalGroupChangeEvent{{ID: 1, GroupID: 2, Kind: LocalGroupChangeCreated, After: `{"name":"GPT Pro","status":"active","rate_multiplier":0.06}`}}, nil
}

func (*localGroupPolicyStore) ClaimLocalGroupChange(context.Context, int64, time.Time, time.Time) (bool, error) {
	return true, nil
}

func (s *localGroupPolicyStore) CompleteLocalGroupChange(_ context.Context, _ int64, _ time.Time, sendErr error, next time.Time) error {
	s.completed++
	s.completionErr = sendErr
	s.nextAttempt = next
	return nil
}

type localGroupPolicySender struct {
	delivered      int
	recipientCalls int
	notices        []LocalGroupChangeNotice
	sendErr        error
}

func (s *localGroupPolicySender) Recipients(context.Context) ([]string, error) {
	s.recipientCalls++
	return []string{"qq:bot:GroupMessage:1_2"}, nil
}

func (s *localGroupPolicySender) SendLocalGroupChange(_ context.Context, _ string, notice LocalGroupChangeNotice) error {
	s.delivered++
	s.notices = append(s.notices, notice)
	return s.sendErr
}

func localGroupTestEvents(t *testing.T, enabled map[string]bool) GlobalNotificationEvents {
	t.Helper()
	values := map[string]bool{
		"group_online":             false,
		"group_offline":            false,
		"group_name_change":        false,
		"group_description_change": false,
		"group_rate_change":        false,
	}
	for key, value := range enabled {
		values[key] = value
	}
	raw, err := json.Marshal(values)
	require.NoError(t, err)
	var events GlobalNotificationEvents
	require.NoError(t, json.Unmarshal(raw, &events))
	return events
}

func TestLocalGroupDispatchFiltersIndependentChangesBeforeSending(t *testing.T) {
	settings := defaultGlobalSettings()
	settings.AstrBot.Enabled = true
	settings.AstrBot.Events = localGroupTestEvents(t, map[string]bool{
		"local_group_change":       true,
		"group_description_change": true,
		"group_rate_change":        true,
	})
	settings.LocalMonitor.Enabled = true
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	observedAt := time.Date(2026, 10, 22, 3, 26, 35, 0, time.UTC)
	store := &localGroupPolicyStore{events: []LocalGroupChangeEvent{{
		ID: 2, GroupID: 42, Kind: LocalGroupChangeUpdated, CreatedAt: observedAt,
		Before: `{"name":"Old Pro","status":"active","description":"稳定线路","rate_multiplier":0.02}`,
		After:  `{"name":"New Pro","status":"disabled","description":"","rate_multiplier":0.023}`,
	}}}
	sender := &localGroupPolicySender{}
	svc := NewService(store, nil, nil, nil, false)
	svc.SetGlobalSettingsStore(&globalSettingsMemoryStore{values: map[string]string{globalSettingsKey: string(raw)}})

	require.NoError(t, svc.DispatchLocalGroupChangeNotifications(t.Context(), sender, 20))

	require.Equal(t, 1, sender.delivered)
	require.Equal(t, 1, sender.recipientCalls)
	require.Equal(t, "【来至外星球的通知】\n分组描述调整：New Pro，原描述「稳定线路」，描述已清空\n分组倍率调整：New Pro，0.02x → 0.023x（上调）\n通知时间：2026-10-22 11:26:35", sender.notices[0].AstrBotText)
	require.Equal(t, 1, store.completed)
	require.NoError(t, store.completionErr)
}

func TestLocalGroupDispatchHandlesMissingSenderAfterFiltering(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled bool
	}{
		{"filtered event is consumed", false},
		{"matching event stays retryable", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settings := defaultGlobalSettings()
			settings.AstrBot.Enabled = true
			settings.AstrBot.Events = localGroupTestEvents(t, map[string]bool{"group_online": tt.enabled})
			settings.LocalMonitor.Enabled = true
			raw, err := json.Marshal(settings)
			require.NoError(t, err)
			store := &localGroupPolicyStore{}
			svc := NewService(store, nil, nil, nil, false)
			svc.SetGlobalSettingsStore(&globalSettingsMemoryStore{values: map[string]string{globalSettingsKey: string(raw)}})

			require.NoError(t, svc.DispatchLocalGroupChangeNotifications(t.Context(), nil, 20))

			require.Equal(t, 1, store.completed)
			if tt.enabled {
				require.ErrorContains(t, store.completionErr, "notifier")
			} else {
				require.NoError(t, store.completionErr)
			}
		})
	}
}

func TestLocalGroupQueueConsumesUnrelatedEventsWithoutResolvingRecipients(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	store := &localGroupPolicyStore{events: []LocalGroupChangeEvent{{
		ID: 2, GroupID: 42, Kind: LocalGroupChangeUpdated,
		Before: `{"name":"GPT Pro","status":"active","sort_order":1}`,
		After:  `{"name":"GPT Pro","status":"active","sort_order":2}`,
	}}}
	sender := &localGroupEmptyRecipientSender{}
	queue := NewLocalGroupChangeQueue(store, sender, func() time.Time { return now })

	require.NoError(t, queue.Dispatch(t.Context(), 20))

	require.Zero(t, sender.recipientCalls)
	require.Zero(t, sender.delivered)
	require.Equal(t, 1, store.completed)
	require.NoError(t, store.completionErr)
	require.Equal(t, now, store.nextAttempt)
}

func TestLocalGroupQueueConsumesSilentCreationWithoutSender(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	store := &localGroupPolicyStore{events: []LocalGroupChangeEvent{{
		ID: 2, GroupID: 42, Kind: LocalGroupChangeCreated,
		After: `{"name":"GPT Pro","status":"disabled"}`,
	}}}
	queue := NewLocalGroupChangeQueue(store, nil, func() time.Time { return now })

	require.NoError(t, queue.Dispatch(t.Context(), 20))

	require.Equal(t, 1, store.completed)
	require.NoError(t, store.completionErr)
	require.Equal(t, now, store.nextAttempt)
}

func TestLocalGroupQueueKeepsDeliveryErrorsRetryable(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-time.Hour)
	store := &localGroupPolicyStore{events: []LocalGroupChangeEvent{{
		ID: 2, GroupID: 42, Kind: LocalGroupChangeCreated, CreatedAt: observedAt,
		After: `{"name":"GPT Pro","status":"active"}`,
	}}}
	sender := &localGroupPolicySender{sendErr: errors.New("transport unavailable")}
	queue := NewLocalGroupChangeQueue(store, sender, func() time.Time { return now })

	require.NoError(t, queue.Dispatch(t.Context(), 20))
	require.ErrorContains(t, store.completionErr, "transport unavailable")
	require.True(t, store.nextAttempt.After(now))
	require.Equal(t, observedAt, sender.notices[0].ObservedAt)
	firstText := sender.notices[0].AstrBotText

	now = store.nextAttempt
	sender.sendErr = nil
	require.NoError(t, queue.Dispatch(t.Context(), 20))
	require.Equal(t, 2, sender.delivered)
	require.NoError(t, store.completionErr)
	require.Equal(t, firstText, sender.notices[1].AstrBotText, "retry must use the event timestamp")
}

func TestLocalGroupQueueKeepsEventRetryableWithoutRecipients(t *testing.T) {
	for _, recipients := range [][]string{nil, {}, {"", "  ", "\t"}} {
		t.Run(stringMustJSON(recipients), func(t *testing.T) {
			now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
			store := &localGroupPolicyStore{}
			sender := &localGroupEmptyRecipientSender{recipients: recipients}
			queue := NewLocalGroupChangeQueue(store, sender, func() time.Time { return now })
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Zero(t, sender.delivered)
			require.Equal(t, 1, store.completed)
			require.ErrorContains(t, store.completionErr, "recipient")
			require.True(t, store.nextAttempt.After(now), "configuration errors must remain pending for retry")

			// A later configured target can receive the pending event.
			now = store.nextAttempt
			sender.recipients = []string{"qq:bot:GroupMessage:1_2"}
			require.NoError(t, queue.Dispatch(t.Context(), 20))
			require.Equal(t, 1, sender.delivered)
			require.NoError(t, store.completionErr)
		})
	}
}

func stringMustJSON(value []string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

type localGroupEmptyRecipientSender struct {
	localGroupPolicySender
	recipients []string
}

func (s *localGroupEmptyRecipientSender) Recipients(context.Context) ([]string, error) {
	s.recipientCalls++
	return s.recipients, nil
}
