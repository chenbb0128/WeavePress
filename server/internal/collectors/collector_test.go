package collectors

import (
	"context"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type fixtureFetcher struct {
	result FetchResult
	err    error
}

func (f fixtureFetcher) Validate(context.Context, *url.URL) error { return f.err }
func (f fixtureFetcher) FetchHTML(context.Context, string) (FetchResult, error) {
	return f.result, f.err
}
func (f fixtureFetcher) FetchImage(context.Context, string, string) (FetchResult, error) {
	return FetchResult{}, f.err
}

func TestExtractContentBuildsSafeBlocks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<main><h2>标题</h2><p>第一段内容足够用于解析。</p><script>alert(1)</script><blockquote>引用</blockquote><img data-src="/image.jpg" alt="配图"></main>`))
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse("https://example.com/article")
	normalizeImages(doc.Selection, base)
	html, text, blocks, images, err := extractContent(doc.Selection, base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "script") || strings.Contains(html, "alert") {
		t.Fatalf("unsafe HTML: %s", html)
	}
	if !strings.Contains(text, "第一段") || len(blocks) < 4 {
		t.Fatalf("unexpected extraction: text=%q blocks=%d", text, len(blocks))
	}
	if len(images) != 1 || images[0].SourceURL != "https://example.com/image.jpg" {
		t.Fatalf("images = %#v", images)
	}
}

func TestWeChatCollectorFixture(t *testing.T) {
	pageURL, _ := url.Parse("https://mp.weixin.qq.com/s?__biz=test&mid=1&idx=1&sn=fixture")
	html := `<!doctype html><html><head><meta property="og:title" content="微信样本标题"><meta property="og:image" content="/cover.jpg"><meta name="author" content="样本作者"></head><body><strong class="profile_nickname">样本公众号</strong><div id="js_content"><h2>小标题</h2><p>这是一段用于微信公众号采集器固定样本测试的正文内容，长度需要足够，以确保正文校验能够稳定通过并生成结构化内容块。</p><img data-src="/body.jpg" alt="正文图片"></div><script>var ct = "1700000000";</script></body></html>`
	article, err := (WeChatCollector{}).Collect(context.Background(), fixtureFetcher{result: FetchResult{Body: []byte(html), FinalURL: pageURL, ContentType: "text/html"}}, pageURL.String())
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "微信样本标题" || article.Author != "样本作者" || article.SourceName != "样本公众号" {
		t.Fatalf("unexpected metadata: title=%q author=%q source=%q", article.Title, article.Author, article.SourceName)
	}
	if article.PublishedAt == nil || len(article.Images) != 2 || !article.Images[0].IsCover {
		t.Fatalf("unexpected time or images: published=%v images=%#v", article.PublishedAt, article.Images)
	}
}

func TestWebCollectorFixture(t *testing.T) {
	pageURL, _ := url.Parse("https://example.com/posts/fixture")
	html := `<!doctype html><html lang="zh-CN"><head><title>网页样本标题</title><meta name="author" content="网页作者"><meta property="og:site_name" content="样本站点"></head><body><article><h1>网页样本标题</h1><p>这是一段用于普通网页采集器固定样本测试的主要正文内容，包含足够多的中文字符，以便 Readability 稳定识别文章主体。</p><p>第二段继续提供有意义的正文信息，并验证提取后的纯文本与结构化段落可以正常返回。</p><img src="/fixture.png" alt="网页配图"></article></body></html>`
	article, err := (WebCollector{}).Collect(context.Background(), fixtureFetcher{result: FetchResult{Body: []byte(html), FinalURL: pageURL, ContentType: "text/html"}}, pageURL.String())
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "网页样本标题" || len(article.Blocks) == 0 || len(article.Images) != 1 {
		t.Fatalf("unexpected article: title=%q blocks=%d images=%d", article.Title, len(article.Blocks), len(article.Images))
	}
	if article.Images[0].SourceURL != "https://example.com/fixture.png" {
		t.Fatalf("image URL = %q", article.Images[0].SourceURL)
	}
}

func TestExtractContentPreservesNestedTextAndImageOrder(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="js_content">开头<section><span leaf="">第一段<strong>强调</strong>结尾</span></section><blockquote><p>引用<span>内容</span></p></blockquote><section><span>图前</span><img src="/a.jpg"><span>图后</span><br>下一行</section><ul><li><p>列表文字</p></li></ul>尾声</div>`))
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse("https://example.com/article")
	_, _, blocks, _, err := extractContent(doc.Find("#js_content"), base)
	if err != nil {
		t.Fatal(err)
	}
	want := []workspace.Block{
		{Type: "paragraph", Text: "开头"},
		{Type: "paragraph", Text: "第一段强调结尾"},
		{Type: "quote", Text: "引用内容"},
		{Type: "paragraph", Text: "图前"},
		{Type: "image", SourceURL: "https://example.com/a.jpg"},
		{Type: "paragraph", Text: "图后"},
		{Type: "paragraph", Text: "下一行"},
		{Type: "list", Text: "列表文字"},
		{Type: "paragraph", Text: "尾声"},
	}
	if !reflect.DeepEqual(blocks, want) {
		t.Fatalf("blocks = %#v, want %#v", blocks, want)
	}
}

func TestWeChatSectionSpanArticlePreservesAllText(t *testing.T) {
	body, err := os.ReadFile("testdata/wechat-section-span.html")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse("https://mp.weixin.qq.com/s/kr9amIr21vzoUjO_DTZgYw")
	page := `<meta name="author" content="后厂村吴彦祖"><a id="js_name">新闻哥</a>` + string(body)
	article, err := (WeChatCollector{}).Collect(context.Background(), fixtureFetcher{result: FetchResult{Body: []byte(page), FinalURL: base}}, base.String())
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, block := range article.Blocks {
		text.WriteString(block.Text)
	}
	withoutSpace := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	if withoutSpace(text.String()) != withoutSpace(article.PlainText) {
		t.Fatalf("structured text lost or duplicated: got %d runes, original %d", len([]rune(text.String())), len([]rune(article.PlainText)))
	}
	if len(article.Images) != 37 {
		t.Fatalf("images = %d, want 37", len(article.Images))
	}
	if article.SourceName != "新闻哥" {
		t.Fatalf("source = %q", article.SourceName)
	}
}

func TestValidateCollectedRejectsPartialStructuredText(t *testing.T) {
	_, err := validateCollected(workspace.CollectedArticle{
		PlainText: strings.Repeat("正文", 40) + "尾部",
		Blocks:    []workspace.Block{{Type: "paragraph", Text: "尾部"}, {Type: "image", SourceURL: "https://example.com/a.jpg"}},
	})
	if err == nil {
		t.Fatal("incomplete structured content was accepted")
	}
}
