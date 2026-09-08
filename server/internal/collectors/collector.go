package collectors

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	readability "github.com/go-shiori/go-readability"
	"github.com/microcosm-cc/bluemonday"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type Collector interface {
	Supports(*url.URL) bool
	Collect(context.Context, Fetcher, string) (workspace.CollectedArticle, error)
}

type Registry struct{ collectors []Collector }

func NewRegistry() *Registry {
	return &Registry{collectors: []Collector{WeChatCollector{}, WebCollector{}}}
}

func (r *Registry) Collect(ctx context.Context, fetcher Fetcher, rawURL string) (workspace.CollectedArticle, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return workspace.CollectedArticle{}, err
	}
	for _, collector := range r.collectors {
		if collector.Supports(parsed) {
			return collector.Collect(ctx, fetcher, rawURL)
		}
	}
	return workspace.CollectedArticle{}, collectError("UNSUPPORTED_SOURCE", "暂不支持该内容来源", false, nil)
}

type WeChatCollector struct{}

func (WeChatCollector) Supports(target *url.URL) bool {
	return target != nil && isWechatHost(strings.ToLower(target.Hostname()))
}
func (WeChatCollector) Collect(ctx context.Context, fetcher Fetcher, rawURL string) (workspace.CollectedArticle, error) {
	fetched, err := fetcher.FetchHTML(ctx, rawURL)
	if err != nil {
		return workspace.CollectedArticle{}, err
	}
	bodyText := string(fetched.Body)
	if strings.Contains(bodyText, "环境异常") || strings.Contains(bodyText, "访问过于频繁") || strings.Contains(bodyText, "请输入验证码") {
		return workspace.CollectedArticle{}, collectError("WECHAT_ACCESS_RESTRICTED", "微信公众号页面要求验证或限制访问", false, nil)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(fetched.Body))
	if err != nil {
		return workspace.CollectedArticle{}, collectError("PARSE_FAILED", "微信公众号页面解析失败", false, err)
	}
	content := doc.Find("#js_content").First()
	if content.Length() == 0 {
		return workspace.CollectedArticle{}, collectError("CONTENT_EMPTY", "没有找到微信公众号正文", false, nil)
	}
	normalizeImages(content, fetched.FinalURL)
	cleanHTML, plainText, blocks, images, err := extractContent(content, fetched.FinalURL)
	if err != nil {
		return workspace.CollectedArticle{}, err
	}
	title := firstNonEmpty(meta(doc, "property", "og:title"), strings.TrimSpace(doc.Find("#activity-name").First().Text()), strings.TrimSpace(doc.Find("title").Text()))
	author := firstNonEmpty(meta(doc, "name", "author"), strings.TrimSpace(doc.Find("#js_name").First().Text()))
	sourceName := firstNonEmpty(strings.TrimSpace(doc.Find(".profile_nickname").First().Text()), author)
	result := workspace.CollectedArticle{Title: title, Author: author, SourceName: sourceName, Language: "zh-CN", RawHTML: fetched.Body, CleanHTML: cleanHTML, PlainText: plainText, Blocks: blocks, Images: images, CoverURL: meta(doc, "property", "og:image"), Metadata: map[string]any{"collector": "wechat", "finalUrl": fetched.FinalURL.String()}}
	result.PublishedAt = parsePublishedTime(meta(doc, "property", "article:published_time"), bodyText)
	if result.CoverURL != "" {
		if absolute := resolveURL(fetched.FinalURL, result.CoverURL); absolute != "" {
			result.CoverURL = absolute
			result.Images = prependCover(result.Images, absolute)
		}
	}
	return validateCollected(result)
}

type WebCollector struct{}

func (WebCollector) Supports(target *url.URL) bool {
	return target != nil && (target.Scheme == "http" || target.Scheme == "https")
}
func (WebCollector) Collect(ctx context.Context, fetcher Fetcher, rawURL string) (workspace.CollectedArticle, error) {
	fetched, err := fetcher.FetchHTML(ctx, rawURL)
	if err != nil {
		return workspace.CollectedArticle{}, err
	}
	article, err := readability.FromReader(bytes.NewReader(fetched.Body), fetched.FinalURL)
	if err != nil {
		return workspace.CollectedArticle{}, collectError("PARSE_FAILED", "网页正文解析失败", false, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.Content))
	if err != nil {
		return workspace.CollectedArticle{}, collectError("PARSE_FAILED", "网页正文结构化失败", false, err)
	}
	content := doc.Selection
	normalizeImages(content, fetched.FinalURL)
	cleanHTML, plainText, blocks, images, err := extractContent(content, fetched.FinalURL)
	if err != nil {
		return workspace.CollectedArticle{}, err
	}
	result := workspace.CollectedArticle{Title: strings.TrimSpace(article.Title), Author: strings.TrimSpace(article.Byline), SourceName: strings.TrimSpace(article.SiteName), Language: strings.TrimSpace(article.Language), RawHTML: fetched.Body, CleanHTML: cleanHTML, PlainText: plainText, Blocks: blocks, Images: images, Metadata: map[string]any{"collector": "readability", "excerpt": article.Excerpt, "finalUrl": fetched.FinalURL.String()}}
	if article.PublishedTime != nil {
		value := article.PublishedTime.UTC()
		result.PublishedAt = &value
	}
	if article.Image != "" {
		result.CoverURL = resolveURL(fetched.FinalURL, article.Image)
		result.Images = prependCover(result.Images, result.CoverURL)
	}
	return validateCollected(result)
}

