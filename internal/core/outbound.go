package core

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	outboundDirName          = ".nectar/outbox"
	maxOutboundMediaFiles    = 10
	maxOutboundMediaBytes    = 50 << 20
	maxOutboundMediaTotal    = 100 << 20
	maxOutboundMediaNameSize = 200
)

func outboundDir(workdir string) string {
	return filepath.Join(workdir, filepath.FromSlash(outboundDirName))
}

func ensureOutboundDir(workdir string) (string, error) {
	dir := outboundDir(workdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create outbound media dir: %w", err)
	}
	return dir, nil
}

// collectOutboundMedia returns regular files the agent placed in the outbox.
// Invalid or oversized files are skipped so one bad artifact does not block the
// rest of the reply.
func collectOutboundMedia(dir string, logger *slog.Logger) []platform.Media {
	if logger == nil {
		logger = slog.Default()
	}
	var (
		media      []platform.Media
		totalBytes int64
	)
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			logger.Warn("inspect outbound media failed", "path", path, "error", walkErr)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			logger.Warn("skip outbound symlink", "path", path)
			return nil
		}
		name := outboundFileName(entry.Name())
		if name == "" {
			logger.Warn("skip outbound file with unsupported name", "path", path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			logger.Warn("inspect outbound file failed", "path", path, "error", err)
			return nil
		}
		if !info.Mode().IsRegular() {
			logger.Warn("skip outbound non-regular file", "path", path)
			return nil
		}
		if info.Size() == 0 {
			logger.Warn("skip empty outbound file", "path", path)
			return nil
		}
		if info.Size() > maxOutboundMediaBytes {
			logger.Warn("skip outbound file larger than limit",
				"path", path, "size", info.Size(), "limit", maxOutboundMediaBytes)
			return nil
		}
		if totalBytes+info.Size() > maxOutboundMediaTotal {
			logger.Warn("outbound media total size limit reached",
				"path", path, "total", totalBytes, "limit", maxOutboundMediaTotal)
			return filepath.SkipAll
		}
		if len(media) >= maxOutboundMediaFiles {
			logger.Warn("outbound media file count limit reached",
				"path", path, "limit", maxOutboundMediaFiles)
			return filepath.SkipAll
		}

		mimeType := detectOutboundMime(path)
		mediaType := "file"
		if strings.HasPrefix(mimeType, "image/") {
			mediaType = "image"
		}
		media = append(media, platform.Media{
			Type:     mediaType,
			Path:     path,
			Name:     name,
			MimeType: mimeType,
		})
		totalBytes += info.Size()
		return nil
	})
	if err != nil {
		logger.Warn("scan outbound media failed", "dir", dir, "error", err)
	}
	return media
}

func detectOutboundMime(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "application/octet-stream"
	}
	detected := strings.TrimSpace(strings.SplitN(http.DetectContentType(header[:n]), ";", 2)[0])
	if detected == "" {
		return "application/octet-stream"
	}
	return detected
}

func outboundFileName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == string(filepath.Separator) || strings.HasPrefix(name, ".") {
		return ""
	}
	name = strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f {
			return -1
		}
		return char
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	runes := []rune(name)
	if len(runes) > maxOutboundMediaNameSize {
		name = string(runes[:maxOutboundMediaNameSize])
	}
	return name
}

func outboundInstruction(workdir string) string {
	return fmt.Sprintf(`Outbound files: if the user should receive an image or file you generated,
copy it into %s before finishing. Merely mentioning its local path does not send
it. Nectar sends up to %d files (max %d MiB each) after the task finishes. Do
not put credentials, secrets, or unrelated workspace files there.`,
		outboundDir(workdir), maxOutboundMediaFiles, maxOutboundMediaBytes>>20)
}
