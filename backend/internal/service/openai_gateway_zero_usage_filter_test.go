package service

import "testing"

func TestIsZeroTokenZeroCostOpenAIUsage(t *testing.T) {
	zeroAccountStatsCost := 0.0
	tests := []struct {
		name   string
		newLog func() *UsageLog
		want   bool
	}{
		{name: "nil log", newLog: func() *UsageLog { return nil }, want: false},
		{name: "all zero", newLog: func() *UsageLog { return &UsageLog{} }, want: true},
		{name: "input tokens", newLog: func() *UsageLog { return &UsageLog{InputTokens: 1} }, want: false},
		{name: "output tokens", newLog: func() *UsageLog { return &UsageLog{OutputTokens: 1} }, want: false},
		{name: "cache creation tokens", newLog: func() *UsageLog { return &UsageLog{CacheCreationTokens: 1} }, want: false},
		{name: "cache read tokens", newLog: func() *UsageLog { return &UsageLog{CacheReadTokens: 1} }, want: false},
		{name: "cache creation 5m tokens", newLog: func() *UsageLog { return &UsageLog{CacheCreation5mTokens: 1} }, want: false},
		{name: "cache creation 1h tokens", newLog: func() *UsageLog { return &UsageLog{CacheCreation1hTokens: 1} }, want: false},
		{name: "image input tokens", newLog: func() *UsageLog { return &UsageLog{ImageInputTokens: 1} }, want: false},
		{name: "image output tokens", newLog: func() *UsageLog { return &UsageLog{ImageOutputTokens: 1} }, want: false},
		{name: "total cost", newLog: func() *UsageLog { return &UsageLog{TotalCost: 0.01} }, want: false},
		{name: "actual cost", newLog: func() *UsageLog { return &UsageLog{ActualCost: 0.01} }, want: false},
		{name: "account stats cost", newLog: func() *UsageLog { cost := 0.01; return &UsageLog{AccountStatsCost: &cost} }, want: false},
		{name: "explicit zero account stats cost", newLog: func() *UsageLog { return &UsageLog{AccountStatsCost: &zeroAccountStatsCost} }, want: true},
		{name: "negative token", newLog: func() *UsageLog { return &UsageLog{OutputTokens: -1} }, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.newLog()
			if got := isZeroTokenZeroCostOpenAIUsage(log); got != tt.want {
				t.Fatalf("isZeroTokenZeroCostOpenAIUsage() = %v, want %v", got, tt.want)
			}
		})
	}
}
