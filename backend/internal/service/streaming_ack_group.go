package service

import "context"

// IsStreamingACKEnabledForRequest applies the scheduler's effective group,
// including the Claude-Code-only fallback and forced-platform routing rules.
func (s *GatewayService) IsStreamingACKEnabledForRequest(ctx context.Context, account *Account, groupID *int64) bool {
	if !account.IsStreamingACKEnabled() {
		return false
	}
	if s == nil || s.groupRepo == nil || groupID == nil || *groupID <= 0 {
		return account.IsStreamingACKEnabledForGroup(groupID)
	}
	_, effectiveGroupID, err := s.checkClaudeCodeRestriction(ctx, groupID)
	return err == nil && account.IsStreamingACKEnabledForGroup(effectiveGroupID)
}
