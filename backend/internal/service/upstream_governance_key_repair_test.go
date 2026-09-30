package service

import (
	"context"
	"testing"
	"time"

	gov "github.com/Wei-Shaw/sub2api/internal/upstreamgovernance"
	"github.com/stretchr/testify/require"
)

type repairAdminStub struct {
	governanceAdmin
	got gov.KeyRepairCommitRequest
}

func (s *repairAdminStub) CommitKeyRepair(_ context.Context, request gov.KeyRepairCommitRequest) error {
	s.got = request
	return nil
}

type repairRepoStub struct {
	AccountRepository
	got gov.KeyRepairCommitRequest
}

func (s *repairRepoStub) CommitKeyRepair(_ context.Context, request gov.KeyRepairCommitRequest) error {
	s.got = request
	return nil
}

func TestGovernanceKeyRepairCommitForwarding(t *testing.T) {
	request := gov.KeyRepairCommitRequest{Repair: gov.KeyRepair{ID: "repair-1"}, CandidateVerifiedAt: time.Now()}
	admin := &repairAdminStub{}
	local := &governanceLocalAccounts{admin: admin}
	require.NoError(t, local.CommitKeyRepair(t.Context(), request))
	require.Equal(t, request, admin.got)
	repo := &repairRepoStub{}
	service := &adminServiceImpl{accountRepo: repo}
	require.NoError(t, service.CommitKeyRepair(t.Context(), request))
	require.Equal(t, request, repo.got)
}
