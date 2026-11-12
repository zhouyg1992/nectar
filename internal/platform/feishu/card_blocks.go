package feishu

import (
	"fmt"
	"strings"
)

const maxNativeTables = 5

type tableBlock struct {
	header []string
	align  []string
	rows   [][]string
}

type cardBlock struct {
	text  string
	table *tableBlock
}

func buildCardElements(body string) []any {
	blocks := splitCardBlocks(body)
	elements := make([]any, 0, len(blocks))
	tables := 0

	for _, block := range blocks {
		if block.table != nil {
			tables++
			if tables <= maxNativeTables {
				elements = append(elements, buildNativeTable(block.table))
			} else {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": renderTableAsCode(block.table),
				})
			}
			continue
		}
		text := strings.TrimSpace(sanitizeMarkdownText(block.text))
		if text != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": text,
			})
		}
	}
	return elements
}

func splitCardBlocks(body string) []cardBlock {
	lines := strings.Split(body, "\n")
	blocks := make([]cardBlock, 0, 4)
	buffer := make([]string, 0, len(lines))
	inFence := false

	flushText := func() {
		if len(buffer) == 0 {
			return
		}
		text := strings.Join(buffer, "\n")
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, cardBlock{text: text})
		}
		buffer = buffer[:0]
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			buffer = append(buffer, line)
			continue
		}
		if inFence {
			buffer = append(buffer, line)
			continue
		}
		if !isTableStart(lines, i) {
			buffer = append(buffer, line)
			continue
		}

		flushText()
		end := i + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" && strings.Contains(lines[end], "|") {
			end++
		}

		header := splitTableRow(lines[i])
		align := tableAlignments(lines[i+1], len(header))
		rows := make([][]string, 0, end-i-2)
		for j := i + 2; j < end; j++ {
			rows = append(rows, normalizeTableRow(splitTableRow(lines[j]), len(header)))
		}
		blocks = append(blocks, cardBlock{table: &tableBlock{
			header: header,
			align:  align,
			rows:   rows,
		}})
		i = end - 1
	}

	flushText()
	return blocks
}

func tableAlignments(separator string, count int) []string {
	cells := splitTableRow(separator)
	align := make([]string, count)
	for i := range align {
		value := "left"
		if i < len(cells) {
			cell := strings.TrimSpace(cells[i])
			left := strings.HasPrefix(cell, ":")
			right := strings.HasSuffix(cell, ":")
			switch {
			case left && right:
				value = "center"
			case right:
				value = "right"
			}
		}
		align[i] = value
	}
	return align
}

func normalizeTableRow(row []string, count int) []string {
	if count <= 0 {
		return nil
	}
	if len(row) > count {
		row = append(row[:count-1], strings.Join(row[count-1:], " | "))
	}
	for len(row) < count {
		row = append(row, "")
	}
	return row
}

func buildNativeTable(table *tableBlock) map[string]any {
	columns := make([]map[string]any, len(table.header))
	for i, header := range table.header {
		columns[i] = map[string]any{
			"name":             fmt.Sprintf("col%d", i),
			"display_name":     sanitizeMarkdownText(header),
			"data_type":        "lark_md",
			"horizontal_align": table.align[i],
			"vertical_align":   "center",
			"width":            "auto",
		}
	}

	rows := make([]map[string]any, len(table.rows))
	for r, cells := range table.rows {
		row := make(map[string]any, len(columns))
		for i := range columns {
			value := ""
			if i < len(cells) {
				value = cells[i]
			}
			row[fmt.Sprintf("col%d", i)] = sanitizeMarkdownText(value)
		}
		rows[r] = row
	}

	return map[string]any{
		"tag":        "table",
		"page_size":  10,
		"row_height": "low",
		"header_style": map[string]any{
			"text_align":       "center",
			"background_style": "grey",
			"bold":             true,
			"lines":            1,
		},
		"columns": columns,
		"rows":    rows,
	}
}

func renderTableAsCode(table *tableBlock) string {
	lines := make([]string, 0, len(table.rows)+2)
	lines = append(lines, formatTableRow(table.header))
	separators := make([]string, len(table.align))
	for i, align := range table.align {
		switch align {
		case "center":
			separators[i] = ":---:"
		case "right":
			separators[i] = "---:"
		default:
			separators[i] = "---"
		}
	}
	lines = append(lines, formatTableRow(separators))
	for _, row := range table.rows {
		lines = append(lines, formatTableRow(row))
	}
	return "```\n" + strings.Join(lines, "\n") + "\n```"
}

func formatTableRow(cells []string) string {
	return "| " + strings.Join(cells, " | ") + " |"
}
