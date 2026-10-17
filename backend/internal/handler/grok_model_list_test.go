package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrok47BuildFastAdvertisesReasoningConfiguration(t *testing.T) {
	for _, model := range []string{"grok-4.7-build-fast", "grok-4.7-build-fast-latest"} {
		require.True(t, grokModelSupportsConfigurableReasoning(model), model)
	}
}
