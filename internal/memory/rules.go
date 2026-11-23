package memory

import (
	"strings"
	"unicode"
)

// RuleCandidate is a deterministic, model-free memory candidate. Everything
// extracted by this package starts as a candidate and still needs review.
type RuleCandidate struct {
	Content    string
	Kind       Kind
	Confidence float64
	Tags       []string
	Evidence   []string
}

type rulePattern struct {
	markers    []string
	kind       Kind
	confidence float64
	tag        string
}

// rulePatterns deliberately stays narrow. Automatic extraction is
// precision-first: a bare negation such as "不是" or a task acceptance
// criterion is not a durable memory. Explicit remember/always phrasing is the
// only signal strong enough to create a candidate without a model.
var rulePatterns = []rulePattern{
	{
		markers:    []string{"记住", "以后", "今后", "从现在开始", "请总是", "请始终", "别再", "不要再", "下次不要"},
		kind:       KindProfile,
		confidence: 0.88,
		tag:        "explicit",
	},
}

var transientMarkers = []string{
	"这次", "临时", "现在", "今天", "先试试", "帮我跑", "看一下", "看看这个",
}

var taskRequestMarkers = []string{
	"帮我", "请你", "请帮", "麻烦", "检查", "验证", "排查", "修复", "复现",
	"定位", "测试一下", "跑一下", "看一下", "看看", "审一下",
}

var taskSignalMarkers = []string{
	"必须", "不能", "不要", "不是", "需要", "要求", "验收", "标准", "条件",
	"过滤", "归因", "截图", "页面", "接口", "字段", "步骤", "清单", "用例",
}

// ExtractRuleCandidates extracts at most max explicit, durable-looking
// sentences from one user turn. It intentionally errs on the side of missing
// memories: false positives are more damaging than false negatives here.
func ExtractRuleCandidates(text string, max int) []RuleCandidate {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "/") || max <= 0 {
		return nil
	}
	if looksLikeTaskRequest(text) {
		return nil
	}
	var out []RuleCandidate
	seen := make(map[string]bool)
	for _, sentence := range splitSentences(text) {
		sentence = strings.TrimSpace(sentence)
		if len([]rune(sentence)) < 4 || isTransient(sentence) || isQuestion(sentence) {
			continue
		}
		best, ok := matchRule(sentence)
		if !ok {
			continue
		}
		content := truncateRunes(sentence, 300)
		key := strings.ToLower(content)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, RuleCandidate{
			Content:    content,
			Kind:       best.kind,
			Confidence: best.confidence,
			Tags:       []string{"rule", best.tag},
			Evidence:   []string{sentence},
		})
		if len(out) >= max {
			break
		}
	}
	return out
}

func matchRule(sentence string) (rulePattern, bool) {
	lower := strings.ToLower(sentence)
	var (
		best  rulePattern
		found bool
	)
	for _, pattern := range rulePatterns {
		matched := false
		for _, marker := range pattern.markers {
			if strings.Contains(lower, strings.ToLower(marker)) {
				matched = true
				break
			}
		}
		if matched && (!found || pattern.confidence > best.confidence) {
			best = pattern
			found = true
		}
	}
	if !found {
		return rulePattern{}, false
	}
	return best, true
}

func hasAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isTransient(sentence string) bool {
	return hasAny(strings.ToLower(sentence), transientMarkers...)
}

func isQuestion(sentence string) bool {
	trimmed := strings.TrimSpace(sentence)
	if strings.ContainsAny(trimmed, "?？") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if hasAny(lower, "请问", "想问", "问一下") {
		return true
	}
	return hasAny(lower,
		"什么", "怎么", "如何", "为什么", "为啥", "哪里", "哪个", "哪些",
		"是否", "是不是", "能不能", "可不可以", "有没有", "要不要", "会不会",
		"行不行", "对不对", "好不好", "多少", "几点", "几号", "咋",
		"吗", "呢", "嘛",
	)
}

// looksLikeTaskRequest detects one-off task descriptions such as checklists,
// acceptance criteria, and explicit "check/fix this" requests. Those turns
// describe what to do now, not what should persist across sessions.
func looksLikeTaskRequest(text string) bool {
	if hasChecklist(text) {
		return true
	}
	lower := strings.ToLower(text)
	if !hasAny(lower, taskRequestMarkers...) {
		return false
	}
	return hasAny(lower, taskSignalMarkers...)
}

func hasChecklist(text string) bool {
	items := 0
	for _, line := range strings.Split(text, "\n") {
		if hasListPrefix(strings.TrimSpace(line)) {
			items++
			if items >= 2 {
				return true
			}
		}
	}
	return false
}

func hasListPrefix(line string) bool {
	if line == "" {
		return false
	}
	for _, prefix := range []string{"- ", "* ", "• ", "□ ", "☐ ", "[ ] ", "[x] ", "[X] "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	runes := []rune(line)
	if len(runes) >= 2 && strings.ContainsRune("一二三四五六七八九十", runes[0]) {
		switch runes[1] {
		case '、', '.', '）', ')':
			return true
		}
	}
	if len(runes) >= 3 && runes[0] == '（' && unicode.IsDigit(runes[1]) && runes[2] == '）' {
		return true
	}

	digits := 0
	for digits < len(runes) && unicode.IsDigit(runes[digits]) {
		digits++
	}
	if digits == 0 || digits >= len(runes) {
		return false
	}
	switch runes[digits] {
	case '.', '、', ')', '）':
		return true
	}
	return false
}

func splitSentences(text string) []string {
	var (
		out []string
		buf strings.Builder
	)
	flush := func() {
		if value := strings.TrimSpace(buf.String()); value != "" {
			out = append(out, value)
		}
		buf.Reset()
	}
	for _, r := range text {
		switch r {
		case '。', '！', '？', '!', '?', ';', '\n':
			flush()
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
