package dingtalk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPreprocessDingTalkMarkdown(t *testing.T) {
	got := preprocessDingTalkMarkdown("  indented\nnext\n\n```\n  code\n```")
	if !strings.Contains(got, "\u00A0\u00A0indented  \nnext") {
		t.Fatalf("unexpected markdown: %q", got)
	}
	if !strings.Contains(got, "```\n  code\n```") {
		t.Fatalf("code block should be untouched: %q", got)
	}
}

func TestCardTitleFromContent(t *testing.T) {
	if got := cardTitleFromContent("## **Hello**\nbody"); got != "Hello" {
		t.Fatalf("title = %q", got)
	}
	if got := cardTitleFromContent("🎉"); got != cardTitleFallback {
		t.Fatalf("emoji-only title = %q", got)
	}
}

func TestNormalizeA2UIMarkdownBalancesFence(t *testing.T) {
	got := normalizeA2UIMarkdown("before\n```go\nfmt.Println(1)")
	if strings.Count(got, "```")%2 != 0 {
		t.Fatalf("unbalanced fence: %q", got)
	}
}

func TestNormalizeA2UIMarkdownKeepsSingleLineBreaks(t *testing.T) {
	got := normalizeA2UIMarkdown("1: first\n2: second\n\nUse /switch")
	if !strings.Contains(got, "1: first  \n2: second") {
		t.Fatalf("line breaks were collapsed: %q", got)
	}
	if strings.Contains(got, "```") {
		t.Fatalf("unexpected code fence: %q", got)
	}
}

func TestCommandCardBodiesKeepLineBreaks(t *testing.T) {
	bodies := map[string]string{
		"help":   "**Session**\n- `/new` — new\n- `/list` — list\n\nUse /help",
		"status": "Engine: codex\nModel: deepseek\nSession: ses_1",
		"list":   "1: first\n2: second\n\nUse /switch 1",
		"cron":   "`task-1` · active\nPrompt: check CI\nNext: 2026-11-16 09:00",
		"memory": "Memory\n- id: m1\n- kind: fact",
	}
	for name, body := range bodies {
		got := normalizeA2UIMarkdown(body)
		if !strings.Contains(got, "  \n") {
			t.Fatalf("%s card lost hard line breaks: %q", name, got)
		}
	}
}

func TestNormalizedTableStillBecomesTableBlock(t *testing.T) {
	body := normalizeA2UIMarkdown("| a | b |\n|---|---|\n| 1 | 2 |")
	blocks := parseCardBlocks(body)
	if len(blocks) != 1 || blocks[0].Table == nil {
		t.Fatalf("normalized table was not parsed as a table: %#v", blocks)
	}
	if len(blocks[0].Table.Rows) != 1 || blocks[0].Table.Rows[0][1] != "2" {
		t.Fatalf("normalized table rows = %#v", blocks[0].Table.Rows)
	}
}

func TestPreprocessDingTalkMarkdownConvertsTable(t *testing.T) {
	got := preprocessDingTalkMarkdown("| a | b |\n|---|---|\n| 1 | 2 |")
	if !strings.Contains(got, "```text") {
		t.Fatalf("table was not converted: %q", got)
	}
	if !strings.Contains(got, "| a | b |") {
		t.Fatalf("table content lost: %q", got)
	}
}

func TestParseCardBlocksTable(t *testing.T) {
	blocks := parseCardBlocks("before\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nafter")
	if len(blocks) != 3 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[0].Markdown != "before" || blocks[2].Markdown != "after" {
		t.Fatalf("markdown blocks = %#v", blocks)
	}
	if blocks[1].Table == nil {
		t.Fatalf("middle block is not a table: %#v", blocks[1])
	}
	if len(blocks[1].Table.Headers) != 2 || blocks[1].Table.Headers[0] != "a" {
		t.Fatalf("table = %#v", blocks[1].Table)
	}
	if len(blocks[1].Table.Rows) != 1 || blocks[1].Table.Rows[0][1] != "2" {
		t.Fatalf("table rows = %#v", blocks[1].Table.Rows)
	}
}

func TestEscapeHTMLOutsideCode(t *testing.T) {
	got := normalizeA2UIMarkdown("> quote\n<div>x</div>\n<https://example.com>\n```\n<div>keep</div>\n```")
	if !strings.Contains(got, "> quote") {
		t.Fatalf("blockquote was changed: %q", got)
	}
	if !strings.Contains(got, "&lt;div&gt;x&lt;/div&gt;") {
		t.Fatalf("html was not escaped: %q", got)
	}
	if !strings.Contains(got, "<div>keep</div>") {
		t.Fatalf("code block html was escaped: %q", got)
	}
	if !strings.Contains(got, "<https://example.com>") {
		t.Fatalf("autolink was escaped: %q", got)
	}
}

func TestNormalizeSessionListIsPlatformSafe(t *testing.T) {
	body := "1:  hello  ·  ses_bca6ee20  ·  codex  ·  ctx 18.7k/996.1k (1.9%)  ·  just now  ·  active\n" +
		"2:  Session Nov 15 16:58  ·  ses_04a8d5d7  ·  codex  ·  ctx 19.2k/996.1k (2.0%)  ·  just now\n\n" +
		"Use /switch 1, /switch ID, or /switch NAME. /list all includes archived sessions."
	if got := normalizeA2UIMarkdown(body); !strings.Contains(got, "1:  hello") || !strings.Contains(got, "  \n2:  Session") {
		t.Fatalf("session list lost line breaks or content:\\n%s", got)
	}
	fallback := preprocessDingTalkMarkdown(body)
	for _, want := range []string{"1:", "active", "Use /switch 1"} {
		if !strings.Contains(fallback, want) {
			t.Fatalf("dingtalk fallback = %q, want %q", fallback, want)
		}
	}
}

func TestTruncateUTF8(t *testing.T) {
	input := strings.Repeat("中文", maxCardBodyBytes)
	got := truncateUTF8(input, maxCardBodyBytes)
	if !utf8.ValidString(got) {
		t.Fatal("truncated string is not valid UTF-8")
	}
	if !strings.Contains(got, "content truncated") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}
