package feishu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestRenderCardStatusAndBody(t *testing.T) {
	cases := []struct {
		status   platform.CardStatus
		template string
	}{
		{platform.CardThinking, "blue"},
		{platform.CardInfo, "wathet"},
		{platform.CardComplete, "green"},
		{platform.CardError, "red"},
	}
	for _, tc := range cases {
		raw, err := renderCard(&platform.Card{
			Status: tc.status,
			Title:  "title",
			Body:   "**answer**",
			Footer: "codex · 1.0s",
		})
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Header struct {
				Template string `json:"template"`
				Title    struct {
					Content string `json:"content"`
				} `json:"title"`
			} `json:"header"`
			Elements []map[string]any `json:"elements"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Header.Template != tc.template {
			t.Fatalf("%s template = %q, want %q", tc.status, payload.Header.Template, tc.template)
		}
		if payload.Header.Title.Content != "title" {
			t.Fatalf("%s title = %q", tc.status, payload.Header.Title.Content)
		}
		if len(payload.Elements) != 2 {
			t.Fatalf("%s elements = %d, want 2", tc.status, len(payload.Elements))
		}
		if payload.Elements[0]["tag"] != "markdown" {
			t.Fatalf("%s body tag = %v, want markdown", tc.status, payload.Elements[0]["tag"])
		}
	}
}

func TestTruncateCardBody(t *testing.T) {
	body := strings.Repeat("a", maxCardBodyBytes+100)
	got := truncateCardBody(body)
	if len(got) <= maxCardBodyBytes {
		t.Fatalf("truncated body length = %d, want > %d", len(got), maxCardBodyBytes)
	}
	if !strings.Contains(got, "已截断") {
		t.Fatal("missing truncation marker")
	}
}
