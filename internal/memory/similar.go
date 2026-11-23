package memory

import (
	"strings"
	"unicode"
)

// SimilarContent reports whether two memory texts are close enough to be the
// same candidate. It is intentionally conservative: it only merges wording
// variants and punctuation differences, not merely related topics.
func SimilarContent(a, b string) bool {
	left := normalizeMemoryText(a)
	right := normalizeMemoryText(b)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}

	leftRunes, rightRunes := len([]rune(left)), len([]rune(right))
	if leftRunes < 4 || rightRunes < 4 {
		return false
	}
	if float64(minInt(leftRunes, rightRunes))/float64(maxInt(leftRunes, rightRunes)) < 0.5 {
		return false
	}

	leftTokens := tokenSet(a)
	rightTokens := tokenSet(b)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return false
	}
	overlap := 0
	for token := range leftTokens {
		if rightTokens[token] {
			overlap++
		}
	}
	smaller := minInt(len(leftTokens), len(rightTokens))
	return float64(overlap)/float64(smaller) >= 0.8
}

func normalizeMemoryText(text string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func tokenSet(text string) map[string]bool {
	out := make(map[string]bool)
	for _, token := range tokenizeMemory(text) {
		out[token] = true
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
