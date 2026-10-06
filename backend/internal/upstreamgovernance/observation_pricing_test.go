package upstreamgovernance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObservationPolicyValidationAllowsArbitraryPositiveSeconds(t *testing.T) {
	policy := ObservationPolicy{Enabled: true, FastIntervalSeconds: 1, FullIntervalSeconds: 7, DecreaseStabilitySeconds: 60, MaxRateIncreasePercent: 0}
	require.NoError(t, validateObservationPolicy(policy))
}

func TestObservationPolicyValidationRejectsNonPositiveSeconds(t *testing.T) {
	for _, policy := range []ObservationPolicy{
		{FastIntervalSeconds: 0, FullIntervalSeconds: 60},
		{FastIntervalSeconds: 1, FullIntervalSeconds: -1},
		{FastIntervalSeconds: 1, FullIntervalSeconds: 60, DecreaseStabilitySeconds: -1},
	} {
		require.ErrorIs(t, validateObservationPolicy(policy), ErrInvalid)
	}
}
