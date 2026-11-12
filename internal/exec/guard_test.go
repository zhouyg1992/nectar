package execguard

import "testing"

func TestTruncateKeepsUTF8(t *testing.T) {
	input := "你好世界"
	got := Truncate(input, 4)
	if got != "你" {
		t.Fatalf("Truncate() = %q, want %q", got, "你")
	}
}

func TestLimitedBuffer(t *testing.T) {
	buf := NewLimitedBuffer(4)
	if _, err := buf.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "abcd" || !buf.Truncated() {
		t.Fatalf("buffer = %q truncated=%v", buf.String(), buf.Truncated())
	}
}