func extractContent(content *goquery.Selection, base *url.URL) (string, string, []workspace.Block, []workspace.CollectedImage, error) {
	content.Find("script,style,iframe,form,input,button,noscript").Remove()
	htmlValue, err := content.Html()
	if err != nil {
		return "", "", nil, nil, collectError("PARSE_FAILED", "读取正文失败", false, err)
	}
	policy := bluemonday.UGCPolicy()
	policy.AllowAttrs("src", "alt", "title").OnElements("img")
	cleanHTML := policy.Sanitize(htmlValue)
	plainText := normalizeSpace(content.Text())
	blocks := make([]workspace.Block, 0)
	images := make([]workspace.CollectedImage, 0)
	seen := map[string]bool{}
	position := uint(0)
	content.Find("h1,h2,h3,h4,h5,h6,p,blockquote,pre,li,img").Each(func(_ int, selection *goquery.Selection) {
		name := goquery.NodeName(selection)
		if name == "img" {
			source, _ := selection.Attr("src")
			source = resolveURL(base, source)
			if source == "" || seen[source] {
				return
			}
			seen[source] = true
			alt, _ := selection.Attr("alt")
			images = append(images, workspace.CollectedImage{SourceURL: source, Alt: strings.TrimSpace(alt), Position: position})
			blocks = append(blocks, workspace.Block{Type: "image", SourceURL: source, Alt: strings.TrimSpace(alt)})
			position++
			return
		}
		text := normalizeSpace(selection.Text())
		if text == "" {
			return
		}
		block := workspace.Block{Text: text}
		switch {
		case strings.HasPrefix(name, "h"):
			block.Type = "heading"
			block.Level, _ = strconv.Atoi(strings.TrimPrefix(name, "h"))
		case name == "blockquote":
			block.Type = "quote"
		case name == "pre":
			block.Type = "code"
		case name == "li":
			block.Type = "list"
		default:
			block.Type = "paragraph"
		}
		blocks = append(blocks, block)
	})
	return cleanHTML, plainText, blocks, images, nil
}

func validateCollected(article workspace.CollectedArticle) (workspace.CollectedArticle, error) {
	article.Title = normalizeSpace(article.Title)
	article.Author = normalizeSpace(article.Author)
	article.SourceName = normalizeSpace(article.SourceName)
	if article.Title == "" {
		article.Title = "未命名文章"
	}
	if len([]rune(article.PlainText)) < 50 || len(article.Blocks) == 0 {
		return workspace.CollectedArticle{}, collectError("CONTENT_EMPTY", "正文内容为空或过短", false, nil)
	}
	return article, nil
}

func normalizeImages(content *goquery.Selection, base *url.URL) {
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		source, ok := image.Attr("data-src")
		if !ok || strings.TrimSpace(source) == "" {
			source, _ = image.Attr("src")
		}
		image.SetAttr("src", resolveURL(base, source))
		image.RemoveAttr("data-src")
		image.RemoveAttr("onclick")
	})
}

func meta(doc *goquery.Document, attribute, value string) string {
	result, _ := doc.Find(fmt.Sprintf("meta[%s='%s']", attribute, value)).First().Attr("content")
	return strings.TrimSpace(result)
}
func resolveURL(base *url.URL, value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || value == "" {
		return ""
	}
	return base.ResolveReference(parsed).String()
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func normalizeSpace(value string) string { return strings.Join(strings.Fields(value), " ") }
func prependCover(images []workspace.CollectedImage, source string) []workspace.CollectedImage {
	if source == "" {
		return images
	}
	for index := range images {
		if images[index].SourceURL == source {
			images[index].IsCover = true
			return images
		}
	}
	return append([]workspace.CollectedImage{{SourceURL: source, IsCover: true}}, images...)
}

var wechatTimestamp = regexp.MustCompile(`(?m)(?:var\s+ct\s*=|"ct"\s*:)\s*["']?(\d{10})`)

func parsePublishedTime(metaValue, htmlValue string) *time.Time {
	if metaValue != "" {
		if parsed, err := time.Parse(time.RFC3339, metaValue); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	match := wechatTimestamp.FindStringSubmatch(htmlValue)
	if len(match) == 2 {
		seconds, _ := strconv.ParseInt(match[1], 10, 64)
		parsed := time.Unix(seconds, 0).UTC()
		return &parsed
	}
	return nil
}
