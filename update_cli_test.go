package main

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
