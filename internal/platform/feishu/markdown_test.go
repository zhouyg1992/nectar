package feishu

import (
	"strings"
	"testing"
)

func TestBuildCardElementsNativeTableAndMarkdownPreserved(t *testing.T) {
	body := strings.Join([]string{
		"# 标题",
		"",
		"> 引用 **粗体** 和 `code`",
		"",
		"| 方案 | 优点 |",
		"|---|---|",
		"| A | 快 |",
		"",
		"## 结论",
		"",
		"选 **A**。",
	}, "\n")

	elements := buildCardElements(body)
	if len(elements) != 3 {
		t.Fatalf("elements = %d, want 3: %#v", len(elements), elements)
	}
	intro, ok := elements[0].(map[string]any)
	if !ok || intro["tag"] != "markdown" {
		t.Fatalf("intro = %#v", elements[0])
	}
	content := intro["content"].(string)
	for _, want := range []string{"# 标题", "> 引用 **粗体** 和 `code`"} {
		if !strings.Contains(content, want) {
			t.Fatalf("intro content = %q, want %q", content, want)
		}
	}

	table, ok := elements[1].(map[string]any)
	if !ok || table["tag"] != "table" {
		t.Fatalf("table = %#v", elements[1])
	}
	columns := table["columns"].([]map[string]any)
	rows := table["rows"].([]map[string]any)
	if len(columns) != 2 || columns[0]["display_name"] != "方案" {
		t.Fatalf("columns = %#v", columns)
	}
	if len(rows) != 1 || rows[0]["col0"] != "A" || rows[0]["col1"] != "快" {
		t.Fatalf("rows = %#v", rows)
	}

	conclusion, ok := elements[2].(map[string]any)
	if !ok || conclusion["tag"] != "markdown" {
		t.Fatalf("conclusion = %#v", elements[2])
	}
	content = conclusion["content"].(string)
	if !strings.Contains(content, "## 结论") || !strings.Contains(content, "选 **A**。") {
		t.Fatalf("conclusion content = %q", content)
	}
}

func TestSanitizeMarkdownTextPreservesGFM(t *testing.T) {
	body := "# H1\n\n> quote\n\n`inline` and **bold**"
	got := sanitizeMarkdownText(body)
	for _, want := range []string{"# H1", "> quote", "`inline`", "**bold**"} {
		if !strings.Contains(got, want) {
			t.Fatalf("normalized body = %q, want %q", got, want)
		}
	}
}

func TestSanitizeMarkdownTextEscapesAngles(t *testing.T) {
	body := "List<T> means a > b, but `a < b` and <at id=all></at> stay."
	got := sanitizeMarkdownText(body)
	for _, want := range []string{
		"List&#60;T&#62;",
		"a &#62; b",
		"`a < b`",
		"<at id=all></at>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("normalized body = %q, want it to contain %q", got, want)
		}
	}
}

func TestSanitizeMarkdownTextConvertsImages(t *testing.T) {
	body := "![替代文本](https://example.com/a.png)\n\n[![点击图片](https://example.com/b.png)](https://example.com)\n\n![image_key](img_v3_abc)\n\n![引用图片][img1]\n\n[img1]: https://example.com/ref.png"
	got := sanitizeMarkdownText(body)
	for _, want := range []string{
		"[Image: 替代文本](https://example.com/a.png)",
		"[Image: 点击图片](https://example.com)",
		"Image: image_key",
		"[Image: 引用图片](https://example.com/ref.png)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("normalized body = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "![") {
		t.Fatalf("markdown image syntax remains: %q", got)
	}
}

func TestSanitizeMarkdownTextPreservesFencedCode(t *testing.T) {
	body := "```markdown\n![x](https://example.com/a.png)\n<T>\n> # heading\n```"
	if got := sanitizeMarkdownText(body); got != body {
		t.Fatalf("fenced content changed:\n%s", got)
	}
}

func TestSanitizeMarkdownTextLeavesPlainTextAlone(t *testing.T) {
	body := "普通回答\n\n- 第一项\n- 第二项"
	if got := sanitizeMarkdownText(body); got != body {
		t.Fatalf("plain text changed: %q", got)
	}
}

func TestTruncateCardBodyClosesFence(t *testing.T) {
	body := "text\n```\n" + strings.Repeat("x", maxCardBodyBytes)
	got := truncateCardBody(body)
	if strings.Count(got, "```")%2 != 0 {
		t.Fatalf("truncated body left an unclosed fence:\n%s", got[len(got)-200:])
	}
	if !strings.Contains(got, "content truncated") {
		t.Fatal("missing truncation marker")
	}
}
