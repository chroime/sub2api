package upstreamgovernance

import "strings"

// summarizeModelSecurity turns raw model-run observations into a small,
// safe-to-display trust assessment. It is intentionally conservative: a
// missing tokenizer or usage field is unverified, and a token discrepancy or
// malformed usage is suspicious, but neither is labelled as proven fraud.
func summarizeModelSecurity(template string, result ModelRunResult) ModelSecuritySummary {
	summary := ModelSecuritySummary{
		TrustState:         "unverified",
		TrustLabel:         "无法核验",
		TokenInputState:    result.Tokens.InputState,
		TokenOutputState:   result.Tokens.OutputState,
		InputDeltaPercent:  result.Tokens.InputDeltaPercent,
		OutputDeltaPercent: result.Tokens.OutputDeltaPercent,
		CandyVerdict:       result.CandyVerdict,
		CandyVerdictLabel:  modelCandyVerdictLabel(result.CandyVerdict),
	}

	summary.TokenDifference = modelTokenDifference(result.Tokens.InputState) || modelTokenDifference(result.Tokens.OutputState)
	if template == "pelican" && strings.TrimSpace(result.HTML) != "" {
		summary.PelicanHTMLAvailable = true
		summary.PelicanHTMLBytes = len(result.HTML)
	}

	switch {
	case summary.TokenDifference:
		summary.TrustState = "suspicious"
		summary.TrustLabel = "需要复核"
		summary.Summary = "检测到输入或输出令牌数量存在差异，请结合上游声明和本地计数复核。"
	case result.Tokens.MarkersPassed != nil && !*result.Tokens.MarkersPassed:
		summary.TrustState = "needs_review"
		summary.TrustLabel = "需要复核"
		summary.Summary = "上下文标记未全部找回，无法确认是截断、模型理解偏差还是计数差异。"
	case result.CandyVerdict == "numeric_incorrect":
		summary.TrustState = "quality_failed"
		summary.TrustLabel = "答案错误"
		summary.Summary = "糖果逻辑题答案未通过标准答案校验。"
	case result.CandyVerdict == "needs_review":
		summary.TrustState = "needs_review"
		summary.TrustLabel = "需要复核"
		summary.Summary = "糖果逻辑题出现多个结论或无法提取唯一答案，需要人工复核。"
	case result.CandyVerdict == "numeric_correct":
		summary.TrustState = "trusted"
		summary.TrustLabel = "答案正确"
		summary.Summary = "糖果逻辑题答案通过标准答案校验；证明过程仍可人工复核。"
	case modelTokenStatesUnverified(result.Tokens):
		summary.Summary = "当前结果缺少可比的令牌计数，暂时不能完成完整可信度核验。"
	case template == "pelican" && summary.PelicanHTMLAvailable:
		summary.Summary = "已提取鹈鹕 SVG 页面结果，可在安全隔离的预览窗口中查看。"
	default:
		summary.Summary = "未发现明确异常，但当前结果仍需结合完整测试记录判断。"
	}
	return summary
}

func modelTokenDifference(state string) bool {
	switch state {
	case "suspected_difference", "invalid_usage":
		return true
	default:
		return false
	}
}

func modelTokenStatesUnverified(a ModelTokenAudit) bool {
	if a.InputState == "" && a.OutputState == "" {
		return true
	}
	for _, state := range []string{a.InputState, a.OutputState} {
		switch state {
		case "tokenizer_unknown", "usage_missing", "cache_incomparable", "incomparable_reasoning":
			return true
		}
	}
	return false
}

func modelCandyVerdictLabel(verdict string) string {
	switch verdict {
	case "numeric_correct":
		return "答案正确"
	case "numeric_incorrect":
		return "答案错误"
	case "needs_review":
		return "需要复核"
	default:
		return ""
	}
}
