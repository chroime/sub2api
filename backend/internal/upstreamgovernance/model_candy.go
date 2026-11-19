package upstreamgovernance

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

const modelCandyJudgeVersion = "candy-numeric-20261118-v3"
const modelCandyHistoryMaxBytes = 128 * 1024

var (
	modelCandyUnit            = `(?:颗糖果|个糖果|颗|个|粒|candies|candy|pieces?|items?)?`
	modelCandyNumber          = `(?:-?[0-9]+(?:\.[0-9]+)?|[零〇一二两三四五六七八九十百]+)`
	modelCandyValue           = regexp.MustCompile(`(?i)^\s*(` + modelCandyNumber + `)\s*` + modelCandyUnit + `\s*[。.!！:：]?\s*$`)
	modelCandyExplainedValue  = regexp.MustCompile(`(?i)^\s*(` + modelCandyNumber + `)\s*` + modelCandyUnit + `\s*[，,：:]\s*(?:具体|选取|摸取|取法|其中|即|前提是|因为|这里|理由|证明)`)
	modelCandyLeadingValue    = regexp.MustCompile(`(?i)^\s*(` + modelCandyNumber + `)\s*` + modelCandyUnit + `(?:\s|[,，。.!！;；:：)\]}]|$)`)
	modelCandyShapeNote       = regexp.MustCompile(`（\s*(?:圆形\s*\d+\s*[颗个]|\d+\s*[颗个]\s*圆形)\s*[，,＋+和、]\s*(?:五角星形?\s*\d+\s*[颗个]|\d+\s*[颗个]\s*五角星形?)\s*）`)
	modelCandyLabel           = regexp.MustCompile(`(?i)(?:(?:最终|最后)?(?:答案|结论|结果)\s*(?:(?:应为|应该是|应当是|是|为|就是)\s*)?[:：]?|(?:最少|至少)\s*(?:需要|需|要)?\s*(?:取出|摸出|抽取|取|摸)\s*(?:的)?\s*(?:糖果)?\s*(?:数目|数量|总数)?\s*(?:是|为)?\s*[:：]?|["']?(?:the\s+)?(?:(?:final|correct)\s+)?(?:answer|result|conclusion)["']?\s*(?:is\s*)?[:：=]?|["'](?:final[_\s]+)?(?:answer|result|conclusion|minimum|count|total|value)["']\s*[:=]|(?:the\s+)?minimum(?:\s+number)?(?:\s+of\s+(?:cand(?:y|ies)|pieces?|items?))?\s*(?:(?:is|equals?)\s*)?[:：=]?|(?:we\s+)?(?:need|must|should)\s+(?:to\s+)?(?:take|draw|pick)\s+(?:at\s+least\s+)?)\s*`)
	modelCandyPrefix          = regexp.MustCompile(`(?i)^(?:(?:因此|所以|综上所述|综上|由此可知|故|总之|therefore|thus|hence|overall|in\s+conclusion|so)[，,：:\s]*)+`)
	modelCandyMarkdown        = regexp.MustCompile(`^(?:#{1,6}\s+|[-*+]\s+)`)
	modelCandyText            = regexp.MustCompile(`\\(?:text|mathrm|textrm)\s*\{([^{}]*)\}`)
	modelCandyBox             = regexp.MustCompile(`\\(?:boxed|fbox)\s*\{([^{}]*)\}`)
	modelCandyHTML            = regexp.MustCompile(`<[^>]*>`)
	modelCandySentences       = regexp.MustCompile(`[。！!；;]`)
	modelCandyUncertain       = regexp.MustCompile(`可能|也许|大概|不确定|或许|或者|还是|[?？]|(?i:maybe|perhaps|either|could\s+be|might\s+be|may\s+be)`)
	modelCandyUncertainPrefix = regexp.MustCompile(`^(?:也?可能|也许|大概|不确定|或许|或者|还是|(?i:maybe|perhaps|either|could\s+be|might\s+be|may\s+be))`)
	modelCandyAlternatives    = regexp.MustCompile(modelCandyNumber + `\s*` + modelCandyUnit + `\s*(?:或(?:者)?|还是|和|、|/|至|到|[-~～]|[,，]\s*|or)\s*` + modelCandyNumber)
	modelCandyNonFinal        = regexp.MustCompile(`先假设|并非.*(?:答案|结论)|不能作为.*(?:答案|结论)|(?:只是|仅为).*示例|撤回.*答案`)
)

// Only formatting is normalized: formulas are never evaluated, and numbers
// mentioned in a proof are never treated as answers by themselves.
func normalizeModelCandyLine(line string) string {
	line = strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return '0' + r - '０'
		}
		return r
	}, line)
	for strings.HasPrefix(strings.TrimSpace(line), ">") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
	}
	line = strings.NewReplacer("**", "", "__", "", "`", "", `\[`, "", `\]`, "", `\(`, "", `\)`, "", "$", "", `\,`, " ", `\!`, "", `\;`, " ", `\quad`, " ", "|", " ").Replace(line)
	line = modelCandyText.ReplaceAllString(line, "$1")
	line = modelCandyHTML.ReplaceAllString(line, "")
	line = modelCandyShapeNote.ReplaceAllString(line, "")
	return strings.TrimSpace(modelCandyMarkdown.ReplaceAllString(strings.TrimSpace(line), ""))
}

