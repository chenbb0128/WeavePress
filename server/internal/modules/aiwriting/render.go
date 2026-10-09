package aiwriting

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

// BuildEditorDocument mirrors generated blocks into the structured editor model.
// Image IDs initially refer to source article assets; the store maps them to draft assets.
func BuildEditorDocument(blocks []GeneratedBlock) *editorial.Document {
	doc := &editorial.Document{Type: "doc", Content: make([]editorial.Node, 0, len(blocks))}
	for _, block := range blocks {
		textNode := func(value string) editorial.Node { return editorial.Node{Type: "text", Text: value} }
		switch block.Type {
		case "heading":
			level := block.Level
			if level == 4 {
				level = 3
			}
			if level != 2 && level != 3 {
				level = 2
			}
			doc.Content = append(doc.Content, editorial.Node{Type: "heading", Attrs: map[string]json.RawMessage{"level": json.RawMessage(fmt.Sprintf("%d", level))}, Content: []editorial.Node{textNode(block.Text)}})
		case "paragraph":
			doc.Content = append(doc.Content, editorial.Node{Type: "paragraph", Content: []editorial.Node{textNode(block.Text)}})
		case "quote":
			doc.Content = append(doc.Content, editorial.Node{Type: "blockquote", Content: []editorial.Node{{Type: "paragraph", Content: []editorial.Node{textNode(block.Text)}}}})
		case "list":
			items := make([]editorial.Node, 0, len(block.Items))
			for _, item := range block.Items {
				items = append(items, editorial.Node{Type: "listItem", Content: []editorial.Node{{Type: "paragraph", Content: []editorial.Node{textNode(item)}}}})
			}
			doc.Content = append(doc.Content, editorial.Node{Type: "bulletList", Content: items})
		case "image":
			if block.AssetID == nil || *block.AssetID == 0 {
				continue
			}
			doc.Content = append(doc.Content, editorial.Node{Type: "image", Attrs: map[string]json.RawMessage{
				"draftAssetId": json.RawMessage(fmt.Sprintf("%d", *block.AssetID)), "width": json.RawMessage("100"), "align": json.RawMessage(`"center"`), "alt": json.RawMessage(marshalPromptJSON(block.Alt)), "caption": json.RawMessage(`""`),
			}})
		}
	}
	return doc
}

func RenderGeneration(article workspace.Article, blocks []GeneratedBlock) string {
	var result strings.Builder
	for _, block := range blocks {
		switch block.Type {
		case "heading":
			if block.Level >= 2 && block.Level <= 4 && strings.TrimSpace(block.Text) != "" {
				fmt.Fprintf(&result, "<h%d>%s</h%d>", block.Level, escapedText(block.Text), block.Level)
			}
		case "paragraph":
			if strings.TrimSpace(block.Text) != "" {
				result.WriteString("<p>" + escapedText(block.Text) + "</p>")
			}
		case "quote":
			if strings.TrimSpace(block.Text) != "" {
				result.WriteString("<blockquote>" + escapedText(block.Text) + "</blockquote>")
			}
		case "list":
			if len(block.Items) > 0 {
				result.WriteString("<ul>")
				for _, item := range block.Items {
					if strings.TrimSpace(item) != "" {
						result.WriteString("<li>" + escapedText(item) + "</li>")
					}
				}
				result.WriteString("</ul>")
			}
		case "image":
			if block.AssetID != nil && *block.AssetID > 0 {
				fmt.Fprintf(&result, `<figure><img data-weavepress-asset-id="%d" alt="%s"></figure>`, *block.AssetID, html.EscapeString(block.Alt))
			}
		}
	}
	appendSourceAttribution(&result, article)
	return editorial.SanitizeHTML(result.String())
}

func appendSourceAttribution(result *strings.Builder, article workspace.Article) {
	result.WriteString("<h2>参考来源</h2><p>")
	parts := make([]string, 0, 3)
	if strings.TrimSpace(article.Title) != "" {
		parts = append(parts, "《"+escapedText(article.Title)+"》")
	}
	if strings.TrimSpace(article.SourceName) != "" {
		parts = append(parts, escapedText(article.SourceName))
	}
	if strings.TrimSpace(article.CanonicalURL) != "" {
		canonicalURL := strings.TrimSpace(article.CanonicalURL)
		escapedURL := html.EscapeString(canonicalURL)
		if safeSourceURL(canonicalURL) {
			parts = append(parts, `<a href="`+escapedURL+`">`+escapedURL+`</a>`)
		} else {
			parts = append(parts, escapedURL)
		}
	}
	result.WriteString(strings.Join(parts, " · "))
	result.WriteString("</p>")
}

func safeSourceURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func escapedText(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}
