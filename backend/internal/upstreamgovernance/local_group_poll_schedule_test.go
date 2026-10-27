package upstreamgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalGroupWorkerPollHonorsItsOwnDeadline(t *testing.T) {
	for _, tt := range []struct {
		name       string
		interval   int64
		durableKey bool
		wakeEvery  time.Duration
	}{
		{name: "300 seconds exceeds worker fallback", interval: 300, wakeEvery: time.Minute},
		{name: "short upstream deadline cannot advance 60 second poll", interval: 60, durableKey: true, wakeEvery: time.Second},
		{name: "one second works without upstream credentials", interval: 1, wakeEvery: 250 * time.Millisecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
			started := now
			store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
			sender := &localGroupPolicySender{}
			svc, _ := localGroupPollFixture(t, store, sender, &now, tt.interval, tt.durableKey)
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 1, "first enabled poll should run immediately")

			deadline := started.Add(time.Duration(tt.interval) * time.Second)
			for now = started.Add(tt.wakeEvery); now.Before(deadline); now = now.Add(tt.wakeEvery) {
				nextUpstream := now.Add(tt.wakeEvery)
				store.nextSite = &nextUpstream
				require.NoError(t, svc.runDue(t.Context()))
				require.Len(t, store.polls, 1, "another scheduler wake must not poll local events early at %s", now)
			}
			now = deadline
			require.NoError(t, svc.runDue(t.Context()))
			require.Equal(t, []time.Time{started, deadline}, store.polls)
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 2, "the same deadline must not be consumed twice")
		})
	}
}

func TestLocalGroupWorkerWaitUsesRemainingPollDeadline(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
	svc, _ := localGroupPollFixture(t, store, &localGroupPolicySender{}, &now, 5, false)
	require.NoError(t, svc.runDue(t.Context()))
	now = now.Add(3 * time.Second)
	require.Equal(t, 2*time.Second, svc.workerWait(t.Context()), "remote work must not reset the remaining local wait")
	nextUpstream := now.Add(time.Second)
	store.nextSite = &nextUpstream
	require.Equal(t, time.Second, svc.workerWait(t.Context()))
	store.nextSite = nil
	now = now.Add(2 * time.Second)
	require.Equal(t, 100*time.Millisecond, svc.workerWait(t.Context()), "a due poll must wake promptly")
}

func TestLocalGroupWorkerPollSurvivesRestartAndCadenceChanges(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	started := now
	store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
	sender := &localGroupPolicySender{}
	svc, settings := localGroupPollFixture(t, store, sender, &now, 300, false)
	require.NoError(t, svc.runDue(t.Context()))

	now = started.Add(time.Minute)
	restarted := NewService(store, nil, nil, nil, false)
	restarted.now = func() time.Time { return now }
	restarted.SetGlobalSettingsStore(settings)
	restarted.SetLocalGroupChangeNotifier(sender)
	require.NoError(t, restarted.runDue(t.Context()))
	require.Len(t, store.polls, 1, "restart must retain the last reserved poll")

	global, err := restarted.GetGlobalSettings(t.Context())
	require.NoError(t, err)
	global.LocalMonitor.IntervalSeconds = 90
	global, err = restarted.SaveGlobalSettings(t.Context(), global)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, restarted.workerWait(t.Context()))
	now = started.Add(90 * time.Second)
	require.NoError(t, restarted.runDue(t.Context()))
	require.Len(t, store.polls, 2)

	global.LocalMonitor.IntervalSeconds = 300
	_, err = restarted.SaveGlobalSettings(t.Context(), global)
	require.NoError(t, err)
	now = started.Add(180 * time.Second)
	require.NoError(t, restarted.runDue(t.Context()))
	require.Len(t, store.polls, 2, "extending cadence must move the next local poll")
	now = started.Add(390 * time.Second)
	require.NoError(t, restarted.runDue(t.Context()))
	require.Len(t, store.polls, 3)
}

