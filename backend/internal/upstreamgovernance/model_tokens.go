package upstreamgovernance

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tiktoken-go/tokenizer"
)

type modelCodec struct {
	mu    sync.Mutex
	codec tokenizer.Codec
}

func (c *modelCodec) count(text string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.codec.Count(text)
}

var modelCodecs struct {
	sync.Mutex
	values map[string]*modelCodec
}

var modelDateSuffix = regexp.MustCompile(`-(?:\d{4}-\d{2}-\d{2}|\d{4})$`)

func modelTokenizer(cfg ModelTestConfig) (*modelCodec, string, string) {
	name, source := cfg.Tokenizer, "user_selected"
	if name == "" || name == "auto" {
		source = "model_mapping"
		model := modelDateSuffix.ReplaceAllString(strings.ToLower(cfg.Model), "")
		switch model {
		case "gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "o1", "o1-mini", "o1-preview", "o3", "o3-mini", "o4-mini", "gpt-5", "gpt-5-mini", "gpt-5-nano":
			name = "o200k_base"
		case "gpt-4", "gpt-4-turbo", "gpt-4-turbo-preview", "gpt-4-32k", "gpt-3.5-turbo", "gpt-3.5-turbo-16k":
			name = "cl100k_base"
		default:
			return nil, "", "unavailable"
		}
	}
	if name != "cl100k_base" && name != "o200k_base" {
		return nil, name, "unavailable"
	}
	modelCodecs.Lock()
	defer modelCodecs.Unlock()
	if modelCodecs.values == nil {
		modelCodecs.values = make(map[string]*modelCodec)
	}
	if c := modelCodecs.values[name]; c != nil {
		return c, name, source
	}
	c, err := tokenizer.Get(tokenizer.Encoding(name))
	if err != nil {
		return nil, name, "unavailable"
	}
	value := &modelCodec{codec: c}
	modelCodecs.values[name] = value
	return value, name, source
}

func modelTextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func modelCountPointer(codec *modelCodec, text string) *int64 {
	n, err := codec.count(text)
	if err != nil {
		return nil
	}
	value := int64(n)
	return &value
}

func auditModelTokens(cfg ModelTestConfig, mode string, prompt modelPrompt, text string, usage ModelUsage, reasoningPossible bool) ModelTokenAudit {
	a := ModelTokenAudit{InputHash: modelTextHash(prompt.text), OutputHash: modelTextHash(text), InputBytes: len(prompt.text), OutputBytes: len(text), InputChars: utf8.RuneCountInString(prompt.text), OutputChars: utf8.RuneCountInString(text), InputRequestedTokens: cfg.InputTokens, InputOverheadMargin: 32, Note: "Local counts cover visible text only. Input differences use the larger of 32 tokens or configured percentage as a heuristic overhead margin, not a proven bound. Upstream-added content and tokenizer identity remain unknown; differences are signals for review, not a fraud verdict."}
	codec, name, source := modelTokenizer(cfg)
	a.Tokenizer, a.Source = name, source
	if codec != nil {
		a.TokenizerVersion = "tiktoken-go/tokenizer v0.8.0"
		a.InputLocal, a.OutputLocal = modelCountPointer(codec, prompt.text), modelCountPointer(codec, text)
	}
	tolerance := cfg.TokenTolerancePercent
	if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 {
		tolerance = 10
	}
	a.InputState = "tokenizer_unknown"
	a.OutputState = "tokenizer_unknown"
	if a.InputLocal != nil {
		a.InputState = "usage_missing"
		if usage.InputTokens != nil {
			declared := *usage.InputTokens
			invalid := declared < 0
			cacheUnknown := false
			if mode == "anthropic" {
				cacheUnknown = usage.CachedTokens == nil || usage.CacheCreationTokens == nil
				for _, cached := range []*int64{usage.CachedTokens, usage.CacheCreationTokens} {
					if cached != nil {
						if *cached < 0 || declared > math.MaxInt64-*cached {
							invalid = true
						} else {
							declared += *cached
						}
					}
				}
			}
			if invalid {
				a.InputState = "invalid_usage"
			} else if cacheUnknown {
				a.InputState = "cache_incomparable"
			} else {
				a.InputDeclaredTotal = &declared
				a.InputDeltaPercent = modelTokenDelta(*a.InputLocal, declared)
				a.InputState = "estimate_only"
				// Message wrapping can add tokens; never treat an excess as confirmed
				// tampering. A configurable tolerance flags discrepancies for review.
				margin := math.Max(float64(a.InputOverheadMargin), float64(*a.InputLocal)*tolerance/100)
				if math.Abs(float64(declared)-float64(*a.InputLocal)) > margin {
					a.InputState = "suspected_difference"
				}
			}
		}
	}
	if a.OutputLocal != nil {
		a.OutputState = "usage_missing"
		if usage.OutputTokens != nil {
			visible := *usage.OutputTokens
			invalid := visible < 0
			if usage.ReasoningTokens != nil && *usage.ReasoningTokens < 0 {
				invalid = true
			}
			if (mode == "chat_completions" || mode == "responses") && usage.ReasoningTokens != nil {
				visible -= *usage.ReasoningTokens
				invalid = invalid || visible < 0
			}
			switch {
			case invalid:
				a.OutputState = "invalid_usage"
			case reasoningPossible && usage.ReasoningTokens == nil && mode != "gemini":
				a.OutputState = "incomparable_reasoning"
			case mode == "anthropic" && reasoningPossible:
				// Anthropic output usage can include thinking but exposes no reliable
				// disjoint visible-output count. Keep it incomparable even if gateways
				// invent a reasoning_tokens extension.
				a.OutputState = "incomparable_reasoning"
			default:
				a.OutputDeclaredVisible = &visible
				a.OutputDeltaPercent = modelTokenDelta(*a.OutputLocal, visible)
				a.OutputState = "within_tolerance"
				if (*a.OutputLocal == 0 && visible != 0) || (a.OutputDeltaPercent != nil && math.Abs(*a.OutputDeltaPercent) > tolerance) {
					a.OutputState = "suspected_difference"
				}
			}
		}
	}
	// Malformed usage remains an explicit upstream data issue even when the local
	// tokenizer is unavailable. A missing field stays nil and is never zero-filled.
	for _, v := range []*int64{usage.InputTokens, usage.CachedTokens, usage.CacheCreationTokens} {
		if v != nil && *v < 0 {
			a.InputState = "invalid_usage"
		}
	}
	for _, v := range []*int64{usage.OutputTokens, usage.ReasoningTokens} {
		if v != nil && *v < 0 {
			a.OutputState = "invalid_usage"
		}
	}
	if len(prompt.markers) != 0 {
		passed := true
		for _, m := range prompt.markers {
			m.Found = strings.Contains(text, m.Expected)
			passed = passed && m.Found
			a.Markers = append(a.Markers, m)
		}
		a.MarkersPassed = &passed
		a.Note += " " + modelPromptNote(prompt)
	}
	if prompt.echo != "" {
		pass := strings.Contains(text, prompt.echo)
		a.EchoExpected = prompt.echo
		a.EchoPassed = &pass
		a.Note += " A missing or changed echo can reflect output truncation or instruction-following failure; it does not prove altered usage."
	}
	return a
}

func modelTokenDelta(local, declared int64) *float64 {
	if local == 0 {
		if declared == 0 {
			zero := float64(0)
			return &zero
		}
		return nil
	}
	delta := 100 * (float64(declared) - float64(local)) / float64(local)
	return &delta
}
