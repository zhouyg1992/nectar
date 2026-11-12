package feishu

import (
	"regexp"
	"strings"
)

var (
	tableDelimiter = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)
	linkedImage    = regexp.MustCompile(`\[!\[([^\]]*)\]\([^)]*\)\]\(([^)]+)\)`)
	markdownImage  = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	referenceImage = regexp.MustCompile(`!\[([^\]]*)\]\[([^\]]*)\]`)
	referenceDef   = regexp.MustCompile(`^\s*\[([^\]]+)\]:\s*(\S+)`)
)

// sanitizeMarkdownText makes only the adjustments that Feishu's Card 2.0
// markdown renderer cannot tolerate. Headings, blockquotes, inline code,
// lists, links and fenced code are deliberately left untouched.
func sanitizeMarkdownText(body string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	refs := make(map[string]string)
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if match := referenceDef.FindStringSubmatch(line); match != nil {
			refs[normalizeReference(match[1])] = match[2]
		}
	}

	inFence = false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = sanitizeReferenceImages(line, refs)
		lines[i] = escapeAngles(sanitizeMarkdownImages(line))
	}
	return strings.Join(lines, "\n")
}

func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") {
		return false
	}
	return tableDelimiter.MatchString(lines[i+1])
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// Feishu cards reject Markdown image syntax unless the target is a valid
// Feishu image key. Convert images to links/text so external URLs cannot make
// the whole card update fail.
func sanitizeMarkdownImages(line string) string {
	line = linkedImage.ReplaceAllStringFunc(line, func(match string) string {
		parts := linkedImage.FindStringSubmatch(match)
		label := imageLabel(parts[1])
		return "[" + label + "](" + parts[2] + ")"
	})
	return markdownImage.ReplaceAllStringFunc(line, func(match string) string {
		parts := markdownImage.FindStringSubmatch(match)
		label := imageLabel(parts[1])
		url := parts[2]
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "data:") {
			return "[" + label + "](" + url + ")"
		}
		return label
	})
}

func sanitizeReferenceImages(line string, refs map[string]string) string {
	return referenceImage.ReplaceAllStringFunc(line, func(match string) string {
		parts := referenceImage.FindStringSubmatch(match)
		alt, ref := parts[1], parts[2]
		if strings.TrimSpace(ref) == "" {
			ref = alt
		}
		url := refs[normalizeReference(ref)]
		label := imageLabel(alt)
		if url == "" {
			return label
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "data:") {
			return "[" + label + "](" + url + ")"
		}
		return label
	})
}

func normalizeReference(ref string) string {
	return strings.ToLower(strings.Join(strings.Fields(ref), " "))
}

func imageLabel(alt string) string {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return "图片"
	}
	return "图片：" + alt
}

var knownTags = []string{
	"<at ", "<at>", "</at>",
	"<a ", "<a>", "</a>",
	"<text_tag", "</text_tag>",
	"<br>", "<br/>", "<br />",
}

// escapeAngles turns bare "<" and ">" into HTML entities so Feishu does not
// swallow them as markup. Fenced code and Feishu-supported tags are untouched.
func escapeAngles(line string) string {
	if !strings.ContainsAny(line, "<>") {
		return line
	}
	if prefix, rest := blockquotePrefix(line); prefix != "" {
		return prefix + escapeAngles(rest)
	}

	var b strings.Builder
	b.Grow(len(line))
	inCode := false

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '`':
			inCode = !inCode
			b.WriteByte(c)
		case inCode:
			b.WriteByte(c)
		case c == '<':
			if n := knownTagLen(line[i:]); n > 0 {
				b.WriteString(line[i : i+n])
				i += n - 1
				continue
			}
			b.WriteString("&#60;")
		case c == '>':
			b.WriteString("&#62;")
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

func blockquotePrefix(line string) (string, string) {
	i := 0
	for i < len(line) {
		j := i
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j >= len(line) || line[j] != '>' {
			break
		}
		i = j + 1
	}
	return line[:i], line[i:]
}

func knownTagLen(s string) int {
	for _, tag := range knownTags {
		if !strings.HasPrefix(s, tag) {
			continue
		}
		if idx := strings.IndexByte(s, '>'); idx >= 0 {
			return idx + 1
		}
	}
	return 0
}
