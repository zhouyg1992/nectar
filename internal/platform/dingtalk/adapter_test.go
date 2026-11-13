package dingtalk

import (
	"testing"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestAllowedScopes(t *testing.T) {
	adapter, err := New(Config{
		ClientID:     "dingtest",
		ClientSecret: "secret",
		AllowedChats: []string{"cid-allowed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.allowed(platform.Message{ChatID: "cid-dm", ChatType: "p2p", UserID: "staff-1"}) {
		t.Fatal("chat allowlist should not block direct messages")
	}
	if adapter.allowed(platform.Message{ChatID: "cid-other", ChatType: "group", UserID: "staff-1"}) {
		t.Fatal("group outside allowlist should be blocked")
	}
	if !adapter.allowed(platform.Message{ChatID: "cid-allowed", ChatType: "group", UserID: "staff-1"}) {
		t.Fatal("allowed group should pass")
	}
}

func TestAllowedUsers(t *testing.T) {
	adapter, err := New(Config{
		ClientID:     "dingtest",
		ClientSecret: "secret",
		AllowedUsers: []string{"staff-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.allowed(platform.Message{ChatID: "cid-1", ChatType: "p2p", UserID: "staff-1"}) {
		t.Fatal("allowed user should pass")
	}
	if adapter.allowed(platform.Message{ChatID: "cid-2", ChatType: "p2p", UserID: "staff-2"}) {
		t.Fatal("other user should be blocked")
	}
	if adapter.allowed(platform.Message{ChatID: "cid-3", ChatType: "p2p", UserID: ""}) {
		t.Fatal("empty user id should be blocked when allowlist is set")
	}
}
