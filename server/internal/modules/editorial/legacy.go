package editorial

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	xhtml "golang.org/x/net/html"
)

type LegacyConversion struct {
	Document Document
	Warnings []string
}

func ConvertLegacyHTML(content string, articleToDraft map[uint64]uint64) (LegacyConversion, error) {
	sanitized := SanitizeHTML(content)
	warnings := legacyDroppedElementWarnings(content)
	nodes, err := xhtml.ParseFragment(strings.NewReader(sanitized), fragmentContext())
	if err != nil {
		return LegacyConversion{}, fmt.Errorf("%w: %v", ErrLegacyConvertFailed, err)
	}

	converter := legacyConverter{articleToDraft: articleToDraft, warnings: warnings}
	document := Document{Type: "doc"}
	for _, node := range nodes {
		document.Content = append(document.Content, converter.convertBlock(node)...)
	}
	if !documentHasBody(document) {
		return LegacyConversion{}, ErrLegacyConvertFailed
	}
	if err := ValidateDocument(document); err != nil {
		return LegacyConversion{}, fmt.Errorf("%w: %v", ErrLegacyConvertFailed, err)
	}
	return LegacyConversion{Document: document, Warnings: converter.warnings}, nil
}

type legacyConverter struct {
	articleToDraft map[uint64]uint64
	warnings       []string
}

func (converter *legacyConverter) convertBlock(node *xhtml.Node) []Node {
	switch node.Type {
	case xhtml.TextNode:
		text := strings.TrimSpace(node.Data)
		if text == "" {
			return nil
		}
		return []Node{paragraphNode([]Node{{Type: "text", Text: text}})}
	case xhtml.ElementNode:
		switch node.Data {
		case "h2", "h3", "h4":
			level := uint(2)
			if node.Data != "h2" {
				level = 3
			}
			content := converter.convertInlineChildren(node, nil)
			if !inlineHasText(content) {
				return nil
			}
			return []Node{{Type: "heading", Attrs: rawAttrs(map[string]any{"level": level}), Content: content}}
		case "p":
			return converter.convertParagraph(node)
		case "blockquote":
			content := converter.convertBlockChildren(node)
			if len(content) == 0 {
				return nil
			}
			return []Node{{Type: "blockquote", Content: content}}
		case "ul", "ol":
			return converter.convertList(node)
		case "figure", "img":
			if image, ok := converter.convertImage(node); ok {
				return []Node{image}
			}
			return nil
		default:
			text := strings.TrimSpace(legacyTextContent(node))
			if text == "" {
				return nil
			}
			converter.warn("无法精确保留 <%s> 结构，已降级为普通段落", node.Data)
			return []Node{paragraphNode([]Node{{Type: "text", Text: text}})}
		}
	default:
		return nil
	}
}

func (converter *legacyConverter) convertParagraph(node *xhtml.Node) []Node {
	result := make([]Node, 0)
	inline := make([]Node, 0)
	flushInline := func() {
		if inlineHasText(inline) {
			result = append(result, paragraphNode(inline))
		}
		inline = nil
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == "img" {
			flushInline()
			if image, ok := converter.convertImage(child); ok {
				result = append(result, image)
			}
			continue
		}
		inline = append(inline, converter.convertInline(child, nil)...)
	}
	flushInline()
	return result
}

func (converter *legacyConverter) convertBlockChildren(parent *xhtml.Node) []Node {
	result := make([]Node, 0)
	var inline []*xhtml.Node
	flushInline := func() {
		if len(inline) == 0 {
			return
		}
		wrapper := &xhtml.Node{Type: xhtml.ElementNode, Data: "p"}
		for _, child := range inline {
			wrapper.AppendChild(cloneHTMLNode(child))
		}
		content := converter.convertInlineChildren(wrapper, nil)
		if inlineHasText(content) {
			result = append(result, paragraphNode(content))
		}
		inline = nil
	}
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		if isLegacyBlockElement(child) {
			flushInline()
			result = append(result, converter.convertBlock(child)...)
			continue
		}
		inline = append(inline, child)
	}
	flushInline()
	return result
}

func (converter *legacyConverter) convertList(node *xhtml.Node) []Node {
	items := make([]Node, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != xhtml.ElementNode || child.Data != "li" {
			continue
		}
		content := converter.convertBlockChildren(child)
		if len(content) > 0 {
			items = append(items, Node{Type: "listItem", Content: content})
		}
	}
	if len(items) == 0 {
		return nil
	}
	typeName := "bulletList"
	if node.Data == "ol" {
		typeName = "orderedList"
	}
	return []Node{{Type: typeName, Content: items}}
}

func (converter *legacyConverter) convertInlineChildren(parent *xhtml.Node, marks []Mark) []Node {
	result := make([]Node, 0)
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		result = append(result, converter.convertInline(child, marks)...)
	}
	return result
}

