package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	MonitorGroupBindingLegacy = "legacy"
	MonitorGroupBindingKey    = "key"
	MonitorGroupBindingManual = "manual"
	MonitorGroupBindingNone   = "none"
)

var ErrChannelMonitorGroupBinding = infraerrors.BadRequest("CHANNEL_MONITOR_GROUP_BINDING_INVALID", "The monitor's local group or API key is unavailable or belongs to another platform")

var errMonitorBindingChangedConcurrently = errors.New("monitor binding changed concurrently")

// Only identities and presentation fields cross this boundary, never secrets.
type MonitorLocalKeyBinding struct {
	ID      int64
	GroupID *int64
}

type MonitorBindingGroup struct {
	ID       int64
	Name     string
	Platform string
	Status   string
}

type MonitorGroupBindingIdentity struct {
	Mode          string
	LocalAPIKeyID *int64
	GroupID       *int64
	Provider      string
}

type MonitorBindingSources struct {
	Keys   map[int64]*MonitorLocalKeyBinding
	Groups map[int64]*MonitorBindingGroup
}

// Optional so isolated probe-only stores do not need local credential access.
type monitorGroupBindingStore interface {
	FindMonitorLocalKey(context.Context, string) (*MonitorLocalKeyBinding, error)
	FindMonitorBindingGroup(context.Context, int64) (*MonitorBindingGroup, error)
	FindLegacyMonitorBindingGroup(context.Context, string, string) (*MonitorBindingGroup, error)
	LoadMonitorBindingSources(context.Context, []*ChannelMonitor) (*MonitorBindingSources, error)
	SaveMonitorGroupBinding(context.Context, *ChannelMonitor, MonitorGroupBindingIdentity) (bool, error)
}

func monitorBindingIdentity(m *ChannelMonitor) MonitorGroupBindingIdentity {
	return MonitorGroupBindingIdentity{m.GroupBindingMode, cloneInt64Pointer(m.LocalAPIKeyID), cloneInt64Pointer(m.GroupID), m.Provider}
}

func sameMonitorBindingID(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func monitorBindingChanged(m *ChannelMonitor, before MonitorGroupBindingIdentity) bool {
	if (before.Mode == "" || before.Mode == MonitorGroupBindingLegacy) && before.GroupID != nil && sameMonitorBindingID(m.GroupID, before.GroupID) {
		return false // A uniquely backfilled legacy association retains its history.
	}
	return !sameMonitorBindingID(m.GroupID, before.GroupID) || !sameMonitorBindingID(m.LocalAPIKeyID, before.LocalAPIKeyID) ||
		(before.Mode != "" && before.Mode != MonitorGroupBindingLegacy && before.Mode != m.GroupBindingMode)
}

func markMonitorBindingBoundary(m *ChannelMonitor, before MonitorGroupBindingIdentity) {
	if m.ID != 0 && monitorBindingChanged(m, before) {
		now := time.Now().UTC()
		m.GroupBindingChangedAt = &now
		m.AvailabilityReset = nil
		m.LastCheckedAt = nil
	}
}

func applyMonitorBindingGroup(m *ChannelMonitor, source *MonitorLocalKeyBinding, g *MonitorBindingGroup) {
	m.GroupName = ""
	m.GroupBindingStatus = m.GroupBindingMode
	if m.GroupBindingMode == MonitorGroupBindingKey && source == nil {
		m.GroupBindingStatus = "key_missing"
		return
	}
	if m.GroupID == nil {
		if m.GroupBindingMode == MonitorGroupBindingKey {
			m.GroupBindingStatus = "no_group"
		}
		return
	}
	if g == nil {
		m.GroupBindingStatus = "group_missing"
		return
	}
	m.GroupName = g.Name
	if g.Platform != m.Provider {
		m.GroupBindingStatus = "platform_mismatch"
	} else if g.Status != "active" {
		m.GroupBindingStatus = "group_inactive"
	}
}

// initializeMonitorBinding always prefers a locally recognized credential.
// External credentials may choose a group explicitly; only legacy conversion
// is allowed to resolve an old name, and only when it has one exact match.
func (s *ChannelMonitorService) initializeMonitorBinding(ctx context.Context, m *ChannelMonitor, plain string, requestedGroup *int64, legacy bool) error {
	store, ok := s.repo.(monitorGroupBindingStore)
	if !ok {
		return nil
	}
	before := monitorBindingIdentity(m)
	if requestedGroup != nil && *requestedGroup < 0 {
		return ErrChannelMonitorGroupBinding
	}
	var key *MonitorLocalKeyBinding
	var err error
	if defaultCheckMode(m.CheckMode) != MonitorCheckModeQuota && strings.TrimSpace(plain) != "" {
		key, err = store.FindMonitorLocalKey(ctx, strings.TrimSpace(plain))
		if err != nil {
			return fmt.Errorf("resolve monitor key: %w", err)
		}
	}
	m.LocalAPIKeyID = nil
	m.GroupBindingMode = MonitorGroupBindingNone
	var selected *int64
	if requestedGroup != nil && *requestedGroup > 0 {
		selected = requestedGroup
	} else if requestedGroup == nil && (legacy || before.Mode == MonitorGroupBindingManual) {
		selected = before.GroupID
	}
	var g *MonitorBindingGroup
	if key != nil {
		m.GroupBindingMode = MonitorGroupBindingKey
		m.LocalAPIKeyID = &key.ID
		selected = key.GroupID
	} else if legacy && selected == nil && strings.TrimSpace(m.GroupName) != "" {
		g, err = store.FindLegacyMonitorBindingGroup(ctx, m.GroupName, m.Provider)
		if err != nil {
			return err
		}
		if g != nil {
			selected = &g.ID
		}
	}
	m.GroupID = cloneInt64Pointer(selected)
	if key == nil && selected != nil {
		m.GroupBindingMode = MonitorGroupBindingManual
	}
	if selected != nil && g == nil {
		g, err = store.FindMonitorBindingGroup(ctx, *selected)
		if err != nil {
			return err
		}
	}
	applyMonitorBindingGroup(m, key, g)
	if !legacy && m.GroupBindingStatus != MonitorGroupBindingKey && m.GroupBindingStatus != MonitorGroupBindingManual && m.GroupBindingStatus != MonitorGroupBindingNone {
		return ErrChannelMonitorGroupBinding
	}
	markMonitorBindingBoundary(m, before)
	return nil
}

func (s *ChannelMonitorService) refreshMonitorBindings(ctx context.Context, monitors []*ChannelMonitor) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := s.refreshMonitorBindingsOnce(ctx, monitors)
		if !errors.Is(err, errMonitorBindingChangedConcurrently) {
			return err
		}
		for _, m := range monitors {
			fresh, err := s.repo.GetByID(ctx, m.ID)
			if err != nil {
				return err
			}
			*m = *fresh
		}
	}
	return ErrChannelMonitorGroupBinding
}

