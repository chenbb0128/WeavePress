package aiwriting

import (
	"strings"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestRenderGenerationAppendsTraceableSourceAndEscapesText(t *testing.T) {
	assetID := uint64(7)
	article := workspace.Article{
		Title:        `<原题 & "测试">`,
		Author:       "不应输出的作者",
		SourceName:   "原公众号 & 站点",
		CanonicalURL: "https://example.com/article?id=1&from=feed",
	}
	blocks := []GeneratedBlock{
		{Type: "heading", Level: 3, Text: `<script>alert("x")</script>章节`},
		{Type: "paragraph", Text: "重新组织的正文 & 补充"},
		{Type: "quote", Text: `<b>逐字引用</b>`},
		{Type: "list", Items: []string{"第一项", `<img src=x onerror=alert(1)>`}},
		{Type: "image", AssetID: &assetID, Alt: `配图" onerror="alert(1)`},
	}

	got := RenderGeneration(article, blocks)

	for _, want := range []string{"<h3>", "章节", "重新组织的正文", "<blockquote>", "<ul>", "<figure>", "参考来源", "原题", "原公众号", `href="https://example.com/article?id=1&amp;from=feed"`, `data-weavepress-asset-id="7"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("html missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"<script", `<img src="`, ` onerror="`, "不应输出的作者"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("html contains %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "&lt;b&gt;逐字引用&lt;/b&gt;") {
		t.Fatalf("quote text was not escaped: %s", got)
	}
}

func TestRenderGenerationDoesNotLinkUnsafeCanonicalURL(t *testing.T) {
	article := workspace.Article{Title: "原题", SourceName: "来源", CanonicalURL: `javascript:alert("x")`}

	got := RenderGeneration(article, []GeneratedBlock{{Type: "paragraph", Text: "正文"}})

	if strings.Contains(got, "href=") || strings.Contains(got, "<a") {
		t.Fatalf("unsafe URL became a link: %s", got)
	}
	if !strings.Contains(got, "javascript:alert") {
		t.Fatalf("plain-text source URL missing: %s", got)
	}
}

func TestRenderGenerationOnlyEmitsAllowedGeneratedMarkup(t *testing.T) {
	article := workspace.Article{Title: "原题"}
	blocks := []GeneratedBlock{
		{Type: "heading", Level: 1, Text: "非法标题"},
		{Type: "html", Text: "<div>非法 HTML</div>"},
	}

	got := RenderGeneration(article, blocks)

	if strings.Contains(got, "<h1") || strings.Contains(got, "<div") {
		t.Fatalf("unexpected generated markup: %s", got)
	}
}
