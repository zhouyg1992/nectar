package dingtalk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	publicCatalog = "https://dingtalk.com/card/a2ui/catalogs/public/catalog.json"
)

type cardState struct {
	mu sync.Mutex

	outTrackID   string
	surfaceID    string
	lastTitle    string
	lastActivity string
	lastBody     string
	lastFooter   string
	lastTheme    string
	lastFlow     string
	seq          int
	finalized    bool
	finalizedAt  time.Time
}

func (a *Adapter) sendCard(ctx context.Context, reply platform.Reply) (string, error) {
	card := reply.Card
	if card == nil {
		return "", errors.New("dingtalk card reply is empty")
	}
	if !a.cardAvailable() {
		return a.cardFallback(ctx, reply, card, nil)
	}
	rc, err := a.replyContext(reply)
	if err != nil {
		return "", err
	}

	outTrackID := generateOutTrackID()
	surfaceID := outTrackID
	title := cardTitleValue(card.Title, card.Body)
	activity := strings.TrimSpace(card.Activity)
	body := normalizeA2UIMarkdown(card.Body)
	if strings.TrimSpace(body) == "" {
		body = "Gathering results…"
	}
	footer := strings.TrimSpace(card.Footer)
	theme := cardTheme(card.Status)
	final := cardFinal(card.Status)
	flow := cardFlowStatus(card.Status, true)

	payload := a.buildCreatePayload(rc, outTrackID, surfaceID, title, activity, body, footer, theme, flow)
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return "", err
	}
	respBody, status, err := a.requestJSON(
		ctx,
		http.MethodPost,
		a.apiURL("/v1.0/card/ai/instances/createAndDeliver"),
		payload,
		token,
		nil,
	)
	if err != nil {
		return a.cardFallback(ctx, reply, card, err)
	}
	if err := validateCardResponse(status, respBody); err != nil {
		if isCardUnavailableError(err) {
			a.markCardDegraded(err.Error())
		}
		return a.cardFallback(ctx, reply, card, err)
	}

	state := &cardState{
		outTrackID:   outTrackID,
		surfaceID:    surfaceID,
		lastTitle:    title,
		lastActivity: activity,
		lastBody:     body,
		lastFooter:   footer,
		lastTheme:    theme,
		lastFlow:     flow,
		finalized:    final,
	}
	if final {
		state.finalizedAt = time.Now()
	}
	a.registerCard(state)
	return outTrackID, nil
}

// cardFallback avoids leaving a stale "Thinking" message in the chat. For an
// in-progress card the typing reaction is the feedback; one-shot/final cards
// still need a text fallback so their content is not lost.
func (a *Adapter) cardFallback(ctx context.Context, reply platform.Reply, card *platform.Card, cause error) (string, error) {
	if cause != nil {
		slog.Error("dingtalk: card unavailable, falling back", "error", cause)
	}
	if card.Status == platform.CardThinking {
		return "", nil
	}
	return a.sendCardTextFallback(ctx, reply, card)
}

// sendCardTextFallback sends one-shot card content when AI cards are not
// available.
func (a *Adapter) sendCardTextFallback(ctx context.Context, reply platform.Reply, card *platform.Card) (string, error) {
	if card == nil {
		return "", errors.New("dingtalk card reply is empty")
	}
	rc, err := a.replyContext(reply)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(card.Title)
	body := strings.TrimSpace(card.Body)
	switch {
	case text != "" && body != "":
		text += "\n\n" + body
	case text == "":
		text = body
	}
	if text == "" {
		text = "Gathering results…"
	}
	if err := a.sendMarkdown(ctx, rc, text); err != nil {
		return "", fmt.Errorf("send card text fallback: %w", err)
	}
	return "", nil
}

func (a *Adapter) Update(ctx context.Context, messageID string, reply platform.Reply) error {
	if reply.Card == nil {
		return errors.New("dingtalk update requires a card")
	}
	state := a.lookupCard(messageID)
	if state == nil {
		return fmt.Errorf("dingtalk card %q is not tracked", messageID)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.finalized {
		return nil
	}
	if !a.cardAvailable() {
		return errors.New("dingtalk AI card is temporarily degraded")
	}

	card := reply.Card
	title := cardTitleValue(card.Title, card.Body)
	activity := strings.TrimSpace(card.Activity)
	body := normalizeA2UIMarkdown(card.Body)
	if strings.TrimSpace(body) == "" {
		body = "Gathering results…"
	}
	footer := strings.TrimSpace(card.Footer)
	theme := cardTheme(card.Status)
	flow := cardFlowStatus(card.Status, false)
	final := cardFinal(card.Status)

	if title == state.lastTitle && activity == state.lastActivity && body == state.lastBody && footer == state.lastFooter &&
		theme == state.lastTheme && flow == state.lastFlow {
		return nil
	}

	state.seq++
	outUpdateID := fmt.Sprintf("%s-%d", state.outTrackID, state.seq)
	payload := buildUpdatePayload(state, outUpdateID, title, activity, body, footer, theme, flow)
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return err
	}
	respBody, status, err := a.requestJSON(
		ctx,
		http.MethodPut,
		a.apiURL("/v1.0/card/ai/instances/update"),
		payload,
		token,
		nil,
	)
	if err != nil {
		return fmt.Errorf("update dingtalk card: %w", err)
	}
	if err := validateCardResponse(status, respBody); err != nil {
		if isCardUnavailableError(err) {
			a.markCardDegraded(err.Error())
		}
		return fmt.Errorf("update dingtalk card: %w", err)
	}

	state.lastTitle = title
	state.lastActivity = activity
	state.lastBody = body
	state.lastFooter = footer
	state.lastTheme = theme
	state.lastFlow = flow
	if final {
		state.finalized = true
		state.finalizedAt = time.Now()
	}
	return nil
}

