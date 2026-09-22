//go:build unit

package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDiffSettingsIncludesDocumentationKeys(t *testing.T) {
	before := &service.SystemSettings{DocsTitle: "Old title", DocsContent: "old"}
	after := &service.SystemSettings{DocsTitle: "New title", DocsContent: "new"}

	require.ElementsMatch(t, []string{"docs_title", "docs_content"}, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}))
}
