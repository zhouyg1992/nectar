package telegram

import (
	"strings"
	"testing"
)

func TestRenderTelegramHTMLEscapesAndFormats(t *testing.T) {
	got := renderTelegramHTML("**hello** <tag> & `**code**`")
	for _, want := range []string{"<b>hello</b>", "&lt;tag&gt;", "&amp;", "<code>**code**</code>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered html %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "<tag>") {
		t.Fatalf("raw html tag was not escaped: %q", got)
	}
}

func TestRenderTelegramHTMLCodeBlockAndTable(t *testing.T) {
	source := "```go\nif a < b {\n}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |"
	got := renderTelegramHTML(source)
	if !strings.Contains(got, `<pre><code class="language-go">if a &lt; b {`) {
		t.Fatalf("code block = %q", got)
	}
	if !strings.Contains(got, "<pre>| a | b |") {
		t.Fatalf("table was not preserved: %q", got)
	}
}

func TestSplitTelegramSourceBalancesCodeFences(t *testing.T) {
	source := "```go\n" + strings.Repeat("fmt.Println(\"hello\")\n", 100) + "```"
	chunks := splitTelegramSource(source, 240)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want multiple", len(chunks))
	}
	for index, chunk := range chunks {
		if index == 0 {
			if !strings.HasSuffix(strings.TrimSpace(chunk), "```") {
				t.Fatalf("first chunk did not close fence: %q", chunk)
			}
			continue
		}
		if !strings.HasPrefix(chunk, "```") {
			t.Fatalf("chunk %d did not reopen fence: %q", index, chunk)
		}
	}
}

func TestRenderTelegramChunksStayWithinLimit(t *testing.T) {
	source := strings.Repeat("中文😀\n", 4000)
	chunks := renderTelegramChunks(source)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want multiple", len(chunks))
	}
	for _, chunk := range chunks {
		if length := utf16Len(chunk.html); length > maxTelegramUTF16 {
			t.Fatalf("chunk length = %d, want <= %d", length, maxTelegramUTF16)
		}
	}
	if utf16Len("😀") != 2 {
		t.Fatalf("emoji utf16 length = %d", utf16Len("😀"))
	}
}

func TestTruncatePreviewKeepsTail(t *testing.T) {
	source := strings.Repeat("a", previewRunes+10) + "tail"
	got := truncatePreview(source)
	if !strings.HasPrefix(got, "…\n") || !strings.HasSuffix(got, "tail") {
		t.Fatalf("preview = %q", got)
	}
}