func (a *Adapter) buildCreatePayload(rc replyContext, outTrackID, surfaceID, title, activity, body, footer, theme, flow string) map[string]any {
	data := map[string]string{
		"title":    title,
		"activity": activity,
		"footer":   footer,
	}
	blocks := parseCardBlocks(body)
	messages := []string{
		mustJSON(map[string]any{
			"version": "v1.0",
			"createSurface": map[string]any{
				"surfaceId": surfaceID,
				"catalogId": publicCatalog,
			},
		}),
		mustJSON(map[string]any{
			"version": "v1.0",
			"updateDataModel": map[string]any{
				"surfaceId": surfaceID,
				"path":      "/",
				"value":     data,
			},
		}),
		mustJSON(map[string]any{
			"version": "v1.0",
			"updateComponents": map[string]any{
				"surfaceId":  surfaceID,
				"components": cardComponents(theme, blocks, strings.TrimSpace(activity) != "", strings.TrimSpace(footer) != ""),
			},
		}),
	}
	payload := map[string]any{
		"outTrackId":      outTrackID,
		"openSpaceId":     openSpaceID(rc),
		"protocolVersion": "1.0",
		"flowStatus":      flow,
		"a2uiMessages":    messages,
		"a2uiAnnotations": cardAnnotations(surfaceID, blocks),
		"summary":         cardSummary(body, title),
	}
	applySpaceModels(payload, rc, a.robotCode, cardSummary(body, title))
	return payload
}

func cardAnnotations(surfaceID string, blocks []cardBlock) []map[string]any {
	annotations := []map[string]any{
		{"surfaceId": surfaceID, "componentId": "header", "type": "artifact"},
	}
	added := 0
	for i, block := range blocks {
		if block.Table != nil {
			continue
		}
		annotations = append(annotations, map[string]any{
			"surfaceId": surfaceID, "componentId": fmt.Sprintf("block-%d", i), "type": "artifact",
		})
		added++
		if added >= 5 {
			break
		}
	}
	if added == 0 {
		componentID := "body"
		if len(blocks) > 0 {
			componentID = "block-0"
		}
		annotations = append(annotations, map[string]any{
			"surfaceId": surfaceID, "componentId": componentID, "type": "artifact",
		})
	}
	return annotations
}

func buildUpdatePayload(state *cardState, outUpdateID, title, activity, body, footer, theme, flow string) map[string]any {
	messages := []string{
		updateDataModelJSON(state.surfaceID, "/title", title),
		updateDataModelJSON(state.surfaceID, "/activity", activity),
		updateDataModelJSON(state.surfaceID, "/footer", footer),
		mustJSON(map[string]any{
			"version": "v1.0",
			"updateComponents": map[string]any{
				"surfaceId":  state.surfaceID,
				"components": cardComponents(theme, parseCardBlocks(body), strings.TrimSpace(activity) != "", strings.TrimSpace(footer) != ""),
			},
		}),
	}
	return map[string]any{
		"outTrackId":   state.outTrackID,
		"outUpdateId":  outUpdateID,
		"flowStatus":   flow,
		"a2uiMessages": messages,
	}
}

func updateDataModelJSON(surfaceID, path, value string) string {
	return mustJSON(map[string]any{
		"version": "v1.0",
		"updateDataModel": map[string]any{
			"surfaceId": surfaceID,
			"path":      path,
			"value":     value,
		},
	})
}

