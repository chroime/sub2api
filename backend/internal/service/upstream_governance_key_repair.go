package service

import (
	"context"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
)

func (l *governanceLocalAccounts) CommitKeyRepair(ctx context.Context, request gov.KeyRepairCommitRequest) error {
	committer, ok := l.admin.(gov.KeyRepairCommitter)
	if !ok {
		return gov.ErrUnsupported
	}
	return committer.CommitKeyRepair(ctx, request)
}

func (s *adminServiceImpl) CommitKeyRepair(ctx context.Context, request gov.KeyRepairCommitRequest) error {
	committer, ok := s.accountRepo.(gov.KeyRepairCommitter)
	if !ok {
		return gov.ErrUnsupported
	}
	return committer.CommitKeyRepair(ctx, request)
}

var _ gov.KeyRepairCommitter = (*governanceLocalAccounts)(nil)
