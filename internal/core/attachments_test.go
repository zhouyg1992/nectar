package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/attachment"
)

func TestBuildAttachmentPromptInlinesTextFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	content := "原始队列生成 prompt\nunique attachment content"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	prompt := buildAttachmentPrompt("翻译这个文件内容", []attachment.Local{{
		Kind:     attachment.KindFile,
		Path:     path,
		Name:     "原始队列生成prompt.txt",
		MimeType: "text/plain; charset=utf-8",
	}})

	for _, want := range []string{
		"翻译这个文件内容",
		"不要用历史对话中的其他图片或文件代替本轮附件",
		`[File "原始队列生成prompt.txt" saved at: ` + path + `]`,
		"<attachment_text",
		content,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildAttachmentPromptDoesNotInlineBinaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\nbinary"), 0o600); err != nil {
		t.Fatal(err)
	}

	prompt := buildAttachmentPrompt("分析这个文件", []attachment.Local{{
		Kind:     attachment.KindFile,
		Path:     path,
		Name:     "report.pdf",
		MimeType: "application/pdf",
	}})

	if strings.Contains(prompt, "<attachment_text") {
		t.Fatalf("binary file was inlined:\n%s", prompt)
	}
	if !strings.Contains(prompt, `[File "report.pdf" saved at: `+path+`]`) {
		t.Fatalf("binary path missing:\n%s", prompt)
	}
}

func TestBuildAttachmentPromptTruncatesLargeTextFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", int(maxInlineTextBytes)+10)), 0o600); err != nil {
		t.Fatal(err)
	}

	prompt := buildAttachmentPrompt("分析文件", []attachment.Local{{
		Kind:     attachment.KindFile,
		Path:     path,
		Name:     "large.txt",
		MimeType: "text/plain",
	}})
	if !strings.Contains(prompt, "... [内容已截断]") {
		t.Fatalf("large file was not marked truncated")
	}
}