func cardComponents(theme string, blocks []cardBlock, hasActivity, hasFooter bool) []map[string]any {
	children := make([]string, 0, len(blocks))
	blockComponents := make([]map[string]any, 0, len(blocks))
	for i, block := range blocks {
		id := fmt.Sprintf("block-%d", i)
		children = append(children, id)
		if block.Table != nil {
			blockComponents = append(blockComponents, tableComponent(id, *block.Table))
			continue
		}
		blockComponents = append(blockComponents, map[string]any{
			"id":        id,
			"component": "Markdown",
			"content":   block.Markdown,
		})
	}

	rootChildren := []string{"header"}
	if hasActivity {
		rootChildren = append(rootChildren, "activity")
		if len(blocks) > 0 {
			rootChildren = append(rootChildren, "activity-divider")
		}
	}
	rootChildren = append(rootChildren, "body")
	if hasFooter && len(blocks) > 0 {
		rootChildren = append(rootChildren, "divider")
	}
	rootChildren = append(rootChildren, "footer")

	root := []map[string]any{
		{
			"id":        "root",
			"component": "Column",
			"children":  rootChildren,
			"gap":       8,
			"align":     "stretch",
		},
		{
			"id":        "header",
			"component": "CardHeader",
			"title":     map[string]string{"path": "/title"},
			"theme":     theme,
		},
	}
	if hasActivity {
		root = append(root, map[string]any{
			"id":        "activity",
			"component": "Markdown",
			"content":   map[string]string{"path": "/activity"},
		})
		if len(blocks) > 0 {
			root = append(root, map[string]any{
				"id":        "activity-divider",
				"component": "Divider",
				"axis":      "horizontal",
			})
		}
	}
	root = append(root, map[string]any{
		"id":        "body",
		"component": "Column",
		"children":  children,
		"gap":       8,
		"align":     "stretch",
	})
	if hasFooter && len(blocks) > 0 {
		root = append(root, map[string]any{
			"id":        "divider",
			"component": "Divider",
			"axis":      "horizontal",
		})
	}
	root = append(root, map[string]any{
		"id":        "footer",
		"component": "Text",
		"variant":   "caption",
		"text":      map[string]string{"path": "/footer"},
	})
	return append(root, blockComponents...)
}

func openSpaceID(rc replyContext) string {
	if rc.isGroup {
		return "dtv1.card//im_group." + rc.conversationID
	}
	return "dtv1.card//im_robot." + rc.senderStaffID
}

func applySpaceModels(payload map[string]any, rc replyContext, robotCode, summary string) {
	if rc.isGroup {
		payload["imGroupOpenSpaceModel"] = map[string]any{
			"supportForward": true,
			"lastMessageI18n": map[string]string{
				"ZH_CN": summary,
			},
		}
		payload["imGroupOpenDeliverModel"] = map[string]any{
			"robotCode": robotCode,
		}
		return
	}
	payload["imRobotOpenSpaceModel"] = map[string]any{
		"supportForward": true,
	}
	payload["imRobotOpenDeliverModel"] = map[string]any{
		"robotCode":  robotCode,
		"recipients": []string{rc.senderStaffID},
	}
}

func cardFinal(status platform.CardStatus) bool {
	switch status {
	case platform.CardInfo, platform.CardComplete, platform.CardError:
		return true
	default:
		return false
	}
}

func cardFlowStatus(status platform.CardStatus, creating bool) string {
	switch status {
	case platform.CardComplete, platform.CardInfo:
		return "FINISH"
	case platform.CardError:
		return "ERROR"
	case platform.CardThinking:
		if creating {
			return "PROCESSING"
		}
		return "INPUTTING"
	default:
		return "INPUTTING"
	}
}

func cardTheme(status platform.CardStatus) string {
	switch status {
	case platform.CardComplete:
		return "green"
	case platform.CardError:
		return "red"
	default:
		return "blue"
	}
}

func cardTitleValue(title, body string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return cardTitleFromContent(body)
}

func cardSummary(body, title string) string {
	return summaryText(body, title)
}

func generateOutTrackID() string {
	var random [6]byte
	_, _ = rand.Read(random[:])
	return fmt.Sprintf("nectar-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(random[:]))
}

func mustJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func (a *Adapter) registerCard(state *cardState) {
	a.cardMu.Lock()
	defer a.cardMu.Unlock()
	a.cleanupCardsLocked()
	a.cards[state.outTrackID] = state
}

func (a *Adapter) lookupCard(outTrackID string) *cardState {
	a.cardMu.Lock()
	defer a.cardMu.Unlock()
	return a.cards[outTrackID]
}

func (a *Adapter) cleanupCardsLocked() {
	cutoff := time.Now().Add(-time.Hour)
	for id, state := range a.cards {
		if state.finalized && !state.finalizedAt.IsZero() && state.finalizedAt.Before(cutoff) {
			delete(a.cards, id)
		}
	}
}

func validateCardResponse(status int, body []byte) error {
	var result struct {
		Success *bool  `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Result  struct {
			OutTrackID     string `json:"outTrackId"`
			DeliverResults []struct {
				Success  bool   `json:"success"`
				ErrorMsg string `json:"errorMsg"`
			} `json:"deliverResults"`
		} `json:"result"`
	}
	decodeErr := json.Unmarshal(body, &result)
	if status != http.StatusOK {
		return fmt.Errorf("status=%d code=%q body=%s", status, result.Code, truncateBytes(body, 512))
	}
	if decodeErr != nil {
		return fmt.Errorf("decode card response: %w", decodeErr)
	}
	if result.Success != nil && !*result.Success {
		return fmt.Errorf("card request failed: code=%q message=%q", result.Code, result.Message)
	}
	if result.Code != "" {
		return fmt.Errorf("card request failed: code=%q message=%q", result.Code, result.Message)
	}
	for _, deliver := range result.Result.DeliverResults {
		if !deliver.Success {
			return fmt.Errorf("card delivery failed: %s", deliver.ErrorMsg)
		}
	}
	return nil
}

func isCardUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"access.denied",
		"aicard.instance.write",
		"featuredisabled",
		"permission",
		"403",
		"openspaceidinvalid",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
