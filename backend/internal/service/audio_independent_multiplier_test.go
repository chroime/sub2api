package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAudioIndependentMultiplierAppliesToGatewayBilling(t *testing.T) {
	t.Parallel()
	groupID := int64(77)
	price := 0.25
	apiKey := &APIKey{
		ID: 10, GroupID: &groupID,
		Group: &Group{
			ID: groupID, AudioTTSPricePerMillionChars: &price,
			AudioRateIndependent: true, AudioRateMultiplier: 2,
		},
	}
	usage := &ForwardResult{AudioUsage: &AudioUsage{Mode: "tts", DurationOrUnits: 2}}

	gatewayCost := (&GatewayService{billingService: &BillingService{}}).calculateRecordUsageCost(
		context.Background(), usage, apiKey, "voice-model", 7, 7, time.Time{},
	)
	require.InDelta(t, 0.5, gatewayCost.TotalCost, 1e-12)
	require.InDelta(t, 1.0, gatewayCost.ActualCost, 1e-12)

	openAICost, err := (&OpenAIGatewayService{billingService: &BillingService{}}).calculateOpenAIRecordUsageCost(
		context.Background(), &OpenAIForwardResult{AudioUsage: &AudioUsage{Mode: "tts", DurationOrUnits: 2}},
		apiKey, []string{"voice-model"}, 7, 7, 7, 7, UsageTokens{}, "", nil, false, time.Time{},
	)
	require.NoError(t, err)
	require.InDelta(t, 0.5, openAICost.TotalCost, 1e-12)
	require.InDelta(t, 1.0, openAICost.ActualCost, 1e-12)
}
