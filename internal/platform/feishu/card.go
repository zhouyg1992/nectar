package feishu

import (
	"encoding/json"
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

	elements := make([]any, 0, 2)
	body := truncateCardBody(card.Body)
	if body != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": body,
		})
	}
	if card.Footer != "" {
		elements = append(elements, map[string]any{
			"tag": "note",
			"elements": []any{
				map[string]string{"tag": "plain_text", "content": card.Footer},
			},
		})
	}

	payload := map[string]any{
		"config": map[string]bool{"wide_screen_mode": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]string{"tag": "plain_text", "content": card.Title},
		},
		"elements": elements,
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
	return body[:cut] + "\n\n…（内容过长，已截断）"
}
