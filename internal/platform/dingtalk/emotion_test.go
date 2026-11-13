package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartTypingRepliesAndRecallsEmotion(t *testing.T) {
	var paths []string
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.apiBaseURL = server.URL
	adapter.accessToken = "token"
	adapter.tokenExpiry = time.Now().Add(time.Hour)
	stop := adapter.StartTyping(context.Background(), replyContext{
		conversationID: "cid-1",
		messageID:      "msg-1",
		robotCode:      "robot-test",
	})
	stop()

	if len(paths) != 2 {
		t.Fatalf("paths = %#v", paths)
	}
	if paths[0] != "/v1.0/robot/emotion/reply" || paths[1] != "/v1.0/robot/emotion/recall" {
		t.Fatalf("paths = %#v", paths)
	}
	if bodies[0]["openMsgId"] != "msg-1" || bodies[0]["openConversationId"] != "cid-1" {
		t.Fatalf("emotion body = %#v", bodies[0])
	}
}
