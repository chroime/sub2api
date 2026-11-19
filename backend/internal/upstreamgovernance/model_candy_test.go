package upstreamgovernance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCandyCompatibleConclusions(t *testing.T) {
	for _, text := range []string{
		"因此，最少需要取出 **21 颗**。",
		"所以最少需要取出 **21颗**。",
		"综上所述，答案为：２１颗。",
		"### 最终答案\n\\[\n\\boxed{21\\text{ 颗}}\n\\]",
		"所以最少需要取出\n\n\\[\n\\boxed{21\\text{ 个}}\n\\]",
		"答案是 \\(21\\) 颗。下面给出证明。",
		"**答案：** $21$ 颗。",
		"\\boxed{21}",
		"Final answer: 21 candies.",
		"答案：二十一颗。",
		"因此最少取出21颗。\n如果完全盲取，答案为29颗。",
		"答案是21颗。\n> 引用示例：答案为29颗。",
		"最少需要取出 **21个糖果**，具体方法是：摸取9个圆形糖果和12个五角星形糖果。",
		"最少取出21个：选取9个圆形糖果和12个五角星形糖果。",
		"因此，答案为\n\\[\n\\boxed{21\\text{ 个（圆形 9 个，五角星形 12 个）}}。\n\\]",
		"最少需要取出21颗。\n9+12=\\boxed{21\\text{颗}}。\n故20颗仍有失败的可能，最少是21颗。",
	} {
		t.Run(text, func(t *testing.T) {
			answer, verdict := modelCandyVerdict(text)
			require.Equal(t, "numeric_correct", verdict)
			require.NotNil(t, answer)
			require.Equal(t, 21, *answer)
		})
	}
}

func TestModelCandyCompatibleIncorrectConclusions(t *testing.T) {
	for _, text := range []string{"因此，答案是29颗。", `\boxed{20}`, "Final answer: 29 candies.", "答案：二十九颗。"} {
		answer, verdict := modelCandyVerdict(text)
		require.Equal(t, "numeric_incorrect", verdict, text)
		require.NotNil(t, answer, text)
	}
}

func TestModelCandyAcceptsCommonFormattedConclusions(t *testing.T) {
	for _, text := range []string{
		"Answer: 21 candies, because nine round and twelve star-shaped candies suffice.",
		"The minimum number of candies is 21.",
		"| 最终答案 | **21** 颗 |",
		"```json\n{\"answer\": 21}\n```",
		"```text\n最终答案：21颗。\n```",
		"**21**",
	} {
		t.Run(text, func(t *testing.T) {
			answer, verdict := modelCandyVerdict(text)
			require.Equal(t, "numeric_correct", verdict)
			require.NotNil(t, answer)
			require.Equal(t, 21, *answer)
		})
	}
}

func TestModelCandyAcceptsExplicitConclusionsAfterIntroductoryText(t *testing.T) {
	for _, text := range []string{
		"我认为最终答案应该是21颗。",
		"After checking the cases, I think the answer is 21 candies.",
	} {
		answer, verdict := modelCandyVerdict(text)
		require.Equal(t, "numeric_correct", verdict, text)
		require.NotNil(t, answer, text)
		require.Equal(t, 21, *answer, text)
	}
}

func TestModelCandyCompatibilityRetainsAmbiguity(t *testing.T) {
	for _, text := range []string{
		"因此，答案是21或29颗。",
		"答案：21.5颗。",
		"答案：21/2颗。",
		"答案：-21颗。",
		"答案：21e2颗。",
		"答案是21颗。\n因此最终答案为29颗。",
		"答案是21颗。\n\\boxed{29}",
		"答案可能是21颗。",
		"如果完全盲取，答案是21颗。",
		"引用内容：答案是21颗。",
		"例如答案是21颗，但这只是示例。",
		"因为21颗够用，因此答案还需要考虑。",
		"答案是21颗。\n答案为21.5颗。",
		"答案是21颗。\n也可能是29颗。",
		"如果完全盲取：\n答案是21颗。",
		"示例：\n\\[\n\\boxed{21}\n\\]",
		"答案是21颗。\n可能是29颗。",
		"答案是21颗。\n也许是29颗。",
		"先假设\n\\boxed{21}\n但这并非最终答案。",
		"21。这个数只是示例，并非结论。",
		"21。该数字不能作为最终答案。",
		"> 引用开始\n答案是21颗。",
		"答案是21颗。\n\\boxed{21.5}",
		"答案：21个（圆形9个，五角星12个，或者29个）。",
		"答案：21，29颗。",
		"答案是21颗，具体数值在21到29之间。",
		"答案是21颗，前提是可以看到糖果颜色；否则应当取29颗。",
		"示例：\n最终答案是21颗。",
		"如果完全盲取：\n因此，答案是21颗。",
		"答案是21颗。\n或者29颗。",
	} {
		answer, verdict := modelCandyVerdict(text)
		require.Nil(t, answer, text)
		require.Equal(t, "needs_review", verdict, text)
	}
}
