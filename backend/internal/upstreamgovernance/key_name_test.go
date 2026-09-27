package upstreamgovernance

import (
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
	require.Equal(t, "codex特惠-20260927", managedKeyName(RemoteGroup{ID: "8", Name: "codex特惠"}, created, "sub2api"))
	require.Equal(t, "8-20260927", managedKeyName(RemoteGroup{ID: "8", Name: "  "}, created, "sub2api"))
	for _, platform := range []string{"sub2api", "newapi"} {
		name := managedKeyName(RemoteGroup{ID: "8", Name: strings.Repeat("中文分组😀", 50)}, created, platform)
		require.True(t, utf8.ValidString(name))
		require.True(t, strings.HasSuffix(name, "-20260927"))
		require.LessOrEqual(t, len(name), 100)
		if platform == "newapi" {
			require.LessOrEqual(t, utf8.RuneCountInString(name), 30)
		}
	}
}
