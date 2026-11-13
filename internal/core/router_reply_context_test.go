package core

import (
	"testing"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestReplyTargetCarriesReplyContext(t *testing.T) {
	msg := platform.Message{
		ChatID:       "cid-1",
		ReplyContext: "platform-private",
	}
	reply := replyTarget(msg)
	if reply.ReplyContext != "platform-private" {
		t.Fatalf("reply context = %#v", reply.ReplyContext)
	}
}
