package editorial

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func RenderArticle(article workspace.Article) string {
	var result strings.Builder
	for _, block := range article.Blocks {
		text := html.EscapeString(strings.TrimSpace(block.Text))
		switch block.Type {
		case "heading":
			level := block.Level
			if level < 2 || level > 4 {
				level = 2
			}
			fmt.Fprintf(&result, "<h%d>%s</h%d>", level, text, level)
		case "paragraph":
			if text != "" {
				result.WriteString("<p>" + text + "</p>")
			}
		case "quote":
			result.WriteString("<blockquote>" + text + "</blockquote>")
		case "list":
			result.WriteString("<ul><li>" + text + "</li></ul>")
		case "code":
			result.WriteString("<pre><code>" + text + "</code></pre>")
		case "image":
			if block.AssetID == nil {
				continue
			}
			fmt.Fprintf(&result, `<figure><img data-weavepress-asset-id="%d" alt="%s">`, *block.AssetID, html.EscapeString(block.Alt))
			if strings.TrimSpace(block.Alt) != "" {
				result.WriteString("<figcaption>" + html.EscapeString(block.Alt) + "</figcaption>")
			}
			result.WriteString("</figure>")
		}
	}
	if result.Len() == 0 && strings.TrimSpace(article.PlainText) != "" {
		for _, paragraph := range strings.Split(article.PlainText, "\n") {
			if paragraph = strings.TrimSpace(paragraph); paragraph != "" {
				result.WriteString("<p>" + html.EscapeString(paragraph) + "</p>")
			}
		}
	}
	return SanitizeHTML(result.String())
}

func SanitizeHTML(value string) string {
	policy := bluemonday.NewPolicy()
	policy.AllowElements("section", "h2", "h3", "h4", "p", "blockquote", "ul", "ol", "li", "pre", "code", "strong", "b", "em", "i", "u", "br", "figure", "figcaption", "img", "a")
	policy.AllowAttrs("data-weavepress-asset-id", "alt").OnElements("img")
	policy.AllowAttrs("href", "title").OnElements("a")
	policy.AllowStandardURLs()
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	return strings.TrimSpace(policy.Sanitize(value))
}

func ReferencedAssetIDs(value string) ([]uint64, error) {
	nodes, err := xhtml.ParseFragment(strings.NewReader(value), fragmentContext())
	if err != nil {
		return nil, err
	}
	seen := map[uint64]struct{}{}
	result := make([]uint64, 0)
	var walk func(*xhtml.Node) error
	walk = func(node *xhtml.Node) error {
		if node.Type == xhtml.ElementNode && node.Data == "img" {
			found := false
			for _, attr := range node.Attr {
				if attr.Key != "data-weavepress-asset-id" {
					continue
				}
				found = true
				id, parseErr := strconv.ParseUint(attr.Val, 10, 64)
				if parseErr != nil || id == 0 {
					return fmt.Errorf("invalid draft asset id")
				}
				if _, exists := seen[id]; !exists {
					seen[id] = struct{}{}
					result = append(result, id)
				}
			}
			if !found {
				return fmt.Errorf("draft image must reference an archived asset")
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := walk(node); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func PreviewHTML(value string, assetURL func(uint64) string) string {
	nodes, err := xhtml.ParseFragment(strings.NewReader(value), fragmentContext())
	if err != nil {
		return ""
	}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "img" {
			for _, attr := range node.Attr {
				if attr.Key == "data-weavepress-asset-id" {
					if id, parseErr := strconv.ParseUint(attr.Val, 10, 64); parseErr == nil && id > 0 {
						node.Attr = append(node.Attr, xhtml.Attribute{Key: "src", Val: assetURL(id)})
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	var output bytes.Buffer
	for _, node := range nodes {
		walk(node)
		_ = xhtml.Render(&output, node)
	}
	return output.String()
}

func fragmentContext() *xhtml.Node {
	return &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
}
