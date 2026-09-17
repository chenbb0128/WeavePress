package editorial

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

func RenderDocument(document Document, theme Theme) (string, error) {
	if err := ValidateDocument(document); err != nil {
		return "", err
	}
	registeredTheme, err := ResolveTheme(theme.ID, theme.Version)
	if err != nil {
		return "", err
	}

	var output strings.Builder
	output.WriteString(`<section style="`)
	output.WriteString(registeredTheme.BodyStyle)
	output.WriteString(`">`)
	for _, node := range document.Content {
		renderNode(&output, node, registeredTheme)
	}
	output.WriteString(`</section>`)
	return output.String(), nil
}

func renderNode(output *strings.Builder, node Node, theme Theme) {
	switch node.Type {
	case "paragraph":
		output.WriteString(`<p style="` + theme.ParagraphStyle + `">`)
		renderChildren(output, node.Content, theme)
		output.WriteString(`</p>`)
	case "heading":
		var level uint
		_ = json.Unmarshal(node.Attrs["level"], &level)
		if level == 2 {
			output.WriteString(`<h2 style="` + theme.Heading2Style + `">`)
			renderChildren(output, node.Content, theme)
			output.WriteString(`</h2>`)
			return
		}
		output.WriteString(`<h3 style="` + theme.Heading3Style + `">`)
		renderChildren(output, node.Content, theme)
		output.WriteString(`</h3>`)
	case "blockquote":
		output.WriteString(`<blockquote style="` + theme.QuoteStyle + `">`)
		renderChildren(output, node.Content, theme)
		output.WriteString(`</blockquote>`)
	case "bulletList":
		output.WriteString(`<ul style="` + theme.ListStyle + `">`)
		renderChildren(output, node.Content, theme)
		output.WriteString(`</ul>`)
	case "orderedList":
		output.WriteString(`<ol style="` + theme.ListStyle + `">`)
		renderChildren(output, node.Content, theme)
		output.WriteString(`</ol>`)
	case "listItem":
		output.WriteString(`<li>`)
		renderChildren(output, node.Content, theme)
		output.WriteString(`</li>`)
	case "horizontalRule":
		output.WriteString(`<hr style="` + theme.DividerStyle + `">`)
	case "hardBreak":
		output.WriteString(`<br>`)
	case "text":
		renderText(output, node, theme)
	case "image":
		renderImage(output, node, theme)
	}
}

func renderChildren(output *strings.Builder, nodes []Node, theme Theme) {
	for _, child := range nodes {
		renderNode(output, child, theme)
	}
}

func renderText(output *strings.Builder, node Node, theme Theme) {
	for _, mark := range node.Marks {
		switch mark.Type {
		case "bold":
			output.WriteString(`<strong>`)
		case "italic":
			output.WriteString(`<em>`)
		case "underline":
			output.WriteString(`<u>`)
		case "link":
			var href, title string
			_ = json.Unmarshal(mark.Attrs["href"], &href)
			if titleRaw, exists := mark.Attrs["title"]; exists {
				_ = json.Unmarshal(titleRaw, &title)
			}
			output.WriteString(`<a href="` + html.EscapeString(href) + `"`)
			if title != "" {
				output.WriteString(` title="` + html.EscapeString(title) + `"`)
			}
			output.WriteString(` target="_blank" rel="nofollow noopener noreferrer" style="` + theme.LinkStyle + `">`)
		}
	}
	output.WriteString(html.EscapeString(node.Text))
	for index := len(node.Marks) - 1; index >= 0; index-- {
		switch node.Marks[index].Type {
		case "bold":
			output.WriteString(`</strong>`)
		case "italic":
			output.WriteString(`</em>`)
		case "underline":
			output.WriteString(`</u>`)
		case "link":
			output.WriteString(`</a>`)
		}
	}
}

func renderImage(output *strings.Builder, node Node, theme Theme) {
	var id uint64
	var width uint
	var alt, caption string
	_ = json.Unmarshal(node.Attrs["draftAssetId"], &id)
	_ = json.Unmarshal(node.Attrs["width"], &width)
	_ = json.Unmarshal(node.Attrs["alt"], &alt)
	_ = json.Unmarshal(node.Attrs["caption"], &caption)

	imageWidth := imageWidthStyle(width)
	output.WriteString(`<figure style="margin:24px 0;text-align:center"><img data-weavepress-draft-asset-id="`)
	output.WriteString(fmt.Sprintf("%d", id))
	output.WriteString(`" alt="` + html.EscapeString(alt) + `" style="display:block;width:auto;max-width:` + imageWidth + `;height:auto;margin:0 auto">`)
	if caption != "" {
		output.WriteString(`<figcaption style="` + theme.CaptionStyle + `">` + html.EscapeString(caption) + `</figcaption>`)
	}
	output.WriteString(`</figure>`)
}

func imageWidthStyle(width uint) string {
	switch width {
	case 50:
		return "50%"
	case 75:
		return "75%"
	default:
		return "100%"
	}
}
