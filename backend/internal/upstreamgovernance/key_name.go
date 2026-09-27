package upstreamgovernance

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Freeze this display name before any creation request. The governance marker
// remains the independent ownership and idempotency identifier.
func managedKeyName(group RemoteGroup, createdAt time.Time, platform string) string {
	source := group.Name
	if strings.TrimSpace(source) == "" {
		source = group.ID
	}
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, source))
	suffix := "-" + createdAt.In(time.Local).Format("20060102")
	// Native Sub2API has a 100-byte name validator. Keep a conservative
	// 30-character budget for compatibility with older New API token forms.
	maxRunes := 100
	if platform == "newapi" {
		maxRunes = 30
	}
	for len(name)+len(suffix) > 100 || utf8.RuneCountInString(name)+len(suffix) > maxRunes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return strings.TrimSpace(name) + suffix
}
