package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunPrintsVersionFromDeps(t *testing.T) {
	out := captureRunOutput(t, func() error {
		return Run([]string{"-version"}, Deps{
			Version: "v9.9.9",
			RunBot: func(string) error {
				t.Fatal("unexpected bot start")
				return nil
			},
		})
	})
	if got := strings.TrimSpace(out); got != "nectar v9.9.9" {
		t.Fatalf("version output = %q", got)
	}
}

func TestRunPassesConfigToBot(t *testing.T) {
	var got string
	err := Run([]string{"-config", "/tmp/nectar.toml"}, Deps{
		RunBot: func(configPath string) error {
			got = configPath
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/nectar.toml" {
		t.Fatalf("config path = %q", got)
	}
}

func TestRunDispatchesHelp(t *testing.T) {
	out := captureRunOutput(t, func() error {
		return Run([]string{"help"}, Deps{})
	})
	if !strings.Contains(out, "nectar service <install|uninstall|start|stop|restart|status|logs>") {
		t.Fatalf("help output = %q", out)
	}
}

func captureRunOutput(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	runErr := fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	return string(data)
}
