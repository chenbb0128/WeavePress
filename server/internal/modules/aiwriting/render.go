package aiwriting

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

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
