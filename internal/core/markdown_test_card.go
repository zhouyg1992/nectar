package core

import "github.com/zhouyg1992/nectar/internal/platform"

const markdownTestCardBody = "# Markdown 语法测试\n" +
	"## 二级标题\n" +
	"### 三级标题\n" +
	"#### 四级标题\n" +
	"##### 五级标题\n" +
	"###### 六级标题\n\n" +
	"**粗体**、*斜体*、***粗斜体***、~~删除线~~、`行内代码`。\n\n" +
	"[Nectar 链接](https://github.com/zhouyg1992/nectar)\n\n" +
	"> 引用第一行\n" +
	"> 引用第二行\n\n" +
	"- 无序列表 1\n" +
	"- 无序列表 2\n" +
	"  - 嵌套列表 2.1\n" +
	"  - 嵌套列表 2.2\n\n" +
	"1. 有序列表 1\n" +
	"2. 有序列表 2\n" +
	"   1. 嵌套有序 2.1\n" +
	"   2. 嵌套有序 2.2\n\n" +
	"- [x] 已完成任务\n" +
	"- [ ] 未完成任务\n\n" +
	"---\n\n" +
	"| 列 A | 列 B | 列 C |\n" +
	"|:---|:---:|---:|\n" +
	"| 左对齐 | 居中 | 右对齐 |\n" +
	"| `代码` | **粗体** | [链接](https://example.com) |\n" +
	"| 长文本 | 第二行<br>换行 | 结束 |\n\n" +
	"```go\n" +
	"package main\n\n" +
	"import \"fmt\"\n\n" +
	"func main() {\n" +
	"\tfmt.Println(\"hello\")\n" +
	"}\n" +
	"```\n\n" +
	"```json\n" +
	"{\"name\":\"nectar\",\"dingtalk\":true,\"markdown\":\"test\"}\n" +
	"```\n\n" +
	"HTML 应被转义：<div>raw html</div>\n\n" +
	"行内链接和分隔线测试完成。"

func markdownTestCard() *platform.Card {
	return &platform.Card{
		Status: platform.CardInfo,
		Title:  "🧪 Markdown Syntax Test",
		Body:   markdownTestCardBody,
		Footer: "Nectar · Markdown rendering test",
	}
}
