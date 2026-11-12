package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveContextWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	data := `{
		"model": "magpie/deepseek/deepseek-flash",
		"provider": {
			"magpie": {
				"models": {
					"deepseek/deepseek-flash": {
						"limit": {"context": 1048576}
					}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG", path)

	if got := ResolveContextWindow("", 0); got != 1048576 {
		t.Fatalf("default model context = %d", got)
	}
	if got := ResolveContextWindow("magpie/deepseek/deepseek-flash#low", 0); got != 1048576 {
		t.Fatalf("variant model context = %d", got)
	}
	if got := ResolveContextWindow("deepseek/deepseek-flash", 0); got != 1048576 {
		t.Fatalf("model without provider context = %d", got)
	}
	if got := ResolveContextWindow("magpie/unknown", 200000); got != 200000 {
		t.Fatalf("override context = %d", got)
	}
	if got := ResolveContextWindow("magpie/unknown", 0); got != 0 {
		t.Fatalf("unknown context = %d", got)
	}
}
