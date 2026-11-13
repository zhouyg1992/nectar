package service

import (
	"strings"
	"testing"
)

func TestRenderSystemd(t *testing.T) {
	t.Setenv("CODEX_HOME", "/home/test/.codex")
	m := &Manager{
		Home:       "/home/test",
		BinaryPath: "/home/test/.local/bin/nectar",
		ConfigPath: "/home/test/.nectar/config.toml",
		Label:      systemdUnit,
	}
	content, err := renderSystemd(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ExecStart="/home/test/.local/bin/nectar" -config "/home/test/.nectar/config.toml"`,
		"WorkingDirectory=/home/test",
		`Environment="HOME=/home/test"`,
		`Environment="CODEX_HOME=/home/test/.codex"`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered unit missing %q:\n%s", want, content)
		}
	}
}

func TestSystemdEscapePercent(t *testing.T) {
	if got := systemdEscape("/tmp/100%"); got != "/tmp/100%%" {
		t.Fatalf("systemdEscape() = %q", got)
	}
}
