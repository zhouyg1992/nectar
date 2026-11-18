package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/update"
)

func TestUpdateProgressPrinterNonInteractive(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress-*")
	if err != nil {
		t.Fatal(err)
	}
	printer := newUpdateProgressPrinter(file)
	printer.Report(update.Progress{Asset: "nectar.tar.gz", Downloaded: 0, Total: 100})
	printer.Report(update.Progress{Asset: "nectar.tar.gz", Downloaded: 50, Total: 100})
	printer.Report(update.Progress{Asset: "nectar.tar.gz", Downloaded: 100, Total: 100, Done: true})
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	for _, want := range []string{"  0%", " 50%", "100%"} {
		if !strings.Contains(output, want) {
			t.Fatalf("progress output %q does not contain %q", output, want)
		}
	}
}

func TestResolveUpdateSource(t *testing.T) {
	t.Setenv("NECTAR_UPDATE_MIRROR", "https://env-mirror")
	t.Setenv("NECTAR_UPDATE_DOWNLOAD_BASE", "https://env-base")
	t.Setenv("NECTAR_UPDATE_LATEST_URL", "https://env-latest")
	t.Setenv("NECTAR_UPDATE_LATEST_API_URL", "https://env-api")

	mirror, base, latest, api := resolveUpdateSource("", "", "", "")
	if mirror != "https://env-mirror" || base != "https://env-base" ||
		latest != "https://env-latest" || api != "https://env-api" {
		t.Fatalf("resolved env = %q, %q, %q, %q", mirror, base, latest, api)
	}

	mirror, base, latest, api = resolveUpdateSource(
		"https://flag-mirror", "https://flag-base", "https://flag-latest", "https://flag-api",
	)
	if mirror != "https://flag-mirror" || base != "https://flag-base" ||
		latest != "https://flag-latest" || api != "https://flag-api" {
		t.Fatalf("resolved flags = %q, %q, %q, %q", mirror, base, latest, api)
	}
}
