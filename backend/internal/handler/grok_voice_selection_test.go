package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokVoiceSelectionDoesNotApplyTextModelPricingRestriction(t *testing.T) {
	// Voice routing is capability based. A text model placeholder would make
	// channel-level restrict_models settings reject a valid TTS/STT request
	// before the Grok account is selected.
	require.Empty(t, grokVoiceSelectionModel())
}
