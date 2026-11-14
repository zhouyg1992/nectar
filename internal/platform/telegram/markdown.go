package telegram

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	maxTelegramUTF16  = 4096
	defaultChunkRunes = 3000
	previewRunes      = 3000
)

var (
	boldPattern         = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	strikePattern       = regexp.MustCompile(`~~([^~\n]+)~~`)
	linkPattern         = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)
	codeLanguagePattern = regexp.MustCompile(`^[a-zA-Z0-9_+#.-]{1,24}$`)
)

type renderedChunk struct {
	html  string
	plain string
}

func renderTelegramChunks(source string) []renderedChunk {
	return renderChunks(source, defaultChunkRunes)
}

func renderChunks(source string, limit int) []renderedChunk {
	if limit < 200 {
		limit = 200
	}
	var out []renderedChunk
	for _, part := range splitTelegramSource(source, limit) {
		rendered := renderTelegramHTML(part)
		if rendered == "" {
			continue
		}
		if utf16Len(rendered) > maxTelegramUTF16 && limit > 200 && len([]rune(part)) > 200 {
			out = append(out, renderChunks(part, limit/2)...)
			continue
		}
		out = append(out, renderedChunk{html: rendered, plain: part})
	}
	return out
}

func renderTelegramHTML(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(source, "\n")
	var out strings.Builder

	inCode := false
	var code strings.Builder
	codeLanguage := ""

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inCode {
				inCode = true
				codeLanguage = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				continue
			}
			writeCodeBlock(&out, code.String(), codeLanguage)
			inCode = false
			codeLanguage = ""
			code.Reset()
			continue
		}
		if inCode {
			code.WriteString(line)
			code.WriteByte('\n')
			continue
		}

		if isTableStart(lines, i) {
			var table []string
			for i < len(lines) && strings.Contains(lines[i], "|") {
				table = append(table, lines[i])
				i++
			}
			i--
			out.WriteString("<pre>")
			out.WriteString(html.EscapeString(strings.Join(table, "\n")))
			out.WriteString("</pre>\n")
			continue
		}

		if level, heading, ok := parseHeading(line); ok && level <= 6 {
			out.WriteString("<b>")
			out.WriteString(renderInline(heading))
			out.WriteString("</b>\n")
			continue
		}

		if strings.HasPrefix(trimmed, ">") {
			out.WriteString("<blockquote>")
			out.WriteString(renderInline(strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))))
			out.WriteString("</blockquote>\n")
			continue
		}

		if isHorizontalRule(trimmed) {
			out.WriteString("────────\n")
			continue
		}

		out.WriteString(renderInline(line))
		out.WriteByte('\n')
	}

	if inCode {
		writeCodeBlock(&out, code.String(), codeLanguage)
	}
	return strings.TrimSpace(out.String())
}

func writeCodeBlock(out *strings.Builder, content, language string) {
	out.WriteString("<pre><code")
	if codeLanguagePattern.MatchString(language) {
		out.WriteString(` class="language-`)
		out.WriteString(language)
		out.WriteString(`"`)
	}
	out.WriteString(">")
	out.WriteString(html.EscapeString(strings.TrimSuffix(content, "\n")))
	out.WriteString("</code></pre>\n")
}

func parseHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) {
		return 0, "", false
	}
	if trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, "", false
	}
	return level, strings.TrimSpace(trimmed[level:]), true
}

func isHorizontalRule(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	char := trimmed[0]
	if char != '-' && char != '*' && char != '_' {
		return false
	}
	for i := 1; i < len(trimmed); i++ {
		if trimmed[i] != char {
			return false
		}
	}
	return true
}

func isTableStart(lines []string, index int) bool {
	if index+1 >= len(lines) || !strings.Contains(lines[index], "|") {
		return false
	}
	separator := strings.TrimSpace(lines[index+1])
	if separator == "" || !strings.Contains(separator, "|") || !strings.Contains(separator, "-") {
		return false
	}
	for _, char := range separator {
		switch char {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

func renderInline(text string) string {
	var out strings.Builder
	for {
		index := strings.IndexByte(text, '`')
		if index < 0 {
			out.WriteString(renderInlinePlain(text))
			return out.String()
		}
		out.WriteString(renderInlinePlain(text[:index]))
		rest := text[index+1:]
		end := strings.IndexByte(rest, '`')
		if end < 0 {
			out.WriteString(html.EscapeString(text[index:]))
			return out.String()
		}
		out.WriteString("<code>")
		out.WriteString(html.EscapeString(rest[:end]))
		out.WriteString("</code>")
		text = rest[end+1:]
	}
}

func renderInlinePlain(text string) string {
	text = html.EscapeString(text)
	text = boldPattern.ReplaceAllString(text, "<b>$1</b>")
	text = strikePattern.ReplaceAllString(text, "<s>$1</s>")
	text = linkPattern.ReplaceAllString(text, `<a href="$2">$1</a>`)
	return text
}

func splitTelegramSource(source string, limit int) []string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	if limit < 200 {
		limit = 200
	}

	var chunks []string
	text := source
	for text != "" {
		runes := []rune(text)
		if len(runes) <= limit {
			chunks = append(chunks, text)
			break
		}

		cut := breakIndex(runes, limit)
		chunk := string(runes[:cut])
		prefix := ""
		if language, open := openCodeFence(chunk); open {
			chunk += "\n```"
			prefix = "```" + language + "\n"
		}
		chunks = append(chunks, strings.TrimRight(chunk, "\n"))
		text = prefix + string(runes[cut:])
	}
	return chunks
}

func breakIndex(runes []rune, limit int) int {
	minimum := limit / 2
	for index := limit; index > minimum; index-- {
		if runes[index-1] == '\n' {
			return index
		}
	}
	return limit
}

func openCodeFence(text string) (string, bool) {
	open := false
	language := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "```") {
			continue
		}
		if open {
			open = false
			language = ""
			continue
		}
		open = true
		language = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		if !codeLanguagePattern.MatchString(language) {
			language = ""
		}
	}
	return language, open
}

func truncatePreview(source string) string {
	runes := []rune(source)
	if len(runes) <= previewRunes {
		return source
	}
	return "…\n" + string(runes[len(runes)-previewRunes:])
}

func utf16Len(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func cardSource(card platform.Card) string {
	var lines []string
	if title := strings.TrimSpace(card.Title); title != "" {
		lines = append(lines, "**"+title+"**")
	}
	if activity := strings.TrimSpace(card.Activity); activity != "" {
		lines = append(lines, activity)
	}
	if body := strings.TrimSpace(card.Body); body != "" {
		lines = append(lines, body)
	}
	if footer := strings.TrimSpace(card.Footer); footer != "" {
		lines = append(lines, "— "+footer)
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

func previewSource(source string) string {
	return truncatePreview(strings.TrimSpace(source))
}
