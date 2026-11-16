package service

import "testing"

func TestMatchesAnyFilter(t *testing.T) {
	line := `time=now level=INFO msg="bot starting" bot=test_bot`
	if !matchesAnyFilter(line, []string{"bot=test_bot"}) {
		t.Fatal("expected bot filter to match")
	}
	if matchesAnyFilter(line, []string{"bot=other"}) {
		t.Fatal("unexpected filter match")
	}
}
