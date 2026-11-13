package update

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.2.3-rc.1", "v1.2.3", -1},
		{"v1.2.3", "v1.2.3-rc.1", 1},
	}
	for _, tc := range cases {
		got, err := compareVersions(tc.left, tc.right)
		if err != nil {
			t.Fatalf("compareVersions(%q, %q): %v", tc.left, tc.right, err)
		}
		if got != tc.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"1.2.3":     "v1.2.3",
		"v1.2.3":    "v1.2.3",
		"  v1.2.3 ": "v1.2.3",
	}
	for input, want := range cases {
		if got := normalizeTag(input); got != want {
			t.Fatalf("normalizeTag(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseVersionRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "v1", "v1.2", "v1.2.x", "v1.2.3.4"} {
		if _, err := parseVersion(input); err == nil {
			t.Fatalf("parseVersion(%q) should fail", input)
		}
	}
}