func (converter *legacyConverter) convertInline(node *xhtml.Node, marks []Mark) []Node {
	switch node.Type {
	case xhtml.TextNode:
		if node.Data == "" {
			return nil
		}
		return []Node{{Type: "text", Text: node.Data, Marks: append([]Mark(nil), marks...)}}
	case xhtml.ElementNode:
		switch node.Data {
		case "br":
			return []Node{{Type: "hardBreak"}}
		case "strong", "b":
			return converter.convertInlineChildren(node, appendMark(marks, Mark{Type: "bold"}))
		case "em", "i":
			return converter.convertInlineChildren(node, appendMark(marks, Mark{Type: "italic"}))
		case "u":
			return converter.convertInlineChildren(node, appendMark(marks, Mark{Type: "underline"}))
		case "a":
			attrs := make(map[string]any)
			for _, attr := range node.Attr {
				switch attr.Key {
				case "href":
					attrs["href"] = attr.Val
				case "title":
					attrs["title"] = attr.Val
				}
			}
			if _, ok := attrs["href"]; !ok {
				converter.warn("链接缺少安全地址，已保留文字")
				return converter.convertInlineChildren(node, marks)
			}
			if href, _ := attrs["href"].(string); !isSupportedLinkHref(href) {
				converter.warn("链接协议不受支持，已保留文字")
				return converter.convertInlineChildren(node, marks)
			}
			return converter.convertInlineChildren(node, appendMark(marks, Mark{Type: "link", Attrs: rawAttrs(attrs)}))
		default:
			converter.warn("无法精确保留 <%s> 行内结构，已保留文字", node.Data)
			return converter.convertInlineChildren(node, marks)
		}
	default:
		return nil
	}
}

func (converter *legacyConverter) convertImage(node *xhtml.Node) (Node, bool) {
	image := node
	caption := ""
	if node.Data == "figure" {
		image = firstLegacyElement(node, "img")
		if captionNode := firstLegacyElement(node, "figcaption"); captionNode != nil {
			caption = strings.TrimSpace(legacyTextContent(captionNode))
		}
	}
	if image == nil {
		converter.warn("图片结构缺少 img，已丢弃")
		return Node{}, false
	}
	var articleID uint64
	alt := ""
	for _, attr := range image.Attr {
		switch attr.Key {
		case "data-weavepress-asset-id":
			articleID, _ = strconv.ParseUint(attr.Val, 10, 64)
		case "alt":
			alt = attr.Val
		}
	}
	draftID, ok := converter.articleToDraft[articleID]
	if articleID == 0 || !ok || draftID == 0 {
		converter.warn("旧图片素材 %d 缺少草稿素材映射，已丢弃", articleID)
		return Node{}, false
	}
	return Node{Type: "image", Attrs: rawAttrs(map[string]any{
		"draftAssetId": draftID,
		"width":        100,
		"align":        "center",
		"alt":          alt,
		"caption":      caption,
	})}, true
}

func (converter *legacyConverter) warn(format string, args ...any) {
	converter.warnings = append(converter.warnings, fmt.Sprintf(format, args...))
}

func legacyDroppedElementWarnings(content string) []string {
	nodes, err := xhtml.ParseFragment(strings.NewReader(content), fragmentContext())
	if err != nil {
		return nil
	}
	warnings := make([]string, 0)
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && !knownLegacyElement(node.Data) {
			warnings = append(warnings, fmt.Sprintf("不支持的 <%s> 节点已清理", node.Data))
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return warnings
}

func knownLegacyElement(name string) bool {
	switch name {
	case "section", "h2", "h3", "h4", "p", "blockquote", "ul", "ol", "li", "pre", "code", "strong", "b", "em", "i", "u", "br", "figure", "figcaption", "img", "a":
		return true
	default:
		return false
	}
}

func isLegacyBlockElement(node *xhtml.Node) bool {
	if node.Type != xhtml.ElementNode {
		return false
	}
	switch node.Data {
	case "h2", "h3", "h4", "p", "blockquote", "ul", "ol", "figure", "img", "section", "pre":
		return true
	default:
		return false
	}
}

func paragraphNode(content []Node) Node {
	return Node{Type: "paragraph", Content: content}
}

func rawAttrs(attrs map[string]any) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(attrs))
	for name, value := range attrs {
		result[name], _ = json.Marshal(value)
	}
	return result
}

func appendMark(marks []Mark, mark Mark) []Mark {
	result := append([]Mark(nil), marks...)
	return append(result, mark)
}

func inlineHasText(nodes []Node) bool {
	for _, node := range nodes {
		if node.Type == "hardBreak" || strings.TrimFunc(node.Text, unicode.IsSpace) != "" {
			return true
		}
	}
	return false
}

func documentHasBody(document Document) bool {
	return len(document.Content) > 0
}

func legacyTextContent(node *xhtml.Node) string {
	var result strings.Builder
	var walk func(*xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current.Type == xhtml.TextNode {
			result.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return result.String()
}

func firstLegacyElement(node *xhtml.Node, name string) *xhtml.Node {
	if node.Type == xhtml.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := firstLegacyElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func cloneHTMLNode(node *xhtml.Node) *xhtml.Node {
	clone := &xhtml.Node{Type: node.Type, DataAtom: node.DataAtom, Data: node.Data, Namespace: node.Namespace, Attr: append([]xhtml.Attribute(nil), node.Attr...)}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		clone.AppendChild(cloneHTMLNode(child))
	}
	return clone
}
