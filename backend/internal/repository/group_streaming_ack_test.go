package repository

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestGroupEntityStreamingACKPreservesTriState(t *testing.T) {
	disabled, enabled := false, true
	for _, policy := range []*bool{nil, &disabled, &enabled} {
		group := groupEntityToService(&dbent.Group{StreamingAckEnabled: policy})
		require.Equal(t, policy, group.StreamingACKEnabled)
	}
}
