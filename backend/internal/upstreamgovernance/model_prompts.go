package upstreamgovernance

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const ModelCandyPrompt = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）
苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4`

const ModelPelicanPrompt = `创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要有任何限制`

type modelPrompt struct {
	text    string
	markers []ModelMarkerResult
	echo    string
}

func modelTemplateVersion(template string) string {
	switch template {
	case "candy", "pelican", "probe", "token_audit", "context":
		return template + "-20260927-v1"
	}
	return ""
}

func buildModelPrompt(req ModelRunRequest) (modelPrompt, error) {
	switch req.Template {
	case "candy":
		return modelPrompt{text: ModelCandyPrompt}, nil
	case "pelican":
		return modelPrompt{text: ModelPelicanPrompt}, nil
	case "probe":
		value, err := modelRandomHex(12)
		if err != nil {
			return modelPrompt{}, err
		}
		expected := "GOV_PROBE_" + value
		return modelPrompt{text: "Reply with exactly this verification token: " + expected, markers: []ModelMarkerResult{{Position: "probe", Label: "PROBE", Expected: expected}}}, nil
	case "token_audit", "context":
		return buildModelContextPrompt(req.Config, req.Template == "token_audit")
	default:
		return modelPrompt{}, ErrInvalid
	}
}

func modelRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func buildModelContextPrompt(cfg ModelTestConfig, echo bool) (modelPrompt, error) {
	requested := cfg.InputTokens
	if requested == 0 {
		requested = 1024
	}
	if requested < 1 || requested > 500000 {
		return modelPrompt{}, ErrInvalid
	}
	p := modelPrompt{}
	for _, label := range []string{"HEAD", "MIDDLE", "TAIL"} {
		value, err := modelRandomHex(12)
		if err != nil {
			return p, err
		}
		p.markers = append(p.markers, ModelMarkerResult{Position: strings.ToLower(label), Label: label, Expected: value})
	}
	if echo {
		value, err := modelRandomHex(48)
		if err != nil {
			return p, err
		}
		p.echo = "BEGIN_ECHO_" + value + "_END_ECHO"
	}
	// The requested size is a construction target, not a claim about the upstream
	// tokenizer. The audit below records the actual local count and exact bytes.
	const unit = "The archive contains ordinary reference material about rivers, mountains, seasons, languages, libraries, clocks, and bicycles. 本段只是背景资料，不包含需要回忆的标记。\n"
	unitTokens := 48
	if codec, _, _ := modelTokenizer(cfg); codec != nil {
		if n, err := codec.count(unit); err == nil && n > 0 {
			unitTokens = n
		}
	}
	units := (requested - 240) / unitTokens
	if units < 0 {
		units = 0
	}
	var b strings.Builder
	b.WriteString("Read the source document. Return the exact values of its HEAD, MIDDLE and TAIL anchors as JSON with those three keys. Do not replace values with labels or guesses.")
	if echo {
		b.WriteString(" Also return the entire ECHO record, including its BEGIN_ECHO_ and _END_ECHO delimiters, under the JSON key ECHO.")
	}
	b.WriteString("\n<source_document>\n")
	b.WriteString("HEAD=" + p.markers[0].Expected + "\n")
	b.WriteString(strings.Repeat(unit, units/2))
	b.WriteString("MIDDLE=" + p.markers[1].Expected + "\n")
	b.WriteString(strings.Repeat(unit, units-units/2))
	b.WriteString("TAIL=" + p.markers[2].Expected + "\n")
	if echo {
		b.WriteString("ECHO=" + p.echo + "\n")
	}
	b.WriteString("</source_document>\nReturn the requested anchor values from the document.")
	p.text = b.String()
	return p, nil
}

// Only an unambiguous whole HTML answer or a single HTML code block becomes an
// artifact. Original response text is always retained separately and unmodified.
func extractModelHTML(text string) string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if (strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")) && strings.HasSuffix(lower, "</html>") {
		return text
	}
	var found []string
	lines := strings.SplitAfter(text, "\n")
	for i := 0; i < len(lines); i++ {
		fence := strings.ToLower(strings.TrimSpace(lines[i]))
		if fence != "```html" && fence != "```" {
			continue
		}
		start := i + 1
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "```"; i++ {
		}
		if i == len(lines) {
			break
		}
		code := strings.Join(lines[start:i], "")
		codeLower := strings.ToLower(strings.TrimSpace(code))
		if strings.HasPrefix(codeLower, "<!doctype html") || strings.HasPrefix(codeLower, "<html") {
			found = append(found, code)
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	return ""
}

func modelPromptNote(p modelPrompt) string {
	if len(p.markers) == 0 {
		return ""
	}
	return fmt.Sprintf("%d randomized recall markers; missing markers are recall evidence, not proof of input truncation or billing fraud.", len(p.markers))
}
