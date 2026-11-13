package dingtalk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
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
