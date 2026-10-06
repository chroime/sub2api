package upstreamgovernance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelSecuritySummaryExposesTokenDifferenceAndQualityVerdicts(t *testing.T) {
	inputDelta, outputDelta := 42.5, -12.25
	summary := summarizeModelSecurity("candy", ModelRunResult{
		CandyAnswer:  ptrInt(21),
		CandyVerdict: "numeric_correct",
		Tokens: ModelTokenAudit{
			InputState:         "suspected_difference",
			OutputState:        "within_tolerance",
			InputDeltaPercent:  &inputDelta,
			OutputDeltaPercent: &outputDelta,
		},
	})
	require.Equal(t, "suspicious", summary.TrustState)
	require.Equal(t, "需要复核", summary.TrustLabel)
	require.True(t, summary.TokenDifference)
	require.Equal(t, 42.5, *summary.InputDeltaPercent)
	require.Equal(t, "numeric_correct", summary.CandyVerdict)
	require.Equal(t, "答案正确", summary.CandyVerdictLabel)
	require.Contains(t, summary.Summary, "令牌")
}

func TestModelSecuritySummaryKeepsPelicanAsAvailabilityMetadata(t *testing.T) {
	summary := summarizeModelSecurity("pelican", ModelRunResult{HTML: "<html><body><svg></svg></body></html>"})
	require.Equal(t, "unverified", summary.TrustState)
	require.False(t, summary.TokenDifference)
	require.True(t, summary.PelicanHTMLAvailable)
	require.Equal(t, len("<html><body><svg></svg></body></html>"), summary.PelicanHTMLBytes)
	raw, err := json.Marshal(summary)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "<svg>")
}

func ptrInt(value int) *int { return &value }
