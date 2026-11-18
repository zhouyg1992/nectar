package dingtalk

import (
	"fmt"
	"strings"
)

const (
	maxCardBlocks           = 40
	maxTableRows            = 100
	maxTableColumns         = 10
	maxMarkdownTableRows    = 50
	maxMarkdownTableColumns = 8
	tableFallbackTag        = "```text"
)

type cardBlock struct {
	Markdown string
	Table    *markdownTable
}

type markdownTable struct {
	Headers []string
	Rows    [][]string
	Raw     string
}

func parseCardBlocks(content string) []cardBlock {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	var blocks []cardBlock
	var pending []string

	flushMarkdown := func() {
		text := strings.TrimSpace(strings.Join(pending, "\n"))
		if text != "" {
			blocks = append(blocks, cardBlock{Markdown: text})
		}
		pending = nil
	}

	inCodeBlock := false
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			pending = append(pending, line)
			i++
			continue
		}
		if !inCodeBlock && !strings.HasPrefix(trimmed, ">") &&
			i+1 < len(lines) && isTableRow(line) && isTableSeparator(lines[i+1]) {
			table, next := parseMarkdownTable(lines, i)
			if len(table.Headers) > 0 && len(table.Rows) > 0 {
				flushMarkdown()
				blocks = append(blocks, cardBlock{Table: &table})
				i = next
				continue
			}
		}
		pending = append(pending, line)
		i++
	}
	flushMarkdown()

	blocks = splitPlainParagraphs(blocks)
	if len(blocks) > maxCardBlocks {
		return []cardBlock{{Markdown: content}}
	}
	return blocks
}

func splitPlainParagraphs(blocks []cardBlock) []cardBlock {
	out := make([]cardBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Table != nil {
			out = append(out, block)
			continue
		}
		if _, ok := plainCardText(block.Markdown); !ok {
			out = append(out, block)
			continue
		}
		parts := strings.Split(block.Markdown, "\n\n")
		if len(parts) == 1 {
			out = append(out, block)
			continue
		}
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, cardBlock{Markdown: part})
			}
		}
	}
	return out
}

// plainCardText returns the text for a block that can use the Text component.
// Text stretches to the card width more reliably than Markdown and preserves
// native newlines without Markdown hard-break side effects. Blocks containing
// Markdown syntax keep the Markdown component so links, lists, emphasis, and
// code still render correctly.
func plainCardText(s string) (string, bool) {
	text := strings.NewReplacer("\\\n", "\n", "  \n", "\n").Replace(s)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "`*_#[]|>~") ||
			strings.Contains(line, "://") ||
			strings.HasPrefix(line, "mailto:") {
			return "", false
		}
		if strings.HasPrefix(line, "- ") ||
			strings.HasPrefix(line, "+ ") ||
			strings.HasPrefix(line, "1. ") {
			return "", false
		}
	}
	return text, true
}

func parseMarkdownTable(lines []string, start int) (markdownTable, int) {
	headers := splitTableRow(lines[start])
	if len(headers) > maxTableColumns {
		headers = headers[:maxTableColumns]
	}
	table := markdownTable{Headers: headers}
	i := start + 2
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" || !isTableRow(line) {
			break
		}
		if isTableSeparator(line) {
			i++
			continue
		}
		row := splitTableRow(line)
		for len(row) < len(headers) {
			row = append(row, "")
		}
		if len(row) > len(headers) {
			row = row[:len(headers)]
		}
		table.Rows = append(table.Rows, row)
		if len(table.Rows) >= maxTableRows {
			i++
			break
		}
		i++
	}
	table.Raw = strings.Join(lines[start:i], "\n")
	return table, i
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, cleanTableCell(part))
	}
	return cells
}

func cleanTableCell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `\|`, "|")
	value = strings.NewReplacer(
		"`", "",
		"**", "",
		"__", "",
		"~~", "",
	).Replace(value)
	return strings.TrimSpace(value)
}

// useMarkdownTable keeps small tables on the Markdown renderer. The native
// A2UI Table has a fixed gray header style and can render its header at a
// different width than the body; Markdown tables keep the two aligned and
// avoid the gray header entirely. Wide/long tables still use the native Table
// so pagination and horizontal layout remain available.
func useMarkdownTable(table *markdownTable) bool {
	if table == nil {
		return false
	}
	return len(table.Rows) <= maxMarkdownTableRows &&
		len(table.Headers) <= maxMarkdownTableColumns
}

func tableComponent(id string, table markdownTable) map[string]any {
	meta := make([]map[string]string, 0, len(table.Headers))
	for i, header := range table.Headers {
		name := strings.TrimSpace(header)
		if name == "" {
			name = fmt.Sprintf("Column %d", i+1)
		}
		meta = append(meta, map[string]string{
			"colKey":  fmt.Sprintf("c%d", i),
			"colName": name,
			"type":    "STRING",
		})
	}
	rows := make([]map[string]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		item := make(map[string]string, len(table.Headers))
		for i := range table.Headers {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			item[fmt.Sprintf("c%d", i)] = value
		}
		rows = append(rows, item)
	}
	return map[string]any{
		"id":        id,
		"component": "Table",
		"maxWidth":  "match_parent",
		"widthMode": "match_parent",
		"data": map[string]any{
			"meta": meta,
			"data": rows,
		},
		"zebra":            true,
		"maxLine":          3,
		"fallbackMarkdown": tableFallbackTag + "\n" + table.Raw + "\n```",
	}
}
