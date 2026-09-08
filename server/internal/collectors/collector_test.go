package collectors

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
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
