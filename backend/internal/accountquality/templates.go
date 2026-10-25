package accountquality

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func Templates() []Template {
	return []Template{{"logic", "确定性逻辑", "logic-20261025-v1", "计算随机数列的加权校验值，严格按 JSON 返回。"}, {"constraint", "精确指令", "constraint-20261025-v1", "只返回本次生成的唯一校验标记。"}, {"context", "上下文检索", "context-20261025-v1", "从独立文档中提取三个随机锚点。"}, {"candy", "糖果题（数值结论）", "candy-20261025-v1", "仅校验明确数值结论；证明完整性保留人工复核。"}}
}
func TemplateVersion(id string) string {
	for _, t := range Templates() {
		if t.ID == id {
			return t.Version
		}
	}
	return ""
}
func nonce() string {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("quality nonce entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func BuildPrompt(id string) (string, string, error) {
	switch id {
	case "constraint":
		expected := "QUALITY_" + nonce()
		return "请仅返回以下校验标记，不要引号、解释、Markdown 或其他字符：\n" + expected, expected, nil
	case "logic":
		var b [3]byte
		if _, e := rand.Read(b[:]); e != nil {
			return "", "", e
		}
		a, c, d := int(b[0])+11, int(b[1])+7, int(b[2])+3
		expected := fmt.Sprintf(`{"answer":%d}`, a+2*c+3*d)
		return fmt.Sprintf("请计算整数校验值 a+2*b+3*c，其中 a=%d，b=%d，c=%d。仅返回 JSON 对象 {\"answer\":整数}，不要其他字段或说明。", a, c, d), expected, nil
	case "context":
		values := map[string]string{"HEAD": nonce(), "MIDDLE": nonce(), "TAIL": nonce()}
		raw, _ := json.Marshal(values)
		filler := strings.Repeat("普通参考记录：河流、书籍、时钟、山脉。本段不含需要返回的锚点。\n", 20)
		return "读取文档并仅返回包含 HEAD、MIDDLE、TAIL 三个原值的 JSON 对象，不要解释。\n<document>\nHEAD=" + values["HEAD"] + "\n" + filler + "MIDDLE=" + values["MIDDLE"] + "\n" + filler + "TAIL=" + values["TAIL"] + "\n</document>", string(raw), nil
	case "candy":
		return "袋中有苹果味、桃子味和西瓜味糖果。圆形数量依次为7、9、8，五角星形数量依次为7、6、4。可凭手感分辨形状。活动前决定摸出的最少数量，保证手中有圆形苹果味和五角星桃子味，或者圆形桃子味和五角星苹果味。请给出最少数量，最后单独一行写：答案：N。", "21", nil
	}
	return "", "", ErrInvalid
}
func Judge(template, expected, text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "needs_review", "回答为空，无法判定"
	}
	if template == "constraint" {
		if text == expected {
			return "pass", "返回了本次唯一标记"
		}
		if len(text) <= 200 {
			return "fail", "未严格返回本次要求的唯一标记"
		}
		return "needs_review", "回答包含额外内容，需要人工复核"
	}
	if template == "candy" {
		last := strings.TrimSpace(text[strings.LastIndex(text, "\n")+1:])
		last = strings.TrimPrefix(strings.TrimPrefix(last, "答案："), "答案:")
		last = strings.TrimSpace(last)
		if last == "21" {
			return "pass", "明确数值结论为21；未自动判断证明完整性"
		}
		var n int
		if _, e := fmt.Sscanf(last, "%d", &n); e == nil && fmt.Sprint(n) == last {
			return "fail", "明确数值结论与21不符"
		}
		return "needs_review", "未提取到唯一明确数值结论"
	}
	var want, got map[string]json.RawMessage
	if json.Unmarshal([]byte(expected), &want) != nil || json.Unmarshal([]byte(text), &got) != nil {
		return "needs_review", "输出不符合约定 JSON，无法确定性判定"
	}
	if len(want) != len(got) {
		return "fail", "JSON 字段与题目约束不符"
	}
	for key, value := range want {
		var a, b any
		if json.Unmarshal(value, &a) != nil || json.Unmarshal(got[key], &b) != nil {
			return "fail", "缺少要求的结果字段"
		}
		ra, _ := json.Marshal(a)
		rb, _ := json.Marshal(b)
		if string(ra) != string(rb) {
			return "fail", "结果字段 " + key + " 与标准值不符"
		}
	}
	return "pass", "所有确定性结果及字段约束均符合标准"
}
