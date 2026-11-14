package repository

import (
	"context"
	"fmt"
	"strings"

	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitorhistory"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *channelMonitorRepository) FindMonitorLocalKey(ctx context.Context, credential string) (*service.MonitorLocalKeyBinding, error) {
	k, err := clientFromContext(ctx, r.client).APIKey.Query().
		Where(apikey.KeyEQ(credential), apikey.DeletedAtIsNil()).
		Select(apikey.FieldID, apikey.FieldGroupID).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find local monitor credential: %w", err)
	}
	return &service.MonitorLocalKeyBinding{ID: k.ID, GroupID: k.GroupID}, nil
}

func monitorBindingGroup(g *dbent.Group) *service.MonitorBindingGroup {
	return &service.MonitorBindingGroup{ID: g.ID, Name: g.Name, Platform: g.Platform, Status: g.Status}
}

func (r *channelMonitorRepository) FindMonitorBindingGroup(ctx context.Context, id int64) (*service.MonitorBindingGroup, error) {
	g, err := clientFromContext(ctx, r.client).Group.Query().Where(group.IDEQ(id), group.DeletedAtIsNil()).
		Select(group.FieldID, group.FieldName, group.FieldPlatform, group.FieldStatus).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find monitor group: %w", err)
	}
	return monitorBindingGroup(g), nil
}

func (r *channelMonitorRepository) FindLegacyMonitorBindingGroup(ctx context.Context, name, platform string) (*service.MonitorBindingGroup, error) {
	groups, err := clientFromContext(ctx, r.client).Group.Query().Where(
		group.NameEQ(strings.TrimSpace(name)), group.PlatformEQ(platform), group.DeletedAtIsNil(), group.StatusEQ("active"),
	).Select(group.FieldID, group.FieldName, group.FieldPlatform, group.FieldStatus).Limit(2).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve legacy monitor group: %w", err)
	}
	if len(groups) != 1 {
		return nil, nil
	}
	return monitorBindingGroup(groups[0]), nil
}

func (r *channelMonitorRepository) LoadMonitorBindingSources(ctx context.Context, monitors []*service.ChannelMonitor) (*service.MonitorBindingSources, error) {
	out := &service.MonitorBindingSources{Keys: make(map[int64]*service.MonitorLocalKeyBinding), Groups: make(map[int64]*service.MonitorBindingGroup)}
	var keyIDs, groupIDs []int64
	for _, m := range monitors {
		if m.LocalAPIKeyID != nil && m.GroupBindingMode == service.MonitorGroupBindingKey {
			keyIDs = append(keyIDs, *m.LocalAPIKeyID)
		} else if m.GroupID != nil {
			groupIDs = append(groupIDs, *m.GroupID)
		}
	}
	client := clientFromContext(ctx, r.client)
	if len(keyIDs) > 0 {
		keys, err := client.APIKey.Query().Where(apikey.IDIn(keyIDs...), apikey.DeletedAtIsNil()).
			Select(apikey.FieldID, apikey.FieldGroupID).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load monitor credential identities: %w", err)
		}
		for _, k := range keys {
			out.Keys[k.ID] = &service.MonitorLocalKeyBinding{ID: k.ID, GroupID: k.GroupID}
			if k.GroupID != nil {
				groupIDs = append(groupIDs, *k.GroupID)
			}
		}
	}
	if len(groupIDs) > 0 {
		groups, err := client.Group.Query().Where(group.IDIn(groupIDs...), group.DeletedAtIsNil()).
			Select(group.FieldID, group.FieldName, group.FieldPlatform, group.FieldStatus).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load monitor group identities: %w", err)
		}
		for _, g := range groups {
			out.Groups[g.ID] = monitorBindingGroup(g)
		}
	}
	return out, nil
}

// Compare the source identity before saving: a background read must never
// overwrite a credential/group that an administrator has just changed.
func (r *channelMonitorRepository) SaveMonitorGroupBinding(ctx context.Context, m *service.ChannelMonitor, before service.MonitorGroupBindingIdentity) (bool, error) {
	u := clientFromContext(ctx, r.client).ChannelMonitor.Update().Where(
		channelmonitor.IDEQ(m.ID), channelmonitor.GroupBindingModeEQ(before.Mode),
		channelmonitor.ProviderEQ(channelmonitor.Provider(before.Provider)),
	)
	if before.LocalAPIKeyID == nil {
		u.Where(channelmonitor.LocalAPIKeyIDIsNil())
	} else {
		u.Where(channelmonitor.LocalAPIKeyIDEQ(*before.LocalAPIKeyID))
	}
	if before.GroupID == nil {
		u.Where(channelmonitor.GroupIDIsNil())
	} else {
		u.Where(channelmonitor.GroupIDEQ(*before.GroupID))
	}
	u.SetGroupBindingMode(m.GroupBindingMode).SetGroupName(m.GroupName)
	if m.LocalAPIKeyID == nil {
		u.ClearLocalAPIKeyID()
	} else {
		u.SetLocalAPIKeyID(*m.LocalAPIKeyID)
	}
	if m.GroupID == nil {
		u.ClearGroupID()
	} else {
		u.SetGroupID(*m.GroupID)
	}
	if m.GroupBindingChangedAt != nil {
		u.SetGroupBindingChangedAt(*m.GroupBindingChangedAt)
	}
	if m.LastCheckedAt == nil {
		u.ClearLastCheckedAt()
	}
	n, err := u.Save(ctx)
	if err != nil {
		return false, fmt.Errorf("save monitor group binding: %w", err)
	}
	return n == 1, nil
}

// The key's entry group wins over manual display metadata. Resolve current
// membership here as well, so rates/IQ/usage cannot use an old group snapshot.
const monitorBoundGroupJoin = `
LEFT JOIN api_keys binding_key ON m.group_binding_mode='key'
 AND binding_key.id=m.local_api_key_id AND binding_key.deleted_at IS NULL
JOIN groups g ON g.id=CASE WHEN m.group_binding_mode='key' THEN binding_key.group_id ELSE m.group_id END
 AND g.platform=m.provider AND g.deleted_at IS NULL AND g.status='active'
 AND m.group_binding_mode IN ('key','manual','legacy')
`

// Applied to every public history projection. A rebinding keeps old records
// intact while starting a fresh current-association window.
const monitorHistoryBindingScope = `
 AND EXISTS (SELECT 1 FROM channel_monitors binding_monitor
 WHERE binding_monitor.id=h.monitor_id AND
 (binding_monitor.group_binding_changed_at IS NULL OR h.checked_at >= binding_monitor.group_binding_changed_at))
`

func monitorHistoryBindingPredicate(s *entsql.Selector) {
	m := entsql.Table(channelmonitor.Table).As("binding_monitor")
	s.Where(entsql.Exists(entsql.Select(m.C(channelmonitor.FieldID)).From(m).Where(entsql.And(
		entsql.ColumnsEQ(m.C(channelmonitor.FieldID), s.C(channelmonitorhistory.FieldMonitorID)),
		entsql.Or(entsql.IsNull(m.C(channelmonitor.FieldGroupBindingChangedAt)),
			entsql.ColumnsGTE(s.C(channelmonitorhistory.FieldCheckedAt), m.C(channelmonitor.FieldGroupBindingChangedAt))),
	))))
}
