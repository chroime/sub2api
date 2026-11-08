package repository

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestGroupEntityInputRoutingRoundTripOwnsTargets(t *testing.T) {
	entity := &dbent.Group{ID: 7, InputRouting: domain.GroupInputRoutingConfig{Enabled: true, Rules: []domain.GroupInputRoutingRule{{ID: "small", Enabled: true, RequestType: "all", MaxInputTokens: 2000, AccountIDs: []int64{11}, GroupIDs: []int64{21}, FallbackToOriginal: true}}}}
	group := groupEntityToService(entity)
	require.Equal(t, entity.InputRouting, group.InputRouting)
	group.InputRouting.Rules[0].AccountIDs[0] = 99
	group.InputRouting.Rules[0].GroupIDs[0] = 99
	require.Equal(t, []int64{11}, entity.InputRouting.Rules[0].AccountIDs)
	require.Equal(t, []int64{21}, entity.InputRouting.Rules[0].GroupIDs)
}
