package feishu

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const maxCardBodyBytes = 24 << 10

// renderCard converts the platform-neutral card into Feishu card JSON.
func renderCard(card *platform.Card) (string, error) {
	template := "blue"
	switch card.Status {
	case platform.CardInfo:
		template = "wathet"
	case platform.CardComplete:
		template = "green"
	case platform.CardError:
		template = "red"
	}

	elements := buildCardElements(truncateCardBody(card.Body))
	if card.Footer != "" {
		elements = append(elements, map[string]any{
			"tag":        "markdown",
			"content":    `<font color="grey">` + escapeAngles(card.Footer) + `</font>`,
			"text_align": "right",
		})
	}

	payload := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"width_mode":     "default",
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": card.Title},
		},
		"body": map[string]any{
			"direction":        "vertical",
			"vertical_spacing": "4px",
			"elements":         elements,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func truncateCardBody(body string) string {
	if len(body) <= maxCardBodyBytes {
		return body
	}
	cut := maxCardBodyBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	// Cut on a line boundary when possible so the tail is not a half row.
	if idx := strings.LastIndexByte(body[:cut], '\n'); idx > maxCardBodyBytes/2 {
		cut = idx
	}
	head := body[:cut]
	// Never leave a fenced block open: Feishu would render the truncation
	// marker (and anything appended later) as code.
	if strings.Count(head, "```")%2 == 1 {
		head += "\n```"
	}
	return head + "\n\n…（内容过长，已截断）"
}
