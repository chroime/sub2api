package upstreamgovernance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogRateNoticeIsChineseAndIncludesSiteURLGroupAndPercentage(t *testing.T) {
	before := `{"Base":0.9,"User":null,"Resolved":0.9,"Peak":null,"Enabled":false,"Start":"","End":""}`
	after := `{"Base":0.93,"User":null,"Resolved":0.93,"Peak":null,"Enabled":false,"Start":"","End":""}`
	notice := renderCatalogChangeNotice(Site{Name: "XenoAI", BaseURL: "https://xenoai.eu.cc"}, Event{Kind: "rate_changed", Resource: "claude-max", Before: before, After: after}, Catalog{Groups: []RemoteGroup{{ID: "claude-max", Name: "Claude Max"}}})
	require.Contains(t, notice.Subject, "XenoAI")
	require.Contains(t, notice.Body, "https://xenoai.eu.cc")
	require.Contains(t, notice.Body, "Claude Max")
	require.Contains(t, notice.Body, "0.9 -> 0.93")
	require.Contains(t, notice.Body, "倍率：0.9 -> 0.93，倍率上调，上调0.03，上调幅度为3.33%。")
	require.NotContains(t, notice.Body, "0.030000000000000027")
	require.Contains(t, notice.Body, "上调幅度为3.33%")
	require.NotContains(t, notice.Body, "Base")
	require.NotContains(t, notice.Body, "Resolved")
	require.NotContains(t, notice.Body, "{")
}

func TestCatalogRateNoticeDoesNotRenderInfinitePercentageForZeroBaseline(t *testing.T) {
	before := `{"Base":0,"User":null,"Resolved":0,"Peak":null,"Enabled":false,"Start":"","End":""}`
	after := `{"Base":0.2,"User":null,"Resolved":0.2,"Peak":null,"Enabled":false,"Start":"","End":""}`
	notice := renderCatalogChangeNotice(Site{Name: "ZeroBase", BaseURL: "https://zero.example"}, Event{Kind: "rate_changed", Resource: "zero", Before: before, After: after}, Catalog{Groups: []RemoteGroup{{ID: "zero", Name: "Zero Group"}}})

	require.Contains(t, notice.Body, "分组名称：Zero Group")
	require.Contains(t, notice.Body, "倍率：0 -> 0.2")
	require.Contains(t, notice.Body, "上调幅度为无法计算")
	require.NotContains(t, notice.Body, "Inf")
}

func TestCatalogRemovedNoticeUsesPreviousGroupName(t *testing.T) {
	notice := renderCatalogChangeNotice(
		Site{Name: "示例上游", BaseURL: "https://up.example"},
		Event{Kind: "group_removed", Resource: "claude-max", Before: `{"ID":"claude-max","Name":"Claude Max"}`},
		Catalog{GroupsComplete: true, Groups: []RemoteGroup{}},
	)

	require.Contains(t, notice.Body, "分组名称：Claude Max")
	require.NotContains(t, notice.Body, "claude-max")
}
