package upstreamgovernance

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPromptPreservesBenchmarks(t *testing.T) {
	p, err := buildModelPrompt(ModelRunRequest{Template: "candy"})
	require.NoError(t, err)
	require.Equal(t, ModelCandyPrompt, p.text)
	require.NotContains(t, p.text, "21")
	require.Contains(t, p.text, "五角星形 7 6 4")
	p, err = buildModelPrompt(ModelRunRequest{Template: "pelican"})
	require.NoError(t, err)
	require.Equal(t, "创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要有任何限制", p.text)
}

func TestModelPromptRandomMarkersAndEcho(t *testing.T) {
	r := ModelRunRequest{Template: "token_audit", Config: ModelTestConfig{Model: "gpt-4o", InputTokens: 1200}}
	a, err := buildModelPrompt(r)
	require.NoError(t, err)
	b, err := buildModelPrompt(r)
	require.NoError(t, err)
	require.NotEqual(t, a.text, b.text)
	require.Len(t, a.markers, 3)
	last := -1
	for _, m := range a.markers {
		i := strings.Index(a.text, m.Expected)
		require.Greater(t, i, last)
		require.Equal(t, 1, strings.Count(a.text, m.Expected), "answer must appear only in source material")
		last = i
	}
	require.NotEmpty(t, a.echo)
	require.Equal(t, 1, strings.Count(a.text, a.echo))
}

func TestModelPromptCandyDoesNotScoreMentionedNumbers(t *testing.T) {
	for _, s := range []string{"A 21-token example; final answer 29", "21 和 29 都有可能。", "取9圆12星共21，最少其实29颗。"} {
		answer, verdict := modelCandyVerdict(s)
		require.Nil(t, answer)
		require.Equal(t, "needs_review", verdict)
	}
	answer, verdict := modelCandyVerdict("21")
	require.Equal(t, 21, *answer)
	require.Equal(t, "numeric_correct", verdict)
	_, verdict = modelCandyVerdict("29颗")
	require.Equal(t, "numeric_incorrect", verdict)
}

func TestModelPromptCandyRecognizesUniqueStandaloneConclusion(t *testing.T) {
	for _, text := range []string{
		"答案是21颗。\n取9颗圆形和12颗五角星形。圆形西瓜只有8颗，所以必有苹果或桃子。",
		"**答案为 21 颗糖果。**\n下面说明这种取法为什么可行，并证明20颗不能保证。",
		"答案：**21**颗。",
		"最少需要取出21颗糖果。\n选择9颗圆形、12颗五角星形。",
		"答案是21颗。\n如果完全盲取，答案是29颗。这里可以按手感选择形状。",
		"答案是21颗。\n最少需要取出21颗糖果。",
	} {
		answer, verdict := modelCandyVerdict(text)
		require.NotNil(t, answer, text)
		require.Equal(t, 21, *answer, text)
		require.Equal(t, "numeric_correct", verdict, "proof still requires independent review: %s", text)
	}
	for _, text := range []string{
		"答案是21颗。\n最终答案是29颗。",
		"最终答案是29颗。\n答案是21颗。",
		"答案是21颗。\n经过修正，答案为29颗。",
		"答案是21颗。\n答案是21或29颗。",
		"答案是21颗。\n答案为21.5颗。",
		"如果完全盲取，答案是29颗。",
		"举例来说，21颗是一个需要考虑的数字。",
		"例如答案是21颗，但这只是示例。",
		"```text\n答案是21颗。\n```",
	} {
		answer, verdict := modelCandyVerdict(text)
		require.Nil(t, answer, text)
		require.Equal(t, "needs_review", verdict, text)
	}
	answer, verdict := modelCandyVerdict("答案为29颗。\n这是我的最终结论。")
	require.Equal(t, 29, *answer)
	require.Equal(t, "numeric_incorrect", verdict)
}

func TestModelPromptHTMLExtractionPreservesOriginalCode(t *testing.T) {
	html := "<!doctype html>\n<html><svg></svg></html>"
	require.Equal(t, html, extractModelHTML(html))
	require.Equal(t, html+"\n", extractModelHTML("Here is the result.\n```html\n"+html+"\n```"))
	require.Empty(t, extractModelHTML("```html\n<html>A</html>\n```\n```html\n<html>B</html>\n```"))
}
