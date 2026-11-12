package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GroupIdentity is an optional presentation label. It does not change the
// group's name, provider platform, access policy or routing behavior.
type GroupIdentity struct {
	Label string `json:"label,omitempty"`
	Color string `json:"color,omitempty"`
}

func NormalizeGroupIdentity(input GroupIdentity) (GroupIdentity, error) {
	if !utf8.ValidString(input.Label) || !utf8.ValidString(input.Color) {
		return GroupIdentity{}, errors.New("group identity must contain valid Unicode text")
	}
	for _, text := range []string{input.Label, input.Color} {
		for _, character := range text {
			if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
				return GroupIdentity{}, errors.New("group identity must be a single line without control characters")
			}
		}
	}
	color := strings.TrimSpace(input.Color)
	switch color {
	case "", "teal", "blue", "amber", "violet", "rose", "gray":
	default:
		if len(color) != 7 || color[0] != '#' || strings.IndexFunc(color[1:], func(character rune) bool {
			return !('0' <= character && character <= '9' || 'a' <= character && character <= 'f' || 'A' <= character && character <= 'F')
		}) != -1 {
			return GroupIdentity{}, errors.New("group identity color must be a supported preset or a six-digit hex color")
		}
		color = strings.ToLower(color)
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return GroupIdentity{}, nil
	}
	if utf8.RuneCountInString(label) > 12 {
		return GroupIdentity{}, errors.New("group identity label cannot exceed 12 Unicode characters")
	}
	if color == "" {
		color = "teal"
	}
	return GroupIdentity{Label: label, Color: color}, nil
}
