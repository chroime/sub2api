package upstreamgovernance

import (
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestManagedKeyNameUsesLocalCreationDateAndPreservesSuffix(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("Asia/Shanghai", 8*60*60)
	t.Cleanup(func() { time.Local = previous })
	created := time.Date(2026, 9, 26, 16, 8, 0, 0, time.UTC)
	rate := 0.06
	require.Equal(t, "codex特惠-0.06-20260927", managedKeyName(RemoteGroup{ID: "8", Name: "codex特惠", ResolvedRateMultiplier: &rate}, created, "sub2api"))
	require.Equal(t, "8-0.06-20260927", managedKeyName(RemoteGroup{ID: "8", Name: "  ", ResolvedRateMultiplier: &rate}, created, "sub2api"))
	rate = 999999.9999
	for _, platform := range []string{"sub2api", "newapi"} {
		name := managedKeyName(RemoteGroup{ID: "8", Name: strings.Repeat("中文分组😀", 50), ResolvedRateMultiplier: &rate}, created, platform)
		require.True(t, utf8.ValidString(name))
		require.True(t, strings.HasSuffix(name, "-999999.9999-20260927"))
		require.LessOrEqual(t, len(name), 100)
		if platform == "newapi" {
			require.LessOrEqual(t, utf8.RuneCountInString(name), 30)
		}
	}
}

func TestManagedKeyNameUsesEffectiveRateWithoutTrailingZeros(t *testing.T) {
	created := time.Date(2026, 9, 27, 12, 0, 0, 0, time.Local)
	declared := 2.0
	for _, tc := range []struct {
		label string
		value float64
		want  string
	}{
		{"override", 0.8, "0.8"},
		{"integer", 2, "2"},
		{"zero", 0, "0"},
		{"negative zero", math.Copysign(0, -1), "0"},
		{"precision", 1.23456, "1.2346"},
		{"rounds to zero", 0.00001, "0"},
		{"negative", -1, "未知"},
		{"not a number", math.NaN(), "未知"},
		{"infinity", math.Inf(1), "未知"},
		{"out of billing range", 1e100, "未知"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			group := RemoteGroup{ID: "8", Name: "特惠", RateMultiplier: &declared, ResolvedRateMultiplier: &tc.value}
			require.Equal(t, "特惠-"+tc.want+"-20260927", managedKeyName(group, created, "sub2api"))
		})
	}
	require.Equal(t, "特惠-未知-20260927", managedKeyName(RemoteGroup{ID: "8", Name: "特惠", RateMultiplier: &declared}, created, "newapi"))
}
