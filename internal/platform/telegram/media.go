package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhouyg1992/nectar/internal/attachment"
)

const maxDownloadBytes = 20 << 20

var safeExtension = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

type getFileResult struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

func (a *Adapter) Download(
	ctx context.Context,
	resource attachment.Resource,
	dir string,
) (attachment.Local, error) {
	fileID := strings.TrimSpace(resource.Key)
	if fileID == "" {
		return attachment.Local{}, errors.New("telegram attachment key is empty")
	}

	var file getFileResult
	if err := a.call(ctx, "getFile", map[string]string{"file_id": fileID}, &file); err != nil {
		return attachment.Local{}, fmt.Errorf("get telegram file: %w", err)
	}
	if strings.TrimSpace(file.FilePath) == "" {
		return attachment.Local{}, errors.New("telegram getFile returned an empty file_path")
	}
	if file.FileSize > maxDownloadBytes {
		return attachment.Local{}, fmt.Errorf(
			"telegram attachment is %d bytes; Bot API download limit is %d bytes",
			file.FileSize, maxDownloadBytes,
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.fileURL(file.FilePath), nil)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("create telegram file request: %w", a.redactError(err))
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("download telegram file: %w", a.redactError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return attachment.Local{}, fmt.Errorf("download telegram file: status=%d", resp.StatusCode)
	}
	if resp.ContentLength > maxDownloadBytes {
		return attachment.Local{}, fmt.Errorf(
			"telegram attachment is %d bytes; Bot API download limit is %d bytes",
			resp.ContentLength, maxDownloadBytes,
		)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return attachment.Local{}, fmt.Errorf("read telegram attachment: %w", err)
	}
	if len(data) > maxDownloadBytes {
		return attachment.Local{}, fmt.Errorf(
			"telegram attachment exceeds %d bytes",
			maxDownloadBytes,
		)
	}
	if len(data) == 0 {
		return attachment.Local{}, errors.New("telegram attachment is empty")
	}

	mimeType := strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if mimeType == "application/octet-stream" {
		if guess := mimeFromExtension(file.FilePath); guess != "" {
			mimeType = guess
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return attachment.Local{}, fmt.Errorf("create attachment dir: %w", err)
	}
	name := sanitizeName(resource.Name)
	if name == "" {
		name = sanitizeName(filepath.Base(file.FilePath))
	}
	if name == "" {
		name = "attachment"
	}
	ext := localExtension(name, mimeType)
	fileHandle, err := os.CreateTemp(dir, "telegram-*"+ext)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("create attachment file: %w", err)
	}
	defer fileHandle.Close()
	if err := fileHandle.Chmod(0o600); err != nil {
		return attachment.Local{}, fmt.Errorf("set attachment permissions: %w", err)
	}
	if _, err := fileHandle.Write(data); err != nil {
		return attachment.Local{}, fmt.Errorf("write attachment file: %w", err)
	}

	return attachment.Local{
		Kind:     resource.Kind,
		Path:     fileHandle.Name(),
		Name:     name,
		MimeType: mimeType,
	}, nil
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Base(value)
	if value == "." || value == string(filepath.Separator) {
		return ""
	}
	return value
}

func localExtension(name, mimeType string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if safeExtension.MatchString(ext) {
		return ext
	}
	ext = extensionFromMime(mimeType)
	if ext != "" {
		return ext
	}
	return ""
}

func mimeFromExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".mp4":
		return "video/mp4"
	case ".pdf":
		return "application/pdf"
	default:
		return ""
	}
}
