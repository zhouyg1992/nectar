package memory

import "testing"

func TestSimilarContentMergesWordingVariants(t *testing.T) {
	for _, pair := range [][2]string{
		{"以后默认用中文回复", "以后默认用中文回复。"},
		{"以后默认用中文回复", "以后默认用中文"},
		{"AppsFlyer 归因不是 NONE 或 organic", "AppsFlyer 归因不是 NONE 或 organic。"},
	} {
		if !SimilarContent(pair[0], pair[1]) {
			t.Fatalf("%q and %q should be similar", pair[0], pair[1])
		}
	}
}

func TestSimilarContentKeepsDistinctMemories(t *testing.T) {
	for _, pair := range [][2]string{
		{"以后默认用中文回复", "以后默认走 pnpm 安装依赖"},
		{"短句", "短句二"},
		{"AppsFlyer 归因不是 NONE 或 organic", "头像必须是正常男性头像"},
	} {
		if SimilarContent(pair[0], pair[1]) {
			t.Fatalf("%q and %q should not be similar", pair[0], pair[1])
		}
	}
}
