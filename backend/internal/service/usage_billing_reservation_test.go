package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageBalanceReservationCommandNormalizeUsesStableDefaults(t *testing.T) {
	cmd := &UsageBalanceReservationCommand{
		RequestID:  "  local:req-1  ",
		APIKeyID:   7,
		UserID:     42,
		HoldAmount: 1.234567895,
	}

	cmd.Normalize(time.Unix(100, 0))

	require.Equal(t, "local:req-1", cmd.RequestID)
	require.InDelta(t, 1.23456790, cmd.HoldAmount, 0.000000001)
	require.False(t, cmd.ExpiresAt.IsZero())
	require.Equal(t, "held", cmd.Status)
}

func TestUsageBalanceReservationCommandRejectsExpiredAndInvalidAmounts(t *testing.T) {
	now := time.Unix(100, 0)

	for name, cmd := range map[string]*UsageBalanceReservationCommand{
		"negative hold":   {RequestID: "req", UserID: 1, APIKeyID: 2, HoldAmount: -1},
		"missing request": {UserID: 1, APIKeyID: 2, HoldAmount: 1},
		"missing user":    {RequestID: "req", APIKeyID: 2, HoldAmount: 1},
		"missing key":     {RequestID: "req", UserID: 1, HoldAmount: 1},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, cmd.Validate(now))
		})
	}

	expired := &UsageBalanceReservationCommand{RequestID: "req", UserID: 1, APIKeyID: 2, HoldAmount: 1, ExpiresAt: now}
	require.Error(t, expired.Validate(now))
}
