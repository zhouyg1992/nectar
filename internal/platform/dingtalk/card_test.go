package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func decodeA2UI(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, ok := payload["a2uiMessages"].([]string)
	if !ok {
		t.Fatalf("a2uiMessages type = %T", payload["a2uiMessages"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, message := range raw {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(message), &decoded); err != nil {
			t.Fatalf("decode a2ui message %q: %v", message, err)
		}
		out = append(out, decoded)
	}
	return out
}

func TestBuildCreatePayloadGroup(t *testing.T) {
	adapter := testAdapter(t)
	rc := replyContext{
		conversationID: "cid-1",
		senderStaffID:  "staff-1",
		isGroup:        true,
		robotCode:      "robot-test",
	}
	payload := adapter.buildCreatePayload(rc, "track-1", "surface-1", "title", "body", "footer", "blue", "PROCESSING")

	if payload["openSpaceId"] != "dtv1.card//im_group.cid-1" {
		t.Fatalf("openSpaceId = %v", payload["openSpaceId"])
	}
	if payload["flowStatus"] != "PROCESSING" {
		t.Fatalf("flowStatus = %v", payload["flowStatus"])
	}
	messages := decodeA2UI(t, payload)
	if len(messages) != 3 {
		t.Fatalf("messages = %d", len(messages))
	}
	if _, ok := messages[0]["createSurface"]; !ok {
		t.Fatalf("first message = %#v", messages[0])
	}
	if _, ok := messages[1]["updateDataModel"]; !ok {
		t.Fatalf("second message = %#v", messages[1])
	}
	if _, ok := messages[2]["updateComponents"]; !ok {
		t.Fatalf("third message = %#v", messages[2])
	}
	if _, ok := payload["imGroupOpenDeliverModel"]; !ok {
		t.Fatalf("missing group deliver model: %#v", payload)
	}
	if _, ok := payload["a2uiAnnotations"]; !ok {
		t.Fatalf("missing annotations: %#v", payload)
	}
}

func TestBuildCreatePayloadDirect(t *testing.T) {
	adapter := testAdapter(t)
	rc := replyContext{
		conversationID: "cid-dm",
		senderStaffID:  "staff-1",
		isGroup:        false,
		robotCode:      "robot-test",
	}
	payload := adapter.buildCreatePayload(rc, "track-2", "surface-2", "title", "body", "", "green", "FINISH")

	if payload["openSpaceId"] != "dtv1.card//im_robot.staff-1" {
		t.Fatalf("openSpaceId = %v", payload["openSpaceId"])
	}
	deliver, ok := payload["imRobotOpenDeliverModel"].(map[string]any)
	if !ok {
		t.Fatalf("missing direct deliver model: %#v", payload)
	}
	recipients, ok := deliver["recipients"].([]string)
	if !ok || len(recipients) != 1 || recipients[0] != "staff-1" {
		t.Fatalf("recipients = %#v", deliver["recipients"])
	}
}

func TestBuildUpdatePayloadFinal(t *testing.T) {
	state := &cardState{
		outTrackID: "track-1",
		surfaceID:  "surface-1",
		lastTheme:  "blue",
	}
	payload := buildUpdatePayload(state, "track-1-1", "done", "answer", "footer", "green", "FINISH")
	if payload["flowStatus"] != "FINISH" {
		t.Fatalf("flowStatus = %v", payload["flowStatus"])
	}
	messages := decodeA2UI(t, payload)
	if len(messages) != 3 {
		t.Fatalf("messages = %d", len(messages))
	}
	last := messages[len(messages)-1]
	update, ok := last["updateComponents"].(map[string]any)
	if !ok {
		t.Fatalf("last message = %#v", last)
	}
	components, ok := update["components"].([]any)
	if !ok || len(components) != 6 {
		t.Fatalf("components = %#v", update["components"])
	}
	header := components[1].(map[string]any)
	if header["theme"] != "green" {
		t.Fatalf("theme = %v", header["theme"])
	}
	root := components[0].(map[string]any)
	children := root["children"].([]any)
	if len(children) != 4 || children[2] != "divider" || children[3] != "footer" {
		t.Fatalf("root children = %#v", children)
	}
	divider := components[4].(map[string]any)
	if divider["component"] != "Divider" || divider["axis"] != "horizontal" {
		t.Fatalf("divider = %#v", divider)
	}
}

func TestBuildCreatePayloadWithoutFooterOmitsDivider(t *testing.T) {
	adapter := testAdapter(t)
	rc := replyContext{
		conversationID: "cid-1",
		senderStaffID:  "staff-1",
		isGroup:        true,
		robotCode:      "robot-test",
	}
	payload := adapter.buildCreatePayload(rc, "track-1", "surface-1", "title", "body", "", "green", "FINISH")
	messages := decodeA2UI(t, payload)
	update := messages[2]["updateComponents"].(map[string]any)
	components := update["components"].([]any)
	for _, raw := range components {
		component := raw.(map[string]any)
		if component["id"] == "divider" {
			t.Fatalf("unexpected divider without footer: %#v", components)
		}
	}
}

func TestBuildUpdatePayloadIncludesComponentsForProgress(t *testing.T) {
	state := &cardState{
		outTrackID: "track-1",
		surfaceID:  "surface-1",
		lastTheme:  "blue",
	}
	payload := buildUpdatePayload(state, "track-1-1", "working", "progress", "", "blue", "INPUTTING")
	messages := decodeA2UI(t, payload)
	last := messages[len(messages)-1]
	if _, ok := last["updateComponents"]; !ok {
		t.Fatalf("progress update has no components: %#v", last)
	}
}

func TestBuildCreatePayloadRendersTable(t *testing.T) {
	adapter := testAdapter(t)
	rc := replyContext{
		conversationID: "cid-1",
		senderStaffID:  "staff-1",
		isGroup:        true,
		robotCode:      "robot-test",
	}
	body := "| name | value |\n|---|---|\n| alpha | 1 |\n| beta | 2 |"
	payload := adapter.buildCreatePayload(rc, "track-1", "surface-1", "title", body, "", "green", "FINISH")
	messages := decodeA2UI(t, payload)
	update := messages[2]["updateComponents"].(map[string]any)
	components := update["components"].([]any)

	var table map[string]any
	for _, raw := range components {
		component := raw.(map[string]any)
		if component["component"] == "Table" {
			table = component
			break
		}
	}
	if table == nil {
		t.Fatalf("no Table component: %#v", components)
	}
	data := table["data"].(map[string]any)
	meta := data["meta"].([]any)
	rows := data["data"].([]any)
	if len(meta) != 2 || len(rows) != 2 {
		t.Fatalf("table data = %#v", data)
	}
	if meta[0].(map[string]any)["colName"] != "name" {
		t.Fatalf("meta = %#v", meta)
	}
	if rows[0].(map[string]any)["c0"] != "alpha" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestCardStatusMapping(t *testing.T) {
	cases := []struct {
		status platform.CardStatus
		theme  string
		create string
		update string
	}{
		{platform.CardThinking, "blue", "PROCESSING", "INPUTTING"},
		{platform.CardInfo, "blue", "FINISH", "FINISH"},
		{platform.CardComplete, "green", "FINISH", "FINISH"},
		{platform.CardError, "red", "ERROR", "ERROR"},
	}
	for _, tc := range cases {
		if got := cardTheme(tc.status); got != tc.theme {
			t.Fatalf("theme(%s) = %q", tc.status, got)
		}
		if got := cardFlowStatus(tc.status, true); got != tc.create {
			t.Fatalf("create flow(%s) = %q", tc.status, got)
		}
		if got := cardFlowStatus(tc.status, false); got != tc.update {
			t.Fatalf("update flow(%s) = %q", tc.status, got)
		}
	}
}

func TestCardDegradedCapabilities(t *testing.T) {
	adapter := testAdapter(t)
	if !adapter.Capabilities().Card {
		t.Fatal("card should be enabled by default")
	}
	adapter.markCardDegraded("403 access.denied")
	caps := adapter.Capabilities()
	if caps.Card || caps.Streaming {
		t.Fatalf("capabilities = %#v", caps)
	}
}

func TestSendAndUpdateCard(t *testing.T) {
	var createPayload map[string]any
	var updatePayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/card/ai/instances/createAndDeliver":
			if r.Method != http.MethodPost {
				t.Fatalf("create method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&createPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{"outTrackId":"track","deliverResults":[{"success":true}]}}`))
		case "/v1.0/card/ai/instances/update":
			if r.Method != http.MethodPut {
				t.Fatalf("update method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&updatePayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{"outTrackId":"track","flowStatus":"FINISH"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)
	rc := replyContext{
		conversationID: "cid-1",
		senderStaffID:  "staff-1",
		isGroup:        true,
		robotCode:      "robot-test",
	}
	messageID, err := adapter.Send(context.Background(), platform.Reply{
		ChatID:       "cid-1",
		ReplyContext: rc,
		Card: &platform.Card{
			Status: platform.CardThinking,
			Title:  "working",
			Body:   "thinking",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected outTrackId")
	}
	if createPayload["openSpaceId"] != "dtv1.card//im_group.cid-1" {
		t.Fatalf("create payload = %#v", createPayload)
	}

	err = adapter.Update(context.Background(), messageID, platform.Reply{
		Card: &platform.Card{
			Status: platform.CardComplete,
			Title:  "done",
			Body:   "answer",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatePayload["flowStatus"] != "FINISH" {
		t.Fatalf("update payload = %#v", updatePayload)
	}
}

func TestSendCardDegradesOnPermissionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/card/ai/instances/createAndDeliver" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"access.denied","message":"AICard.Instance.Write required"}`))
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)
	messageID, err := adapter.Send(context.Background(), platform.Reply{
		ChatID: "cid-1",
		ReplyContext: replyContext{
			conversationID: "cid-1",
			senderStaffID:  "staff-1",
			isGroup:        true,
			robotCode:      "robot-test",
		},
		Card: &platform.Card{Status: platform.CardThinking, Body: "thinking"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if messageID != "" {
		t.Fatalf("fallback should not return a card ID, got %q", messageID)
	}
	if adapter.Capabilities().Card {
		t.Fatal("card should be degraded after permission error")
	}
}

func TestSendOneShotCardFallsBackToText(t *testing.T) {
	var fallback map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/card/ai/instances/createAndDeliver":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"access.denied","message":"AICard.Instance.Write required"}`))
		case "/v1.0/robot/groupMessages/send":
			if err := json.NewDecoder(r.Body).Decode(&fallback); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"processQueryKey":"pq"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)
	messageID, err := adapter.Send(context.Background(), platform.Reply{
		ChatID: "cid-1",
		ReplyContext: replyContext{
			conversationID: "cid-1",
			senderStaffID:  "staff-1",
			isGroup:        true,
			robotCode:      "robot-test",
		},
		Card: &platform.Card{Status: platform.CardInfo, Title: "Help", Body: "commands"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if messageID != "" {
		t.Fatalf("fallback should not return a card ID, got %q", messageID)
	}
	if fallback["msgKey"] != "sampleMarkdown" {
		t.Fatalf("fallback payload = %#v", fallback)
	}
	msgParam, _ := fallback["msgParam"].(string)
	if !strings.Contains(msgParam, "commands") {
		t.Fatalf("fallback msgParam = %q", msgParam)
	}
}
