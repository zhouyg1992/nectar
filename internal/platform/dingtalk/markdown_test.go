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

func TestNormalizeA2UIMarkdownConvertsTable(t *testing.T) {
	got := normalizeA2UIMarkdown("| a | b |\n|---|---|\n| 1 | 2 |")
	if !strings.Contains(got, "```text") {
		t.Fatalf("table was not converted: %q", got)
	}
	if !strings.Contains(got, "| a | b |") {
		t.Fatalf("table content lost: %q", got)
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

func TestTruncateUTF8(t *testing.T) {
	input := strings.Repeat("中文", maxCardBodyBytes)
	got := truncateUTF8(input, maxCardBodyBytes)
	if !utf8.ValidString(got) {
		t.Fatal("truncated string is not valid UTF-8")
	}
	if !strings.Contains(got, "内容过长") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}
