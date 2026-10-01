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
	require.Contains(t, notice.Body, "上调0.03")
	require.Contains(t, notice.Body, "上调幅度为3.33%")
	require.NotContains(t, notice.Body, "Base")
	require.NotContains(t, notice.Body, "Resolved")
	require.NotContains(t, notice.Body, "{")
}
