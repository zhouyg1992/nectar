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

func TestExtractRuleCandidatesProjectAndLimit(t *testing.T) {
	got := ExtractRuleCandidates("这个项目使用 Go，不要引入 CGO。部署在 scripts/deploy.sh。", 1)
	if len(got) != 1 {
		t.Fatalf("candidates = %#v", got)
	}
	if got[0].Kind != KindProject {
		t.Fatalf("kind = %s, want project", got[0].Kind)
	}
}

func TestExtractRuleCandidatesSkipsQuestionsAndTransient(t *testing.T) {
	for _, text := range []string{
		"部署约定是什么？",
		"这次先帮我跑一下测试",
		"/remember 以后默认用中文",
	} {
		if got := ExtractRuleCandidates(text, 2); len(got) != 0 {
			t.Fatalf("%q produced %#v", text, got)
		}
	}
}
