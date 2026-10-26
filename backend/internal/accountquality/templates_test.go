package accountquality

import "testing"

func TestCandyJudgeAcceptsAnExplicitCorrectAnswerBeforeExplanation(t *testing.T) {
	cases := []string{
		"答案是21。因为可以先按形状筛选。",
		"答案为：21颗糖果。\n下面是推导过程，但不影响数值结论。",
		"最终答案就是 21 个。\n解释：先保证圆形和五角星的颜色组合。",
		"21",
		"```text\n答案：21\n```",
	}
	for _, answer := range cases {
		verdict, reason := Judge("candy", "21", answer)
		if verdict != "pass" {
			t.Errorf("answer %q got verdict=%q reason=%q, want pass", answer, verdict, reason)
		}
	}
}

func TestCandyJudgeKeepsAmbiguousOrUnclearAnswersForReview(t *testing.T) {
	cases := []string{
		"答案是21或29颗。",
		"我看到了数字21，但没有给出最终答案。",
		"如果完全盲取，答案是29颗。",
		"答案是29颗。",
	}
	for _, answer := range cases {
		verdict, _ := Judge("candy", "21", answer)
		if answer == "答案是29颗。" {
			if verdict != "fail" {
				t.Errorf("answer %q got verdict=%q, want fail", answer, verdict)
			}
			continue
		}
		if verdict != "needs_review" {
			t.Errorf("answer %q got verdict=%q, want needs_review", answer, verdict)
		}
	}
}
