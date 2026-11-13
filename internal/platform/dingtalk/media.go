package dingtalk

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

const (
	maxImageBytes = 25 << 20
	maxFileBytes  = 50 << 20
	maxAudioBytes = 25 << 20
)

var safeExtension = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

func (a *Adapter) Download(ctx context.Context, resource attachment.Resource, dir string) (attachment.Local, error) {
	code := strings.TrimSpace(resource.Key)
	if code == "" {
		return attachment.Local{}, errors.New("dingtalk attachment key is empty")
	}
	downloadURL, err := a.downloadURL(ctx, code)
	if err != nil {
		return attachment.Local{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("create download request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("download dingtalk attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return attachment.Local{}, fmt.Errorf("download dingtalk attachment: status=%d", resp.StatusCode)
	}
	limit := maxAttachmentBytes(resource.Kind)
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return attachment.Local{}, fmt.Errorf("read dingtalk attachment: %w", err)
	}
	if int64(len(data)) > limit {
		return attachment.Local{}, fmt.Errorf("dingtalk attachment exceeds %d bytes", limit)
	}
	if len(data) == 0 {
		return attachment.Local{}, errors.New("dingtalk attachment is empty")
	}

	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return attachment.Local{}, fmt.Errorf("create attachment dir: %w", err)
	}
	name := strings.TrimSpace(resource.Name)
	if name != "" {
		name = filepath.Base(name)
	}
	ext := attachmentExtension(name, mimeType)
	file, err := os.CreateTemp(dir, "attachment-*"+ext)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("create attachment file: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return attachment.Local{}, fmt.Errorf("set attachment permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return attachment.Local{}, fmt.Errorf("write attachment file: %w", err)
	}
	return attachment.Local{
		Kind:     resource.Kind,
		Path:     file.Name(),
		Name:     name,
		MimeType: mimeType,
	}, nil
}

func (a *Adapter) downloadURL(ctx context.Context, downloadCode string) (string, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return "", err
	}
	var resp struct {
		DownloadURL string `json:"downloadUrl"`
	}
	body, status, err := a.requestJSON(ctx, http.MethodPost, a.apiURL("/v1.0/robot/messageFiles/download"), map[string]string{
		"downloadCode": downloadCode,
		"robotCode":    a.robotCode,
	}, token, &resp)
	if err != nil {
		return "", fmt.Errorf("request download url: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("download url rejected: status=%d body=%s", status, truncateBytes(body, 512))
	}
	if strings.TrimSpace(resp.DownloadURL) == "" {
		return "", errors.New("download url response is empty")
	}
	return resp.DownloadURL, nil
}

func maxAttachmentBytes(kind attachment.Kind) int64 {
	switch kind {
	case attachment.KindImage:
		return maxImageBytes
	case attachment.KindAudio, attachment.KindVideo:
		return maxAudioBytes
	default:
		return maxFileBytes
	}
}

func attachmentExtension(name, mimeType string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if safeExtension.MatchString(ext) {
		return ext
	}
	switch strings.ToLower(strings.Split(mimeType, ";")[0]) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/amr":
		return ".amr"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}
