package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestDownloadImage(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/robot/messageFiles/download":
			if r.Method != http.MethodPost {
				t.Fatalf("download url method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"downloadUrl":"` + server.URL + `/file"}`))
		case "/file":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)

	local, err := adapter.Download(context.Background(), attachment.Resource{
		Kind: attachment.KindImage,
		Key:  "code-1",
		Name: "screenshot.png",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if local.Kind != attachment.KindImage || local.MimeType != "image/png" {
		t.Fatalf("local = %#v", local)
	}
	if filepath.Ext(local.Path) != ".png" {
		t.Fatalf("path = %q", local.Path)
	}
	data, err := os.ReadFile(local.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(png) {
		t.Fatalf("downloaded data = %v", data)
	}
}

func TestSendOutboundMedia(t *testing.T) {
	var (
		uploadTypes []string
		uploadNames []string
		uploadData  [][]byte
		sent        []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media/upload":
			if r.Method != http.MethodPost {
				t.Fatalf("upload method = %s", r.Method)
			}
			if err := r.ParseMultipartForm(20 << 20); err != nil {
				t.Fatal(err)
			}
			uploadTypes = append(uploadTypes, r.URL.Query().Get("type"))
			file, header, err := r.FormFile("media")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Fatal(err)
			}
			uploadNames = append(uploadNames, header.Filename)
			uploadData = append(uploadData, data)
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","media_id":"media-1"}`))
		case "/v1.0/robot/groupMessages/send":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			sent = append(sent, payload)
			_, _ = w.Write([]byte(`{"processQueryKey":"pq"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.oapiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\nchart"), 0o600); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc := replyContext{
		conversationID: "cid-1",
		isGroup:        true,
		robotCode:      "robot-test",
	}
	_, err := adapter.Send(context.Background(), platform.Reply{
		ChatID:       "cid-1",
		ChatType:     "group",
		ReplyContext: rc,
		Media: []platform.Media{
			{Type: "image", Path: imagePath, Name: "chart.png"},
			{Type: "file", Path: filePath, Name: "report.pdf"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(uploadTypes) != 2 || uploadTypes[0] != "image" || uploadTypes[1] != "file" {
		t.Fatalf("upload types = %#v", uploadTypes)
	}
	if len(uploadNames) != 2 || uploadNames[0] != "chart.png" || uploadNames[1] != "report.pdf" {
		t.Fatalf("upload names = %#v", uploadNames)
	}
	if len(uploadData) != 2 || string(uploadData[0]) != "\x89PNG\r\n\x1a\nchart" || string(uploadData[1]) != "pdf" {
		t.Fatalf("upload data = %#v", uploadData)
	}
	if len(sent) != 2 {
		t.Fatalf("sent = %#v", sent)
	}
	if sent[0]["msgKey"] != "sampleImageMsg" || sent[1]["msgKey"] != "sampleFile" {
		t.Fatalf("sent msgKeys = %#v", sent)
	}
	var imageParam map[string]string
	if err := json.Unmarshal([]byte(sent[0]["msgParam"].(string)), &imageParam); err != nil {
		t.Fatal(err)
	}
	if imageParam["photoURL"] != "media-1" {
		t.Fatalf("image param = %#v", imageParam)
	}
	var fileParam map[string]string
	if err := json.Unmarshal([]byte(sent[1]["msgParam"].(string)), &fileParam); err != nil {
		t.Fatal(err)
	}
	if fileParam["mediaId"] != "media-1" || fileParam["fileName"] != "report.pdf" || fileParam["fileType"] != "pdf" {
		t.Fatalf("file param = %#v", fileParam)
	}
}
