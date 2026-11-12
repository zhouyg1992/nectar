package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	attachmentDownloadTimeout = 30 * time.Second
	maxInlineTextBytes        = 48 * 1024
)

func (r *Router) prepareAttachments(msg platform.Message, workdir string) ([]attachment.Local, func(), error) {
	if len(msg.Attachments) == 0 {
		return nil, func() {}, nil
	}

	baseDir := filepath.Join(workdir, ".nectar", "attachments")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, func() {}, fmt.Errorf("create attachment base dir: %w", err)
	}
	dir, err := os.MkdirTemp(baseDir, attachmentDirName(msg.MessageID)+"-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create attachment dir: %w", err)
	}
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			r.logger.Debug("remove attachment dir failed", "dir", dir, "error", err)
			return
		}
		_ = os.Remove(filepath.Dir(dir))
		_ = os.Remove(filepath.Dir(filepath.Dir(dir)))
	}

	ctx, cancel := context.WithTimeout(r.ctx, attachmentDownloadTimeout)
	defer cancel()

	attachments := make([]attachment.Local, 0, len(msg.Attachments))
	for i, resource := range msg.Attachments {
		local, err := r.platform.Download(ctx, resource, dir)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("download attachment %d (%s): %w", i+1, resource.Kind, err)
		}
		if strings.TrimSpace(local.Path) == "" {
			cleanup()
			return nil, func() {}, fmt.Errorf("download attachment %d (%s): empty local path", i+1, resource.Kind)
		}
		attachments = append(attachments, local)
	}
	return attachments, cleanup, nil
}

func attachmentDirName(messageID string) string {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return fmt.Sprintf("message-%d", time.Now().UnixNano())
	}

	var b strings.Builder
	for _, char := range id {
		switch {
		case char >= 'a' && char <= 'z':
			b.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			b.WriteRune(char)
		case char >= '0' && char <= '9':
			b.WriteRune(char)
		case char == '-', char == '_':
			b.WriteRune(char)
		}
		if b.Len() >= 96 {
			break
		}
	}
	if b.Len() == 0 {
		return fmt.Sprintf("message-%d", time.Now().UnixNano())
	}
	return b.String()
}

func buildAttachmentPrompt(text string, attachments []attachment.Local) string {
	if len(attachments) == 0 {
		return text
	}

	text = strings.TrimSpace(text)
	if text == "" {
		text = defaultAttachmentPrompt(attachments)
	}

	var b strings.Builder
	b.WriteString(text)
	b.WriteString("\n\n请处理本轮消息及其附件，不要用历史对话中的其他图片或文件代替本轮附件。\n")
	b.WriteString("附件已保存到本地：\n")

	imageNumber := 0
	for _, item := range attachments {
		switch item.Kind {
		case attachment.KindImage:
			imageNumber++
			fmt.Fprintf(&b, "- [Image %d saved at: %s]\n", imageNumber, item.Path)
		case attachment.KindFile:
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = filepath.Base(item.Path)
			}
			fmt.Fprintf(&b, "- [File %q saved at: %s]\n", name, item.Path)
		}
	}

	for _, item := range attachments {
		if item.Kind != attachment.KindFile || !isTextAttachment(item) {
			continue
		}
		content, truncated, err := readTextPreview(item.Path, maxInlineTextBytes)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = filepath.Base(item.Path)
		}
		fmt.Fprintf(&b, "\n<attachment_text name=%q path=%q>\n", name, item.Path)
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteByte('\n')
		}
		if truncated {
			b.WriteString("... [内容已截断]\n")
		}
		b.WriteString("</attachment_text>\n")
	}
	b.WriteString("请使用可用的文件或图片读取工具分析上述附件。")
	return b.String()
}

func isTextAttachment(item attachment.Local) bool {
	mimeType := strings.ToLower(strings.TrimSpace(strings.SplitN(item.MimeType, ";", 2)[0]))
	if strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "application/yaml" ||
		mimeType == "application/x-yaml" ||
		mimeType == "application/javascript" {
		return true
	}
	switch strings.ToLower(filepath.Ext(item.Path)) {
	case ".txt", ".md", ".markdown", ".json", ".yaml", ".yml", ".toml", ".xml",
		".csv", ".log", ".go", ".js", ".ts", ".tsx", ".jsx", ".py", ".rs",
		".java", ".kt", ".c", ".h", ".cc", ".cpp", ".sh", ".zsh", ".bash",
		".css", ".html", ".sql", ".ini", ".conf", ".env":
		return true
	}
	return false
}

func readTextPreview(path string, limit int64) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return "", false, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	return string(data), truncated, nil
}

func defaultAttachmentPrompt(attachments []attachment.Local) string {
	hasImage := false
	hasFile := false
	for _, item := range attachments {
		switch item.Kind {
		case attachment.KindImage:
			hasImage = true
		case attachment.KindFile:
			hasFile = true
		}
	}
	switch {
	case hasImage && hasFile:
		return "请分析附件中的图片和文件。"
	case hasImage:
		return "请分析附件中的图片。"
	case hasFile:
		return "请分析附件中的文件。"
	default:
		return "请分析附件。"
	}
}
