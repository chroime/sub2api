package service

import (
	"context"
	"errors"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *adminServiceImpl) normalizeAndValidateGroupInputRouting(ctx context.Context, sourceID int64, _ string, config GroupInputRoutingConfig) (GroupInputRoutingConfig, error) {
	config, err := NormalizeGroupInputRouting(config)
	if err != nil {
		return GroupInputRoutingConfig{}, infraerrors.BadRequest("INVALID_GROUP_INPUT_ROUTING", err.Error())
	}
	accountIDs := make([]int64, 0)
	seenAccounts := make(map[int64]struct{})
	seenGroups := make(map[int64]struct{})
	for _, rule := range config.Rules {
		for _, id := range rule.AccountIDs {
			if _, exists := seenAccounts[id]; !exists {
				seenAccounts[id] = struct{}{}
				accountIDs = append(accountIDs, id)
			}
		}
		for _, id := range rule.GroupIDs {
			if id == sourceID {
				return GroupInputRoutingConfig{}, infraerrors.BadRequest("INVALID_GROUP_INPUT_ROUTING", "input routing cannot target its own group")
			}
			if _, exists := seenGroups[id]; exists {
				continue
			}
			seenGroups[id] = struct{}{}
			if s.groupRepo == nil {
				return GroupInputRoutingConfig{}, fmt.Errorf("group repository is not configured")
			}
			target, loadErr := s.groupRepo.GetByID(ctx, id)
			if loadErr != nil && !errors.Is(loadErr, ErrGroupNotFound) {
				return GroupInputRoutingConfig{}, fmt.Errorf("load input routing target group %d: %w", id, loadErr)
			}
			if target == nil || errors.Is(loadErr, ErrGroupNotFound) {
				return GroupInputRoutingConfig{}, infraerrors.BadRequest("INVALID_GROUP_INPUT_ROUTING", fmt.Sprintf("input routing target group %d does not exist", id))
			}
		}
	}
	if len(accountIDs) > 0 {
		if s.accountRepo == nil {
			return GroupInputRoutingConfig{}, fmt.Errorf("account repository is not configured")
		}
		accounts, loadErr := s.accountRepo.GetByIDs(ctx, accountIDs)
		if loadErr != nil {
			return GroupInputRoutingConfig{}, fmt.Errorf("load input routing target accounts: %w", loadErr)
		}
		found := make(map[int64]*Account, len(accounts))
		for _, account := range accounts {
			if account != nil {
				found[account.ID] = account
			}
		}
		for _, id := range accountIDs {
			account := found[id]
			if account == nil {
				return GroupInputRoutingConfig{}, infraerrors.BadRequest("INVALID_GROUP_INPUT_ROUTING", fmt.Sprintf("input routing target account %d does not exist", id))
			}
			if !isConcreteRequestPlatform(account.Platform) {
				return GroupInputRoutingConfig{}, infraerrors.BadRequest("INVALID_GROUP_INPUT_ROUTING", fmt.Sprintf("input routing target account %d has invalid platform %s", id, account.Platform))
			}
		}
	}
	return config, nil
}
