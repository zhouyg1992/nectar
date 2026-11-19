package memory

import (
	"strings"
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

var rulePatterns = []rulePattern{
	{
		markers:    []string{"记住", "以后", "默认", "请总是", "请始终", "别再", "不要再"},
		kind:       KindProfile,
		confidence: 0.88,
		tag:        "explicit",
	},
	{
		markers:    []string{"我不喜欢", "我喜欢", "偏好", "习惯", "更喜欢", "希望以后"},
		kind:       KindProfile,
		confidence: 0.78,
		tag:        "preference",
	},
	{
		markers:    []string{"不对", "不是", "应该是", "改成", "改用", "别用", "不能用", "不要用"},
		kind:       KindProfile,
		confidence: 0.82,
		tag:        "correction",
	},
	{
		markers:    []string{"这个项目", "本项目", "技术栈", "部署", "端口", "路径", "仓库", "架构"},
		kind:       KindProject,
		confidence: 0.70,
		tag:        "project",
	},
}

var transientMarkers = []string{
	"这次", "临时", "现在", "今天", "先试试", "帮我跑", "看一下", "看看这个",
}

// ExtractRuleCandidates extracts at most max explicit, durable-looking
// sentences from one user turn. It intentionally errs on the side of missing
// memories: false positives are more damaging than false negatives here.
func ExtractRuleCandidates(text string, max int) []RuleCandidate {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "/") || max <= 0 {
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
	// A sentence about the current project stays project-scoped even when it
	// also contains an explicit "以后" marker.
	if best.kind != KindProject && hasAny(lower, "这个项目", "本项目", "技术栈", "部署", "仓库") {
		best.kind = KindProject
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
	if strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "？") {
		return true
	}
	return hasAny(strings.ToLower(trimmed), "什么", "怎么", "如何", "哪里", "哪个", "是否", "吗", "呢")
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
