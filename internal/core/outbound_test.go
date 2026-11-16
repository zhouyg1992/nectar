package core

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectOutboundMediaSkipsUnsafeEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), []byte("\x89PNG\r\n\x1a\nchart"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	large, err := os.Create(filepath.Join(dir, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(maxOutboundMediaBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}

	media := collectOutboundMedia(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(media) != 1 {
		t.Fatalf("media = %#v", media)
	}
	if media[0].Name != "chart.png" || media[0].Type != "image" || media[0].MimeType != "image/png" {
		t.Fatalf("media = %#v", media[0])
	}
}

func TestOutboundFileName(t *testing.T) {
	cases := map[string]string{
		"chart.png":     "chart.png",
		".hidden":       "",
		"..":            "",
		"bad\nname.txt": "badname.txt",
	}
	for input, want := range cases {
		if got := outboundFileName(input); got != want {
			t.Fatalf("outboundFileName(%q) = %q, want %q", input, got, want)
		}
	}
}
