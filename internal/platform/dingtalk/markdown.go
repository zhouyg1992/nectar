package dingtalk

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	maxCardBodyBytes  = 24 << 10
	cardTitleMaxRunes = 20
	cardTitleFallback = "reply"
)

var (
	tableSeparatorCell = regexp.MustCompile(`^:?-{3,}:?$`)
	htmlTagPattern     = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	markdownTitleNoise = strings.NewReplacer(
		"**", "", "__", "", "`", "", "~~", "",
	)
)

// preprocessDingTalkMarkdown adapts content for the IM sampleMarkdown renderer.
// Leading spaces become NBSP and single newlines become hard line breaks.
// Fenced code blocks are left untouched.
func preprocessDingTalkMarkdown(s string) string {
	s = normalizeMarkdownTables(strings.ReplaceAll(s, "\r\n", "\n"))
	lines := strings.Split(s, "\n")
	inCodeBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
		}
		if inCodeBlock {
			continue
		}
		spaceCount := len(line) - len(strings.TrimLeft(line, " "))
		if spaceCount > 0 {
			lines[i] = strings.Repeat("\u00A0", spaceCount) + line[spaceCount:]
		}
	}

	var sb strings.Builder
	inCodeBlock = false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
		}
		sb.WriteString(line)
		if i >= len(lines)-1 {
			continue
		}
		if !inCodeBlock && line != "" && lines[i+1] != "" {
			sb.WriteString("  \n")
		} else {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func normalizeA2UIMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = truncateUTF8(s, maxCardBodyBytes)
	s = escapeHTMLOutsideCode(s)
	if strings.Count(s, "```")%2 == 1 {
		s += "\n```"
	}
	return s
}

func normalizeMarkdownTables(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	inCodeBlock := false
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			out = append(out, line)
			i++
			continue
		}
		if !inCodeBlock && i+1 < len(lines) && isTableRow(line) && isTableSeparator(lines[i+1]) {
			out = append(out, "```text")
			for i < len(lines) && isTableRow(lines[i]) {
				out = append(out, strings.TrimRight(lines[i], " \t"))
				i++
			}
			out = append(out, "```")
			continue
		}
		out = append(out, line)
		i++
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	return strings.Contains(line, "|") && strings.TrimSpace(line) != ""
}

func isTableSeparator(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !tableSeparatorCell.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return true
}

// escapeHTMLOutsideCode prevents raw HTML from breaking the card while keeping
// fenced code blocks byte-for-byte intact.
func escapeHTMLOutsideCode(s string) string {
	lines := strings.Split(s, "\n")
	inCodeBlock := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock || strings.Contains(line, "<a atId=") {
			continue
		}
		lines[i] = htmlTagPattern.ReplaceAllStringFunc(line, func(tag string) string {
			lower := strings.ToLower(tag)
			if strings.HasPrefix(lower, "<http://") || strings.HasPrefix(lower, "<https://") || strings.HasPrefix(lower, "<mailto:") {
				return tag
			}
			return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(tag)
		})
	}
	return strings.Join(lines, "\n")
}

func cardTitleFromContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return cardTitleFallback
	}
	lines := strings.Split(content, "\n")
	title := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#>-*+ \t")
		line = markdownTitleNoise.Replace(line)
		line = strings.TrimSpace(line)
		if line != "" {
			title = line
			break
		}
	}
	if title == "" || !hasReadableChar(title) {
		return cardTitleFallback
	}
	runes := []rune(title)
	if len(runes) > cardTitleMaxRunes {
		return string(runes[:cardTitleMaxRunes])
	}
	return title
}

func hasReadableChar(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func summaryText(content, title string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		text = strings.TrimSpace(title)
	}
	if text == "" {
		return "Nectar"
	}
	text = markdownTitleNoise.Replace(text)
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[:idx]
	}
	text = strings.TrimSpace(text)
	if text == "" || !hasReadableChar(text) {
		text = "Nectar"
	}
	text = truncateRunes(text, 200)
	return truncateUTF8(text, 2048)
}