func TestLocalGroupWorkerPollKeepsOutboxRetryDeadline(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	started := now
	store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
	sender := &localGroupPolicySender{sendErr: errors.New("transport unavailable")}
	svc, _ := localGroupPollFixture(t, store, sender, &now, 300, false)
	queue := svc.NewLocalGroupChangeQueue(sender)
	require.NoError(t, queue.Enqueue(t.Context(), LocalGroupChangeEvent{GroupID: 42, Kind: LocalGroupChangeCreated, After: `{"name":"GPT Pro","status":"active"}`}))
	require.NoError(t, svc.runDue(t.Context()))
	require.Equal(t, 1, sender.delivered)
	require.Equal(t, localGroupChangePending, store.events[0].Status)
	require.Equal(t, started.Add(15*time.Minute), store.events[0].NextAttemptAt)
	require.Equal(t, 1, store.events[0].Attempts)

	sender.sendErr = nil
	for _, elapsed := range []time.Duration{time.Second, 300 * time.Second, 600 * time.Second, 899 * time.Second} {
		now = started.Add(elapsed)
		require.NoError(t, svc.runDue(t.Context()))
		require.Equal(t, 1, sender.delivered, "a poll must not bypass the event retry deadline")
	}
	require.Len(t, store.polls, 3, "upstream wake before the next poll must not scan the outbox")
	now = started.Add(900 * time.Second)
	require.NoError(t, svc.runDue(t.Context()))
	require.Equal(t, 2, sender.delivered)
	require.Equal(t, localGroupChangeSent, store.events[0].Status)
	require.Equal(t, started, sender.notices[1].ObservedAt)
	require.NotNil(t, store.events[0].ProcessedAt)
}

func TestLocalGroupWorkerPollPausesWithoutResettingDeadline(t *testing.T) {
	for _, channel := range []string{"monitor", "AstrBot"} {
		t.Run(channel, func(t *testing.T) {
			now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
			started := now
			store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
			svc, _ := localGroupPollFixture(t, store, &localGroupPolicySender{}, &now, 300, false)
			require.NoError(t, svc.runDue(t.Context()))
			settings, err := svc.GetGlobalSettings(t.Context())
			require.NoError(t, err)
			if channel == "monitor" {
				settings.LocalMonitor.Enabled = false
			} else {
				settings.AstrBot.Enabled = false
			}
			settings, err = svc.SaveGlobalSettings(t.Context(), settings)
			require.NoError(t, err)
			now = started.Add(time.Minute)
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 1)

			settings.LocalMonitor.Enabled, settings.AstrBot.Enabled = true, true
			_, err = svc.SaveGlobalSettings(t.Context(), settings)
			require.NoError(t, err)
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 1, "briefly pausing must not allow an earlier poll")
			now = started.Add(300 * time.Second)
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 2, "re-enabling must keep the previously reserved deadline")
		})
	}
}

func TestLocalGroupWorkerPollLargeIntervalDoesNotOverflow(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	started := now
	store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
	svc, _ := localGroupPollFixture(t, store, &localGroupPolicySender{}, &now, maxGlobalCadenceSeconds, false)
	require.NoError(t, svc.runDue(t.Context()))
	require.Equal(t, time.Minute, svc.workerWait(t.Context()))
	now = started.Add(time.Minute)
	require.NoError(t, svc.runDue(t.Context()))
	require.Len(t, store.polls, 1, "a very large interval must not wrap into an already-due deadline")
	now = time.Unix(started.Unix()+128849018820, 0).UTC()
	require.Equal(t, 100*time.Millisecond, svc.workerWait(t.Context()))
	require.NoError(t, svc.runDue(t.Context()))
	require.Len(t, store.polls, 2)
}

func TestLocalGroupWorkerPollRequiresDurableReservation(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
			store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}}
			svc, settings := localGroupPollFixture(t, store, &localGroupPolicySender{}, &now, 300, false)
			wantErr := errors.New("poll checkpoint unavailable")
			failing := &localGroupPollSettingsFailureStore{globalSettingsMemoryStore: settings}
			if operation == "read" {
				failing.readErr = wantErr
			} else {
				failing.writeErr = wantErr
			}
			svc.SetGlobalSettingsStore(failing)
			require.ErrorIs(t, svc.runDue(t.Context()), wantErr)
			require.Empty(t, store.polls, "a failed checkpoint must not permit an unreserved poll")

			failing.readErr, failing.writeErr = nil, nil
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 1, "a recovered store must permit the first poll")
			now = now.Add(time.Second)
			require.NoError(t, svc.runDue(t.Context()))
			require.Len(t, store.polls, 1)
		})
	}
}

