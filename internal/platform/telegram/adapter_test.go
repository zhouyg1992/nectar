package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

type capturedCall struct {
	Method  string
	Payload map[string]any
	Path    string
}

type fakeBotAPI struct {
	mu      sync.Mutex
	calls   []capturedCall
	handle  func(method string, payload map[string]any) any
	rawFile string
}

func (f *fakeBotAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/file/bot") {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(f.rawFile))
		return
	}

	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	var payload map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}

	f.mu.Lock()
	f.calls = append(f.calls, capturedCall{Method: method, Payload: payload, Path: r.URL.Path})
	f.mu.Unlock()

	result := f.handle(method, payload)
	response := map[string]any{"ok": true, "result": result}
	if value, ok := result.(apiFailure); ok {
		response = map[string]any{
			"ok":          false,
			"error_code":  value.Code,
			"description": value.Description,
		}
		if value.RetryAfter > 0 {
			response["parameters"] = map[string]any{"retry_after": value.RetryAfter}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (f *fakeBotAPI) callsFor(method string) []capturedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []capturedCall
	for _, call := range f.calls {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

type apiFailure struct {
	Code        int
	Description string
	RetryAfter  int
}

func newFakeAdapter(t *testing.T, handler func(method string, payload map[string]any) any) (*Adapter, *fakeBotAPI) {
	t.Helper()
	fake := &fakeBotAPI{handle: handler}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	adapter, err := New(Config{
		BotToken:   "123:test-token",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter, fake
}

func TestParsePrivateText(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	adapter.botID = 7
	adapter.botUsername = "nectar_bot"

	msg, groupID, err := adapter.parseMessage(&tgMessage{
		MessageID: 11,
		From:      &tgUser{ID: 42, FirstName: "Alice"},
		Chat:      tgChat{ID: 42, Type: "private"},
		Date:      time.Now().Unix(),
		Text:      "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "" || msg.Channel != "telegram" || msg.ChatType != "p2p" {
		t.Fatalf("message = %#v group=%q", msg, groupID)
	}
	if msg.MessageID != "11" || msg.ChatID != "42" || msg.UserID != "42" || msg.Text != "hello" {
		t.Fatalf("message = %#v", msg)
	}
	rc, ok := msg.ReplyContext.(replyContext)
	if !ok || rc.chatID != 42 || rc.userID != 42 {
		t.Fatalf("reply context = %#v", msg.ReplyContext)
	}
}

func TestParseGroupMentionAndThread(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	adapter.botID = 7
	adapter.botUsername = "nectar_bot"

	msg, _, err := adapter.parseMessage(&tgMessage{
		MessageID:       12,
		MessageThreadID: 99,
		From:            &tgUser{ID: 42, FirstName: "Alice"},
		Chat:            tgChat{ID: -1001, Type: "supergroup"},
		Date:            time.Now().Unix(),
		Text:            "/status@nectar_bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ChatType != "group" || msg.ThreadID != "99" || msg.Text != "/status" {
		t.Fatalf("message = %#v", msg)
	}
}

func TestParseStartCommandBecomesHelp(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _, err := adapter.parseMessage(&tgMessage{
		MessageID: 15,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: 42, Type: "private"},
		Date:      time.Now().Unix(),
		Text:      "/start",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "/help" {
		t.Fatalf("text = %q, want /help", msg.Text)
	}
}

func TestParseGroupRejectsUnmentionedMessage(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	adapter.botID = 7
	adapter.botUsername = "nectar_bot"

	_, _, err = adapter.parseMessage(&tgMessage{
		MessageID: 12,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: -1001, Type: "group"},
		Date:      time.Now().Unix(),
		Text:      "hello everyone",
	})
	if err == nil {
		t.Fatal("expected unmentioned group message to be rejected")
	}
}

func TestParseGroupRejectsCommandForAnotherBot(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	adapter.botID = 7
	adapter.botUsername = "nectar_bot"

	_, _, err = adapter.parseMessage(&tgMessage{
		MessageID: 12,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: -1001, Type: "group"},
		Date:      time.Now().Unix(),
		Text:      "/help@other_bot",
	})
	if err == nil {
		t.Fatal("expected command for another bot to be rejected")
	}
}

func TestParseReplyToBotInGroup(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	adapter.botID = 7
	adapter.botUsername = "nectar_bot"

	msg, _, err := adapter.parseMessage(&tgMessage{
		MessageID: 12,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: -1001, Type: "group"},
		Date:      time.Now().Unix(),
		Text:      "continue",
		ReplyToMessage: &tgMessage{
			From: &tgUser{ID: 7, IsBot: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "continue" {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestParsePhotoAndDocument(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}

	msg, _, err := adapter.parseMessage(&tgMessage{
		MessageID: 13,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: 42, Type: "private"},
		Date:      time.Now().Unix(),
		Caption:   "look",
		Photo:     []tgPhoto{{FileID: "small"}, {FileID: "large"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "look" || len(msg.Attachments) != 1 || msg.Attachments[0].Key != "large" ||
		msg.Attachments[0].Kind != attachment.KindImage {
		t.Fatalf("photo message = %#v", msg)
	}

	msg, _, err = adapter.parseMessage(&tgMessage{
		MessageID: 14,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: 42, Type: "private"},
		Date:      time.Now().Unix(),
		Document:  &tgDocument{FileID: "doc", FileName: "report.pdf", MimeType: "application/pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Name != "report.pdf" ||
		msg.Attachments[0].Kind != attachment.KindFile {
		t.Fatalf("document message = %#v", msg)
	}
}

func TestAllowedScopes(t *testing.T) {
	adapter, err := New(Config{
		BotToken:     "123:test",
		AllowedUsers: []string{"42"},
		AllowedChats: []string{"-1001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.allowed(platform.Message{ChatType: "p2p", ChatID: "42", UserID: "42"}) {
		t.Fatal("allowed user should pass")
	}
	if adapter.allowed(platform.Message{ChatType: "p2p", ChatID: "42", UserID: "43"}) {
		t.Fatal("unknown user should be blocked")
	}
	if !adapter.allowed(platform.Message{ChatType: "group", ChatID: "-1001", UserID: "42"}) {
		t.Fatal("allowed group should pass")
	}
	if adapter.allowed(platform.Message{ChatType: "group", ChatID: "-1002", UserID: "42"}) {
		t.Fatal("unknown group should be blocked")
	}
}

func TestDispatchDeduplicatesPerChat(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan platform.Message, 2)
	adapter.handler = func(msg platform.Message) {
		received <- msg
	}
	now := time.Now().Unix()
	first := update{UpdateID: 1, Message: &tgMessage{
		MessageID: 5,
		From:      &tgUser{ID: 42},
		Chat:      tgChat{ID: 42, Type: "private"},
		Date:      now,
		Text:      "first",
	}}
	second := update{UpdateID: 2, Message: &tgMessage{
		MessageID: 5,
		From:      &tgUser{ID: 43},
		Chat:      tgChat{ID: 43, Type: "private"},
		Date:      now,
		Text:      "second",
	}}

	adapter.dispatchUpdate(first)
	adapter.dispatchUpdate(second)
	adapter.dispatchUpdate(first)

	for _, want := range []string{"first", "second"} {
		select {
		case msg := <-received:
			if msg.Text != want {
				t.Fatalf("message = %q, want %q", msg.Text, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("did not receive %q", want)
		}
	}
	select {
	case msg := <-received:
		t.Fatalf("duplicate message was delivered: %#v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSendChunksLongMessage(t *testing.T) {
	var nextID atomic.Int64
	adapter, fake := newFakeAdapter(t, func(method string, _ map[string]any) any {
		if method == "sendMessage" {
			return map[string]any{"message_id": nextID.Add(1)}
		}
		return nil
	})

	source := "a < b & c\n" + strings.Repeat("中文内容\n", 1500)
	_, err := adapter.Send(context.Background(), platform.Reply{
		ChatID:   "42",
		ThreadID: "7",
		ReplyTo:  "55",
		Text:     source,
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := fake.callsFor("sendMessage")
	if len(calls) < 2 {
		t.Fatalf("send calls = %d, want multiple", len(calls))
	}
	for _, call := range calls {
		text, _ := call.Payload["text"].(string)
		if utf16Len(text) > maxTelegramUTF16 {
			t.Fatalf("message length = %d", utf16Len(text))
		}
		if strings.Contains(text, "a < b") {
			t.Fatalf("html was not escaped: %q", text)
		}
	}
	first := calls[0].Payload
	if first["parse_mode"] != "HTML" {
		t.Fatalf("parse_mode = %#v", first["parse_mode"])
	}
	if first["message_thread_id"] != float64(7) {
		t.Fatalf("message_thread_id = %#v", first["message_thread_id"])
	}
	reply, _ := first["reply_parameters"].(map[string]any)
	if reply["message_id"] != float64(55) {
		t.Fatalf("reply_parameters = %#v", reply)
	}
}

func TestSendFallsBackWhenHTMLRejected(t *testing.T) {
	var calls atomic.Int32
	adapter, fake := newFakeAdapter(t, func(method string, payload map[string]any) any {
		if method != "sendMessage" {
			return nil
		}
		if payload["parse_mode"] == "HTML" {
			return apiFailure{Code: 400, Description: "Bad Request: can't parse entities"}
		}
		calls.Add(1)
		return map[string]any{"message_id": 1}
	})

	if _, err := adapter.Send(context.Background(), platform.Reply{
		ChatID: "42",
		Text:   "**hello**",
	}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("plain fallback calls = %d", calls.Load())
	}
	all := fake.callsFor("sendMessage")
	if len(all) != 2 {
		t.Fatalf("send calls = %d, want 2", len(all))
	}
	if all[1].Payload["parse_mode"] != nil {
		t.Fatalf("fallback parse_mode = %#v", all[1].Payload["parse_mode"])
	}
}

func TestUpdateUsesChatContext(t *testing.T) {
	adapter, fake := newFakeAdapter(t, func(method string, _ map[string]any) any {
		if method == "editMessageText" {
			return true
		}
		return nil
	})

	err := adapter.Update(context.Background(), "99", platform.Reply{
		ChatID:   "42",
		ThreadID: "7",
		Card: &platform.Card{
			Status: platform.CardComplete,
			Title:  "Done",
			Body:   "hello",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := fake.callsFor("editMessageText")
	if len(calls) != 1 {
		t.Fatalf("edit calls = %d", len(calls))
	}
	payload := calls[0].Payload
	if payload["chat_id"] != float64(42) || payload["message_id"] != float64(99) {
		t.Fatalf("payload = %#v", payload)
	}
	text, _ := payload["text"].(string)
	if !strings.Contains(text, "<b>Done</b>") || !strings.Contains(text, "hello") {
		t.Fatalf("text = %q", text)
	}
}

func TestUpdateIgnoresNotModified(t *testing.T) {
	adapter, _ := newFakeAdapter(t, func(method string, _ map[string]any) any {
		if method == "editMessageText" {
			return apiFailure{Code: 400, Description: "Bad Request: message is not modified"}
		}
		return nil
	})
	if err := adapter.Update(context.Background(), "99", platform.Reply{
		ChatID: "42",
		Card:   &platform.Card{Status: platform.CardComplete, Title: "Done", Body: "hello"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSplitsLongFinalAnswer(t *testing.T) {
	adapter, fake := newFakeAdapter(t, func(method string, _ map[string]any) any {
		switch method {
		case "editMessageText":
			return true
		case "sendMessage":
			return map[string]any{"message_id": 1}
		default:
			return nil
		}
	})

	err := adapter.Update(context.Background(), "99", platform.Reply{
		ChatID: "42",
		Card: &platform.Card{
			Status: platform.CardComplete,
			Title:  "Done",
			Body:   strings.Repeat("中文内容", 2500),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.callsFor("editMessageText")) != 1 {
		t.Fatalf("edit calls = %d", len(fake.callsFor("editMessageText")))
	}
	if len(fake.callsFor("sendMessage")) == 0 {
		t.Fatal("long final answer did not send continuation messages")
	}
	for _, call := range fake.callsFor("sendMessage") {
		text, _ := call.Payload["text"].(string)
		if utf16Len(text) > maxTelegramUTF16 {
			t.Fatalf("continuation length = %d", utf16Len(text))
		}
	}
}

func TestDownloadAttachment(t *testing.T) {
	adapter, fake := newFakeAdapter(t, func(method string, _ map[string]any) any {
		if method == "getFile" {
			return map[string]any{"file_path": "documents/note.txt", "file_size": 11}
		}
		return nil
	})
	fake.rawFile = "hello world"

	local, err := adapter.Download(context.Background(), attachment.Resource{
		Kind: attachment.KindFile,
		Key:  "file-id",
		Name: "note.txt",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if local.Name != "note.txt" || local.MimeType != "text/plain" || local.Path == "" {
		t.Fatalf("local = %#v", local)
	}
	data, err := os.ReadFile(local.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("downloaded data = %q", data)
	}
}

func TestStartTypingSendsChatAction(t *testing.T) {
	adapter, fake := newFakeAdapter(t, func(method string, _ map[string]any) any {
		if method == "sendChatAction" {
			return true
		}
		return nil
	})

	stop := adapter.StartTyping(context.Background(), replyContext{chatID: 42, threadID: 7})
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fake.callsFor("sendChatAction")) > 0 {
			payload := fake.callsFor("sendChatAction")[0].Payload
			if payload["chat_id"] != float64(42) || payload["action"] != "typing" {
				t.Fatalf("payload = %#v", payload)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("typing action was not sent")
}

func TestStartLongPolling(t *testing.T) {
	var polls atomic.Int32
	received := make(chan platform.Message, 1)
	fake := &fakeBotAPI{}
	fake.handle = func(method string, _ map[string]any) any {
		switch method {
		case "getMe":
			return map[string]any{"id": 7, "username": "nectar_bot", "is_bot": true}
		case "getUpdates":
			if polls.Add(1) == 1 {
				return []any{map[string]any{
					"update_id": 100,
					"message": map[string]any{
						"message_id": 11,
						"from":       map[string]any{"id": 42, "first_name": "Alice"},
						"chat":       map[string]any{"id": 42, "type": "private"},
						"date":       time.Now().Unix(),
						"text":       "hello",
					},
				}}
			}
			return []any{}
		default:
			return nil
		}
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	adapter, err := New(Config{
		BotToken:   "123:test",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- adapter.Start(ctx, func(msg platform.Message) {
			received <- msg
		})
	}()

	select {
	case msg := <-received:
		if msg.Text != "hello" || msg.ChatID != "42" {
			t.Fatalf("message = %#v", msg)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for message")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not stop after cancellation")
	}
}

func TestMediaGroupCoalesces(t *testing.T) {
	adapter, err := New(Config{BotToken: "123:test"})
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan platform.Message, 1)
	adapter.handler = func(msg platform.Message) {
		received <- msg
	}

	adapter.queueMessage(platform.Message{
		MessageID:   "1",
		ChatID:      "42",
		Text:        "first",
		Attachments: []attachment.Resource{{Kind: attachment.KindImage, Key: "a"}},
	}, "group-1")
	adapter.queueMessage(platform.Message{
		MessageID:   "2",
		ChatID:      "42",
		Text:        "second",
		Attachments: []attachment.Resource{{Kind: attachment.KindImage, Key: "b"}},
	}, "group-1")

	select {
	case msg := <-received:
		if len(msg.Attachments) != 2 || msg.Text != "first\nsecond" {
			t.Fatalf("message = %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for coalesced media group")
	}
}
