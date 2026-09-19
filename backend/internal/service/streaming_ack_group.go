package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// groupStreamingACKEnabled preserves membership/capability checks while moving
// the preference to the group. A nil policy alone uses the legacy account flag.
func groupStreamingACKEnabled(account *Account, group *Group, groupID *int64) bool {
	if !account.SupportsStreamingACK() || !account.belongsToStreamingACKGroup(groupID) {
		return false
	}
	if group != nil {
		if groupID == nil || group.ID != *groupID || (!SupportsStreamingACKPlatform(group.Platform) && group.Platform != PlatformComposite) {
			return false
		}
		if group.StreamingACKEnabled != nil {
			return *group.StreamingACKEnabled
		}
	}
	return account.IsStreamingACKEnabled()
}

func streamingACKGroupFromContext(ctx context.Context, groupID *int64) *Group {
	if ctx == nil || groupID == nil || *groupID <= 0 {
		return nil
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if !IsGroupContextValid(group) || group.ID != *groupID {
		return nil
	}
	return group
}

// IsStreamingACKEnabledForRequest applies the scheduler's effective group,
// including the Claude-Code-only fallback and forced-platform routing rules.
func (s *GatewayService) IsStreamingACKEnabledForRequest(ctx context.Context, account *Account, groupID *int64) bool {
	if !account.SupportsStreamingACK() {
		return false
	}
	if groupID == nil || *groupID <= 0 {
		return account.IsStreamingACKEnabledForGroup(groupID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.groupRepo == nil {
		// Missing repositories occur in isolated handler fixtures. A hydrated
		// auth context still carries an authoritative group policy in this case.
		return groupStreamingACKEnabled(account, streamingACKGroupFromContext(ctx, groupID), groupID)
	}
	group, effectiveGroupID, err := s.checkClaudeCodeRestriction(ctx, groupID)
	if err != nil || effectiveGroupID == nil {
		return false
	}
	// Forced-platform routing deliberately skips the Claude-only restriction
	// and returns only the group ID. It still needs that group's ACK policy.
	if group == nil {
		group, err = s.resolveGroupByID(ctx, *effectiveGroupID)
	}
	return err == nil && group != nil && groupStreamingACKEnabled(account, group, effectiveGroupID)
}

// The OpenAI routes keep the authenticated group when switching accounts or
// resolving a composite target platform. Read its hydrated snapshot first and
// fall back to the injected group reader if the context lacks one.
func (s *OpenAIGatewayService) IsStreamingACKEnabledForRequest(ctx context.Context, account *Account, groupID *int64) bool {
	if !account.SupportsStreamingACK() {
		return false
	}
	if groupID == nil || *groupID <= 0 {
		return account.IsStreamingACKEnabledForGroup(groupID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	group := streamingACKGroupFromContext(ctx, groupID)
	if group == nil && s != nil && s.settingService != nil && s.settingService.defaultSubGroupReader != nil {
		var err error
		group, err = s.settingService.defaultSubGroupReader.GetByID(ctx, *groupID)
		if err != nil || group == nil {
			return false
		}
	}
	return groupStreamingACKEnabled(account, group, groupID)
}