func TestLocalGroupWorkerPollFailureStillReservesItsCadence(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	started := now
	wantErr := errors.New("outbox query failed")
	store := &localGroupPollMemoryStore{memoryStore: &memoryStore{}, dueErr: wantErr}
	svc, _ := localGroupPollFixture(t, store, &localGroupPolicySender{}, &now, 300, false)
	require.ErrorIs(t, svc.runDue(t.Context()), wantErr)
	now = started.Add(time.Second)
	require.NoError(t, svc.runDue(t.Context()), "another task must not force an immediate outbox-query retry")
	require.Len(t, store.polls, 1)
	store.dueErr = nil
	now = started.Add(300 * time.Second)
	require.NoError(t, svc.runDue(t.Context()))
	require.Len(t, store.polls, 2)
}

func localGroupPollFixture(t *testing.T, store *localGroupPollMemoryStore, sender LocalGroupChangeNotifier, now *time.Time, interval int64, durableKey bool) (*Service, *globalSettingsMemoryStore) {
	t.Helper()
	settings := defaultGlobalSettings()
	settings.LocalMonitor.Enabled = true
	settings.LocalMonitor.IntervalSeconds = interval
	settings.AstrBot.Enabled = true
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	settingsStore := &globalSettingsMemoryStore{values: map[string]string{globalSettingsKey: string(raw)}}
	svc := NewService(store, nil, nil, fakeCipher{}, durableKey)
	svc.now = func() time.Time { return *now }
	svc.SetGlobalSettingsStore(settingsStore)
	svc.SetLocalGroupChangeNotifier(sender)
	return svc, settingsStore
}

// This test store preserves the outbox's status, lease and retry boundaries;
// only persistence and the external notification transport are in memory.
type localGroupPollMemoryStore struct {
	*memoryStore
	events   []LocalGroupChangeEvent
	polls    []time.Time
	nextSite *time.Time
	dueErr   error
}

func (s *localGroupPollMemoryStore) DueSites(context.Context, time.Time, int) ([]Site, error) {
	return nil, nil
}

func (s *localGroupPollMemoryStore) NextSiteDueAt(context.Context) (*time.Time, error) {
	return s.nextSite, nil
}

func (s *localGroupPollMemoryStore) EnqueueLocalGroupChange(_ context.Context, event LocalGroupChangeEvent) error {
	event.ID = int64(len(s.events) + 1)
	s.events = append(s.events, event)
	return nil
}

func (s *localGroupPollMemoryStore) DueLocalGroupChanges(_ context.Context, now time.Time, limit int) ([]LocalGroupChangeEvent, error) {
	s.polls = append(s.polls, now)
	if s.dueErr != nil {
		return nil, s.dueErr
	}
	var due []LocalGroupChangeEvent
	for _, event := range s.events {
		if (event.Status == localGroupChangePending || event.Status == localGroupChangeSending) && !event.NextAttemptAt.After(now) && len(due) < limit {
			due = append(due, event)
		}
	}
	return due, nil
}

func (s *localGroupPollMemoryStore) ClaimLocalGroupChange(_ context.Context, id int64, now, next time.Time) (bool, error) {
	for i := range s.events {
		event := &s.events[i]
		if event.ID == id && (event.Status == localGroupChangePending || event.Status == localGroupChangeSending) && !event.NextAttemptAt.After(now) {
			event.Status, event.NextAttemptAt = localGroupChangeSending, next
			return true, nil
		}
	}
	return false, nil
}

func (s *localGroupPollMemoryStore) CompleteLocalGroupChange(_ context.Context, id int64, processedAt time.Time, sendErr error, next time.Time) error {
	for i := range s.events {
		event := &s.events[i]
		if event.ID == id && event.Status == localGroupChangeSending {
			event.NextAttemptAt = next
			if sendErr == nil {
				event.Status, event.ProcessedAt, event.LastError = localGroupChangeSent, &processedAt, ""
			} else {
				event.Status, event.LastError = localGroupChangePending, sendErr.Error()
				event.Attempts++
			}
			return nil
		}
	}
	return ErrConflict
}

type localGroupPollSettingsFailureStore struct {
	*globalSettingsMemoryStore
	readErr, writeErr error
}

func (s *localGroupPollSettingsFailureStore) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	for _, key := range keys {
		if key == "upstream_governance_local_group_last_polled_at" && s.readErr != nil {
			return nil, s.readErr
		}
	}
	return s.globalSettingsMemoryStore.GetMultiple(ctx, keys)
}

func (s *localGroupPollSettingsFailureStore) SetMultiple(ctx context.Context, values map[string]string) error {
	if _, ok := values["upstream_governance_local_group_last_polled_at"]; ok && s.writeErr != nil {
		return s.writeErr
	}
	return s.globalSettingsMemoryStore.SetMultiple(ctx, values)
}
