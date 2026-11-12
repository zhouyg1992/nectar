package service

import (
	"strings"
	"testing"
)

func TestRenderLaunchd(t *testing.T) {
	m := &Manager{
		Home:       "/Users/test",
		BinaryPath: "/Users/test/.local/bin/nectar",
		ConfigPath: "/Users/test/.nectar/config.toml",
		Label:      launchdLabel,
	}
	content, err := renderLaunchd(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>com.zhouyg1992.nectar</string>",
		"<string>/Users/test/.local/bin/nectar</string>",
		"<string>/Users/test/.nectar/config.toml</string>",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
		"/Users/test/Library/Logs/nectar.out.log",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered plist missing %q:\n%s", want, content)
		}
	}
}

func TestRenderLaunchdEscapesXML(t *testing.T) {
	m := &Manager{
		Home:       "/Users/a&b",
		BinaryPath: "/Users/a&b/nectar",
		ConfigPath: "/Users/a&b/config.toml",
		Label:      launchdLabel,
	}
	content, err := renderLaunchd(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "a&amp;b") {
		t.Fatalf("plist did not escape XML: %s", content)
	}
}
