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
			Schema string `json:"schema"`
			Header struct {
				Template string `json:"template"`
				Title    struct {
					Content string `json:"content"`
				} `json:"title"`
			} `json:"header"`
			Body struct {
				Elements []map[string]any `json:"elements"`
			} `json:"body"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Schema != "2.0" {
			t.Fatalf("%s schema = %q", tc.status, payload.Schema)
		}
		if payload.Header.Template != tc.template {
			t.Fatalf("%s template = %q, want %q", tc.status, payload.Header.Template, tc.template)
		}
		if payload.Header.Title.Content != "title" {
			t.Fatalf("%s title = %q", tc.status, payload.Header.Title.Content)
		}
		if len(payload.Body.Elements) != 3 {
			t.Fatalf("%s elements = %d, want 3", tc.status, len(payload.Body.Elements))
		}
		if payload.Body.Elements[0]["tag"] != "markdown" ||
			payload.Body.Elements[1]["tag"] != "hr" ||
			payload.Body.Elements[2]["tag"] != "markdown" {
			t.Fatalf("%s body tags = %v, %v, %v", tc.status,
				payload.Body.Elements[0]["tag"], payload.Body.Elements[1]["tag"], payload.Body.Elements[2]["tag"])
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

func TestRenderCardIncludesNativeTable(t *testing.T) {
	raw, err := renderCard(&platform.Card{
		Status: platform.CardInfo,
		Title:  "table",
		Body:   "intro\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\noutro",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Schema string `json:"schema"`
		Body   struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Schema != "2.0" {
		t.Fatalf("schema = %q", payload.Schema)
	}
	if len(payload.Body.Elements) != 3 {
		t.Fatalf("elements = %#v", payload.Body.Elements)
	}
	if payload.Body.Elements[0]["tag"] != "markdown" || payload.Body.Elements[1]["tag"] != "table" || payload.Body.Elements[2]["tag"] != "markdown" {
		t.Fatalf("element tags = %v, %v, %v", payload.Body.Elements[0]["tag"], payload.Body.Elements[1]["tag"], payload.Body.Elements[2]["tag"])
	}
}
