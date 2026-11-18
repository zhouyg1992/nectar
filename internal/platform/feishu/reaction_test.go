package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestAddCompletionReaction(t *testing.T) {
	var reactionBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok", "tenant_access_token": "test-token", "expire": 7200,
			})
		case "/open-apis/im/v1/messages/om_test/reactions":
			if err := json.NewDecoder(r.Body).Decode(&reactionBody); err != nil {
				t.Errorf("decode reaction body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok", "data": map[string]any{"reaction_id": "reaction-1"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := lark.NewClient(
		"app-id",
		"app-secret",
		lark.WithOpenBaseUrl(server.URL),
		lark.WithOAuthBaseUrl(server.URL),
		lark.WithHttpClient(server.Client()),
		lark.WithEnableTokenCache(false),
	)
	adapter := &Adapter{client: client}
	if err := adapter.AddCompletionReaction(context.Background(), "om_test", platform.Reply{}); err != nil {
		t.Fatal(err)
	}

	reactionType, _ := reactionBody["reaction_type"].(map[string]any)
	if reactionType["emoji_type"] != completionReactionEmoji {
		t.Fatalf("reaction body = %#v", reactionBody)
	}
}