func modelCandyInteger(raw string) (int, bool) {
	if n, err := strconv.Atoi(raw); err == nil {
		return n, n >= 0
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		if n >= 0 && n >= math.MinInt && n <= math.MaxInt && math.Trunc(n) == n {
			return int(n), true
		}
		return 0, false
	}
	// Accept conventional Chinese integers below 100, without guessing malformed
	// sequences such as 二一 or mixing Chinese and Arabic digits.
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	r := []rune(raw)
	if len(r) == 1 {
		n, ok := digits[r[0]]
		if r[0] == '十' {
			return 10, true
		}
		return n, ok
	}
	parts := strings.Split(raw, "十")
	if len(parts) != 2 {
		return 0, false
	}
	tens, ones := 1, 0
	if parts[0] != "" {
		v := []rune(parts[0])
		if len(v) != 1 {
			return 0, false
		}
		n, ok := digits[v[0]]
		if !ok || n == 0 {
			return 0, false
		}
		tens = n
	}
	if parts[1] != "" {
		v := []rune(parts[1])
		if len(v) != 1 {
			return 0, false
		}
		n, ok := digits[v[0]]
		if !ok || n == 0 {
			return 0, false
		}
		ones = n
	}
	return tens*10 + ones, true
}

func modelCandyIgnoredLine(line string) bool {
	for _, prefix := range []string{"如果", "若", "假如", "假设", "完全盲取时", "盲取时", "在盲取情况下", "在完全盲取情况下", "不能按形状选择时", "不区分形状时", "例如", "举例", "示例", "引用"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func modelCandyVerdict(text string) (*int, string) {
	values := map[int]bool{}
	conclusion, ambiguous, pending := false, false, false
	fence := ""
	inQuote := false
	ignoredBlock := false
	contentLines := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" && !strings.HasPrefix(line, "```") && !strings.HasPrefix(line, "~~~") && normalizeModelCandyLine(line) != "" {
			contentLines++
		}
	}
	add := func(value string, explicit bool) {
		value = strings.TrimSpace(modelCandyBox.ReplaceAllString(value, "$1"))
		match := modelCandyValue.FindStringSubmatch(value)
		if len(match) != 2 && explicit {
			match = modelCandyExplainedValue.FindStringSubmatch(value)
		}
		if len(match) != 2 && explicit {
			match = modelCandyLeadingValue.FindStringSubmatch(value)
		}
		if len(match) != 2 {
			if explicit {
				ambiguous = true
			}
			return
		}
		n, ok := modelCandyInteger(match[1])
		if !ok {
			ambiguous = true
			return
		}
		values[n] = true
		conclusion = true
	}
	for _, raw := range strings.Split(text, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			ignoredBlock = false
			inQuote = false
			continue
		}
		if strings.HasPrefix(raw, "```") || strings.HasPrefix(raw, "~~~") {
			marker := raw[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(raw, ">") {
			inQuote = true
		}
		if inQuote {
			pending = false
			continue
		}
		line := normalizeModelCandyLine(raw)
		if line == "" {
			continue
		}
		line = modelCandyPrefix.ReplaceAllString(line, "")
		if modelCandyNonFinal.MatchString(line) {
			ambiguous = true
		}
		if modelCandyIgnoredLine(line) {
			// A heading can scope the following block as a hypothetical or an
			// example. Without a clear boundary it cannot supply a final answer.
			if strings.HasPrefix(line, "引用") || strings.HasSuffix(line, ":") || strings.HasSuffix(line, "：") {
				ignoredBlock = true
			}
			pending = false
			continue
		}
		if ignoredBlock {
			continue
		}
		if modelCandyUncertain.MatchString(line) && (modelCandyLabel.MatchString(line) || modelCandyUncertainPrefix.MatchString(line)) {
			ambiguous = true
		}
		if modelCandyLabel.MatchString(line) && (modelCandyAlternatives.MatchString(line) || (strings.Contains(line, "前提是") && strings.Contains(line, "否则"))) {
			ambiguous = true
		}
		if pending {
			add(line, true)
			pending = false
			continue
		}
		for _, sentence := range modelCandySentences.Split(line, -1) {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" || modelCandyIgnoredLine(sentence) {
				continue
			}
			labels := modelCandyLabel.FindAllStringIndex(sentence, -1)
			for i, label := range labels {
				end := len(sentence)
				if i+1 < len(labels) {
					end = labels[i+1][0]
				}
				value := strings.TrimSpace(sentence[label[1]:end])
				if value == "" && len(labels) == 1 && label[0] == 0 {
					pending = true
					continue
				}
				if value == "" {
					continue
				}
				// Non-prefix labels still detect contradictions, but cannot turn
				// arbitrary narration into an automatic answer.
				add(value, true)
			}
			if len(labels) == 0 {
				boxed := strings.HasPrefix(sentence, `\boxed{`) || strings.HasPrefix(sentence, `\fbox{`)
				bare := modelCandyValue.MatchString(line) && (contentLines == 1 || fence != "")
				if boxed || bare {
					add(sentence, boxed || bare)
				}
			}
		}
	}
	if !conclusion || ambiguous || pending || len(values) != 1 {
		return nil, "needs_review"
	}
	for n := range values {
		if n == 21 {
			return &n, "numeric_correct"
		}
		return &n, "numeric_incorrect"
	}
	return nil, "needs_review"
}
