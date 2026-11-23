package memory

import "testing"

func TestExtractRuleCandidatesExplicitPreference(t *testing.T) {
	got := ExtractRuleCandidates("以后默认用中文回复", 2)
	if len(got) != 1 {
		t.Fatalf("candidates = %#v", got)
	}
	if got[0].Kind != KindProfile || got[0].Confidence < 0.8 {
		t.Fatalf("candidate = %#v", got[0])
	}
	if got[0].Content != "以后默认用中文回复" {
		t.Fatalf("content = %q", got[0].Content)
	}
}

func TestExtractRuleCandidatesLimit(t *testing.T) {
	got := ExtractRuleCandidates("记住用中文。以后默认走 pnpm。", 1)
	if len(got) != 1 {
		t.Fatalf("candidates = %#v", got)
	}
	if got[0].Content != "记住用中文" {
		t.Fatalf("content = %q", got[0].Content)
	}
}

func TestExtractRuleCandidatesStaysProfileKind(t *testing.T) {
	got := ExtractRuleCandidates("以后这个项目默认用 pnpm", 1)
	if len(got) != 1 {
		t.Fatalf("candidates = %#v", got)
	}
	if got[0].Kind != KindProfile {
		t.Fatalf("kind = %s, want profile", got[0].Kind)
	}
}

func TestExtractRuleCandidatesSkipsQuestions(t *testing.T) {
	for _, text := range []string{
		"部署约定是什么？",
		"cpu吃紧，我家SWAP是不是不起作用",
		"这个接口有没有问题",
		"能不能帮我看下这个报错",
		"以后是不是都用中文？",
	} {
		if got := ExtractRuleCandidates(text, 2); len(got) != 0 {
			t.Fatalf("%q produced %#v", text, got)
		}
	}
}

func TestExtractRuleCandidatesSkipsBareCorrections(t *testing.T) {
	for _, text := range []string{
		"头像是正常男性头像，不是无头像、黑头像、卡通头像或非男性头像",
		"AppsFlyer 归因不是 NONE 或 organic",
		"这个项目使用 Go，不要引入 CGO。部署在 scripts/deploy.sh。",
	} {
		if got := ExtractRuleCandidates(text, 2); len(got) != 0 {
			t.Fatalf("%q produced %#v", text, got)
		}
	}
}

func TestExtractRuleCandidatesSkipsTaskChecklists(t *testing.T) {
	text := "1. 头像是正常男性头像，不是无头像\n2. AppsFlyer 归因不是 NONE 或 organic\n3. 以后默认用中文回复"
	if got := ExtractRuleCandidates(text, 2); len(got) != 0 {
		t.Fatalf("checklist produced %#v", got)
	}
}

func TestExtractRuleCandidatesSkipsTaskRequests(t *testing.T) {
	text := "帮我检查这个页面，必须过滤 NONE 和 organic"
	if got := ExtractRuleCandidates(text, 2); len(got) != 0 {
		t.Fatalf("task request produced %#v", got)
	}
}

func TestExtractRuleCandidatesSkipsTransient(t *testing.T) {
	for _, text := range []string{
		"这次先帮我跑一下测试",
		"/remember 以后默认用中文",
	} {
		if got := ExtractRuleCandidates(text, 2); len(got) != 0 {
			t.Fatalf("%q produced %#v", text, got)
		}
	}
}
