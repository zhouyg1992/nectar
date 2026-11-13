package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestUseProactiveAPI(t *testing.T) {
	if !(replyContext{}).useProactiveAPI() {
		t.Fatal("empty webhook should use proactive API")
	}
	fresh := replyContext{
		sessionWebhook:            "https://example.com",
		sessionWebhookExpiredTime: time.Now().Add(time.Hour).UnixMilli(),
	}
	if fresh.useProactiveAPI() {
		t.Fatal("fresh webhook should be used")
	}
	expiring := replyContext{
		sessionWebhook:            "https://example.com",
		sessionWebhookExpiredTime: time.Now().Add(10 * time.Second).UnixMilli(),
	}
	if !expiring.useProactiveAPI() {
		t.Fatal("webhook near expiry should use proactive API")
	}
}

func TestValidateSendResponse(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		proactive bool
		wantErr   bool
	}{
		{"webhook ok", 200, `{"errcode":0}`, false, false},
		{"webhook missing errcode", 200, `{}`, false, true},
		{"webhook rejected", 200, `{"errcode":400}`, false, true},
		{"proactive ok", 200, `{"processQueryKey":"pq"}`, true, false},
		{"proactive missing key", 200, `{}`, true, true},
		{"proactive invalid recipient", 200, `{"processQueryKey":"pq","invalidStaffIdList":["x"]}`, true, true},
		{"http error", 500, `{"code":"system.busy"}`, false, true},
	}
	for _, tc := range cases {
		err := validateSendResponse(tc.status, []byte(tc.body), tc.proactive)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestSendWebhook(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/webhook" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	adapter := testAdapter(t)
	rc := replyContext{
		sessionWebhook:            server.URL + "/webhook",
		sessionWebhookExpiredTime: time.Now().Add(time.Hour).UnixMilli(),
		conversationID:            "cid-1",
		senderStaffID:             "staff-1",
	}
	_, err := adapter.Send(context.Background(), platform.Reply{
		ChatID:       "cid-1",
		Text:         "hello",
		ReplyContext: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["msgtype"] != "markdown" {
		t.Fatalf("payload = %#v", got)
	}
}

func TestSendProactiveGroup(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/robot/groupMessages/send" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"processQueryKey":"pq"}`))
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.cfg.APIBaseURL = server.URL
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)
	rc := replyContext{
		conversationID: "cid-1",
		isGroup:        true,
		robotCode:      "robot-test",
	}
	_, err := adapter.Send(context.Background(), platform.Reply{
		ChatID:       "cid-1",
		Text:         "hello",
		ReplyContext: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["msgKey"] != "sampleMarkdown" || got["openConversationId"] != "cid-1" {
		t.Fatalf("payload = %#v", got)
	}
	msgParam, ok := got["msgParam"].(string)
	if !ok || msgParam == "" {
		t.Fatalf("msgParam = %#v", got["msgParam"])
	}
}

func TestSendProactiveDirect(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/robot/oToMessages/batchSend" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"processQueryKey":"pq"}`))
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)
	rc := replyContext{
		conversationID: "cid-dm",
		senderStaffID:  "staff-1",
		robotCode:      "robot-test",
	}
	_, err := adapter.Send(context.Background(), platform.Reply{
		ChatID:       "cid-dm",
		Text:         "hello",
		ReplyContext: rc,
	})
	if err != nil {
		t.Fatal(err)
	}
	userIDs, ok := got["userIds"].([]any)
	if !ok || len(userIDs) != 1 || userIDs[0] != "staff-1" {
		t.Fatalf("payload = %#v", got)
	}
}
