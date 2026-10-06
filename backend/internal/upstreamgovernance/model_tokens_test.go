package upstreamgovernance

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func modelInt64(v int64) *int64 { return &v }

func TestModelTokenMultilingualAndUnknown(t *testing.T) {
	text := "你好，世界 🌍 café café こんにちは"
	cfg := ModelTestConfig{Model: "gpt-4o", TokenTolerancePercent: 15}
	a := auditModelTokens(cfg, "chat_completions", modelPrompt{text: text}, text, ModelUsage{}, false)
	require.NotNil(t, a.InputLocal)
	require.Greater(t, *a.InputLocal, int64(0))
	require.Equal(t, len(text), a.InputBytes)
	require.Equal(t, utf8.RuneCountInString(text), a.InputChars)
	require.Equal(t, "usage_missing", a.InputState)
	require.Equal(t, "usage_missing", a.OutputState)
	require.Equal(t, a.InputHash, a.OutputHash)
	cfg.Model = "custom-aliased-model"
	a = auditModelTokens(cfg, "chat_completions", modelPrompt{text: text}, text, ModelUsage{InputTokens: modelInt64(10)}, false)
	require.Nil(t, a.InputLocal)
	require.Equal(t, "tokenizer_unknown", a.InputState)
	cfg.Tokenizer = "cl100k_base"
	a = auditModelTokens(cfg, "chat_completions", modelPrompt{text: text}, text, ModelUsage{}, false)
	require.Equal(t, "user_selected", a.Source)
	require.NotNil(t, a.InputLocal)
}

func TestModelTokenSuspiciousUsageAndReasoning(t *testing.T) {
	cfg := ModelTestConfig{Model: "gpt-4o", TokenTolerancePercent: 10}
	p := modelPrompt{text: strings.Repeat("hello world ", 100)}
	base := auditModelTokens(cfg, "responses", p, "hello world hello world", ModelUsage{}, false)
	usage := ModelUsage{InputTokens: modelInt64(1), OutputTokens: modelInt64(*base.OutputLocal + 100), ReasoningTokens: modelInt64(100)}
	a := auditModelTokens(cfg, "responses", p, "hello world hello world", usage, false)
	require.Equal(t, "suspected_difference", a.InputState)
	require.Equal(t, "within_tolerance", a.OutputState)
	require.Equal(t, base.OutputLocal, a.OutputDeclaredVisible)
	usage.ReasoningTokens = modelInt64(0)
	a = auditModelTokens(cfg, "responses", p, "hello world hello world", usage, false)
	require.Equal(t, "suspected_difference", a.OutputState)
	usage.ReasoningTokens = nil
	a = auditModelTokens(cfg, "responses", p, "hello world hello world", usage, true)
	require.Equal(t, "incomparable_reasoning", a.OutputState)
	usage.ReasoningTokens = modelInt64(9999)
	a = auditModelTokens(cfg, "responses", p, "hello world hello world", usage, true)
	require.Equal(t, "invalid_usage", a.OutputState)
}

func TestModelTokenFullMarkersAndEchoAreSeparateFromCounts(t *testing.T) {
	p, err := buildModelPrompt(ModelRunRequest{Template: "token_audit", Config: ModelTestConfig{Model: "gpt-5", InputTokens: 1024}})
	require.NoError(t, err)
	answer := p.markers[0].Expected + " " + p.markers[1].Expected + " " + p.markers[2].Expected + " " + p.echo
	a := auditModelTokens(ModelTestConfig{Model: "gpt-5"}, "responses", p, answer, ModelUsage{}, true)
	require.True(t, *a.MarkersPassed)
	require.True(t, *a.EchoPassed)
	require.Equal(t, "usage_missing", a.InputState)
	require.Equal(t, "usage_missing", a.OutputState)
}

func TestModelTokenInputOverheadCacheAndEmptyOutput(t *testing.T) {
	cfg := ModelTestConfig{Model: "gpt-5", TokenTolerancePercent: 10}
	p := modelPrompt{text: "hello"}
	a := auditModelTokens(cfg, "responses", p, "", ModelUsage{InputTokens: modelInt64(12), OutputTokens: modelInt64(7), ReasoningTokens: modelInt64(0)}, false)
	require.Equal(t, "estimate_only", a.InputState)
	require.Equal(t, "suspected_difference", a.OutputState)
	require.Nil(t, a.OutputDeltaPercent)
	p.text = strings.Repeat("hello world ", 200)
	base := auditModelTokens(cfg, "anthropic", p, "", ModelUsage{}, true)
	usage := ModelUsage{InputTokens: modelInt64(1), CachedTokens: modelInt64(*base.InputLocal - 1), CacheCreationTokens: modelInt64(0)}
	a = auditModelTokens(cfg, "anthropic", p, "", usage, true)
	require.Equal(t, "estimate_only", a.InputState)
	require.Equal(t, base.InputLocal, a.InputDeclaredTotal)
	usage.CacheCreationTokens = nil
	a = auditModelTokens(cfg, "anthropic", p, "", usage, true)
	require.Equal(t, "cache_incomparable", a.InputState)
	a = auditModelTokens(cfg, "gemini", p, "", usage, true)
	require.Equal(t, int64(1), *a.InputDeclaredTotal, "Gemini prompt count already includes cache")
	for _, model := range []string{"gpt-6-astra", "gpt-4o-custom-proxy-alias", "gpt-4-turbo-excel"} {
		cfg.Model = model
		codec, _, _ := modelTokenizer(cfg)
		require.Nil(t, codec)
	}
}

func TestModelTokenContextMissingMarkersIsEvidenceNotFraud(t *testing.T) {
	p, err := buildModelPrompt(ModelRunRequest{Template: "context", Config: ModelTestConfig{Model: "gpt-4o", InputTokens: 600}})
	require.NoError(t, err)
	a := auditModelTokens(ModelTestConfig{Model: "gpt-4o"}, "chat_completions", p, p.markers[0].Expected, ModelUsage{}, false)
	require.NotNil(t, a.MarkersPassed)
	require.False(t, *a.MarkersPassed)
	require.True(t, a.Markers[0].Found)
	require.False(t, a.Markers[2].Found)
	require.Contains(t, a.Note, "not proof")
}
