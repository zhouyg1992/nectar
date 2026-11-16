package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	maxImageBytes         = 25 << 20
	maxFileBytes          = 50 << 20
	maxAudioBytes         = 25 << 20
	maxOutboundMediaBytes = 20 << 20
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

func (a *Adapter) sendMedia(ctx context.Context, reply platform.Reply, media platform.Media) (string, error) {
	rc, err := a.replyContext(reply)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(media.Path)
	if path == "" {
		return "", errors.New("dingtalk media path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat dingtalk media: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", errors.New("dingtalk media must be a non-empty regular file")
	}
	if info.Size() > maxOutboundMediaBytes {
		return "", fmt.Errorf("dingtalk file is %d bytes; limit is %d", info.Size(), maxOutboundMediaBytes)
	}

	name := strings.TrimSpace(media.Name)
	if name == "" {
		name = filepath.Base(path)
	}
	name = filepath.Base(name)
	if name == "" || name == "." {
		return "", errors.New("dingtalk media name is empty")
	}

	asImage := strings.EqualFold(media.Type, "image") && dingtalkImageName(name)
	uploadType := "file"
	if asImage {
		uploadType = "image"
	}
	mediaID, err := a.uploadMedia(ctx, path, name, uploadType)
	if err != nil {
		return "", err
	}

	msgKey := "sampleFile"
	param := map[string]string{
		"mediaId":  mediaID,
		"fileName": name,
		"fileType": dingtalkFileType(name),
	}
	if asImage {
		msgKey = "sampleImageMsg"
		param = map[string]string{"photoURL": mediaID}
	}
	return a.sendProactiveMedia(ctx, rc, msgKey, param)
}

func (a *Adapter) uploadMedia(ctx context.Context, path, name, mediaType string) (string, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("get access token for dingtalk upload: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open dingtalk media: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("type", mediaType); err != nil {
		return "", fmt.Errorf("encode dingtalk upload type: %w", err)
	}
	part, err := writer.CreateFormFile("media", name)
	if err != nil {
		return "", fmt.Errorf("encode dingtalk upload file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("read dingtalk media: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("encode dingtalk upload body: %w", err)
	}

	query := url.Values{
		"access_token": {token},
		"type":         {mediaType},
	}
	endpoint := a.oapiBaseURL + "/media/upload?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("create dingtalk upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload dingtalk media: %w", redactDingTalkURLError(err))
	}
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, 256<<10)
	if err != nil {
		return "", fmt.Errorf("read dingtalk upload response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dingtalk upload rejected: status=%d body=%s", resp.StatusCode, truncateBytes(data, 512))
	}

	var result struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode dingtalk upload response: %w", err)
	}
	if result.ErrCode != nil && *result.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk upload rejected: errcode=%d msg=%s", *result.ErrCode, result.ErrMsg)
	}
	if strings.TrimSpace(result.MediaID) == "" {
		return "", fmt.Errorf("dingtalk upload response has no media_id: %s", truncateBytes(data, 512))
	}
	return result.MediaID, nil
}

func (a *Adapter) sendProactiveMedia(
	ctx context.Context,
	rc replyContext,
	msgKey string,
	param map[string]string,
) (string, error) {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return "", err
	}
	msgParam, err := json.Marshal(param)
	if err != nil {
		return "", fmt.Errorf("encode dingtalk media message: %w", err)
	}

	payload := map[string]any{
		"robotCode": rc.robotCode,
		"msgKey":    msgKey,
		"msgParam":  string(msgParam),
	}
	path := "/v1.0/robot/oToMessages/batchSend"
	if rc.isGroup {
		if rc.conversationID == "" {
			return "", errors.New("dingtalk group media send requires conversationId")
		}
		path = "/v1.0/robot/groupMessages/send"
		payload["openConversationId"] = rc.conversationID
	} else {
		if rc.senderStaffID == "" {
			return "", errors.New("dingtalk direct media send requires senderStaffId")
		}
		payload["userIds"] = []string{rc.senderStaffID}
	}

	body, status, err := a.requestJSON(ctx, http.MethodPost, a.apiURL(path), payload, token, nil)
	if err != nil {
		return "", fmt.Errorf("send dingtalk media: %w", err)
	}
	if err := validateSendResponse(status, body, true); err != nil {
		return "", err
	}
	var result struct {
		ProcessQueryKey string `json:"processQueryKey"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil
	}
	return result.ProcessQueryKey, nil
}

func dingtalkImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp":
		return true
	default:
		return false
	}
}

func dingtalkFileType(name string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "" || !safeExtension.MatchString("."+ext) {
		return "file"
	}
	return ext
}

func redactDingTalkURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	return err
}
