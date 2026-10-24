package upstreamgovernance

import (
	"context"
	"time"
)

const localGroupLastPolledAtKey = "upstream_governance_local_group_last_polled_at"

// NewLocalGroupChangeQueue returns the optional local-group queue for an
// application service. The database trigger writes directly to the store; the
// caller only needs to provide a transport adapter and schedule Dispatch on
// the normal background notification tick.
func (s *Service) NewLocalGroupChangeQueue(sender LocalGroupChangeNotifier) *LocalGroupChangeQueue {
	if s == nil {
		return nil
	}
	store, ok := s.store.(LocalGroupChangeStore)
	if !ok {
		return nil
	}
	return NewLocalGroupChangeQueue(store, sender, s.now)
}

// DispatchLocalGroupChangeNotifications applies the global policy and reserves
// the local monitor's own cadence before polling. Other worker deadlines may
// wake the scheduler sooner without advancing this queue. Disabling the channel
// or monitor pauses delivery; per-event switches consume unrelated audits.
func (s *Service) DispatchLocalGroupChangeNotifications(ctx context.Context, sender LocalGroupChangeNotifier, limit int) error {
	if s == nil {
		return nil
	}
	queue := s.NewLocalGroupChangeQueue(sender)
	if queue == nil {
		return nil
	}
	settings, reserved, err := s.reserveLocalGroupPoll(ctx)
	if err != nil || !reserved {
		return err
	}
	queue.Events = &settings.AstrBot.Events
	return queue.Dispatch(ctx, limit)
}

// The caller holds localGroupPollMu. The checkpoint is separate from the global
// settings JSON/version and the outbox's per-event retry/lease timestamps.
func (s *Service) localGroupPollDeadline(ctx context.Context, interval int64) (*time.Time, error) {
	values, err := s.globalSettingsStore.GetMultiple(ctx, []string{localGroupLastPolledAtKey})
	if err != nil {
		return nil, err
	}
	next := s.now()
	if raw := values[localGroupLastPolledAtKey]; raw != "" {
		last, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, err
		}
		// Recompute from the last poll so cadence edits take effect without
		// restarting the worker, including intervals larger than time.Duration.
		next = addSeconds(last, interval)
	}
	return &next, nil
}

func (s *Service) nextLocalGroupPollAt(ctx context.Context) (*time.Time, error) {
	s.localGroupPollMu.Lock()
	defer s.localGroupPollMu.Unlock()
	settings, err := s.GetGlobalSettings(ctx)
	if err != nil || !settings.AstrBot.Enabled || !settings.LocalMonitor.Enabled {
		return nil, err
	}
	if _, ok := s.store.(LocalGroupChangeStore); !ok {
		return nil, nil
	}
	return s.localGroupPollDeadline(ctx, settings.LocalMonitor.IntervalSeconds)
}

func (s *Service) reserveLocalGroupPoll(ctx context.Context) (GlobalSettings, bool, error) {
	s.localGroupPollMu.Lock()
	defer s.localGroupPollMu.Unlock()
	settings, err := s.GetGlobalSettings(ctx)
	if err != nil || !settings.AstrBot.Enabled || !settings.LocalMonitor.Enabled {
		return settings, false, err
	}
	next, err := s.localGroupPollDeadline(ctx, settings.LocalMonitor.IntervalSeconds)
	if err != nil {
		return settings, false, err
	}
	now := s.now().UTC()
	if next.After(now) {
		return settings, false, nil
	}
	// Reserve durably before querying or sending. Empty/failed polls keep the
	// same cadence, and a restart cannot make the queue immediately due again.
	// The queue retains its existing atomic claims and failure retry deadlines.
	err = s.globalSettingsStore.SetMultiple(ctx, map[string]string{localGroupLastPolledAtKey: now.Format(time.RFC3339Nano)})
	return settings, err == nil, err
}