func (s *ChannelMonitorService) refreshMonitorBindingsOnce(ctx context.Context, monitors []*ChannelMonitor) error {
	store, ok := s.repo.(monitorGroupBindingStore)
	if !ok || len(monitors) == 0 {
		return nil
	}
	for _, m := range monitors {
		if m.GroupBindingMode != "" && m.GroupBindingMode != MonitorGroupBindingLegacy {
			continue
		}
		before := monitorBindingIdentity(m)
		plain := ""
		if s.encryptor != nil && m.APIKey != "" {
			var err error
			plain, err = s.encryptor.Decrypt(m.APIKey)
			if err != nil {
				// Keep legacy state retryable when encryption configuration is repaired.
				m.GroupBindingStatus = "none"
				continue
			}
		}
		if err := s.initializeMonitorBinding(ctx, m, plain, nil, true); err != nil {
			return err
		}
		if saved, err := store.SaveMonitorGroupBinding(ctx, m, before); err != nil {
			return err
		} else if !saved {
			return errMonitorBindingChangedConcurrently
		}
	}
	sources, err := store.LoadMonitorBindingSources(ctx, monitors)
	if err != nil {
		return err
	}
	for _, m := range monitors {
		before := monitorBindingIdentity(m)
		if (m.GroupBindingMode == "" || m.GroupBindingMode == MonitorGroupBindingLegacy) && m.GroupID == nil {
			continue
		}
		oldName := m.GroupName
		var key *MonitorLocalKeyBinding
		if m.GroupBindingMode == MonitorGroupBindingKey {
			if m.LocalAPIKeyID != nil {
				key = sources.Keys[*m.LocalAPIKeyID]
			}
			m.GroupID = nil
			if key != nil {
				m.GroupID = cloneInt64Pointer(key.GroupID)
			}
		}
		var g *MonitorBindingGroup
		if m.GroupID != nil {
			g = sources.Groups[*m.GroupID]
		}
		applyMonitorBindingGroup(m, key, g)
		markMonitorBindingBoundary(m, before)
		if monitorBindingChanged(m, before) || oldName != m.GroupName {
			if saved, err := store.SaveMonitorGroupBinding(ctx, m, before); err != nil {
				return err
			} else if !saved {
				return errMonitorBindingChangedConcurrently
			}
		}
		if m.GroupBindingChangedAt != nil && m.AvailabilityReset != nil && m.AvailabilityReset.ResetAt.Before(*m.GroupBindingChangedAt) {
			m.AvailabilityReset = nil
		}
	}
	return nil
}
