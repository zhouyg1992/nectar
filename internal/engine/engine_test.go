package engine

import (
	"slices"
	"testing"
)

func TestEnvWithReplacesAndAppends(t *testing.T) {
	got := EnvWith([]string{"A=1", "KEEP=x"}, map[string]string{"A": "2", "B": "3"})
	if !slices.Contains(got, "A=2") || !slices.Contains(got, "B=3") || !slices.Contains(got, "KEEP=x") {
		t.Fatalf("env = %v", got)
	}
	if slices.Contains(got, "A=1") {
		t.Fatalf("old env survived: %v", got)
	}
}
