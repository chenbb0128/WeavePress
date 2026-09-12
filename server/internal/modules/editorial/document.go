package editorial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"unicode/utf8"
)

type Document struct {
	Type    string `json:"type"`
	Content []Node `json:"content,omitempty"`
}

type Node struct {
	Type    string                     `json:"type"`
	Attrs   map[string]json.RawMessage `json:"attrs,omitempty"`
	Content []Node                     `json:"content,omitempty"`
	Text    string                     `json:"text,omitempty"`
	Marks   []Mark                     `json:"marks,omitempty"`
}

type Mark struct {
	Type  string                     `json:"type"`
	Attrs map[string]json.RawMessage `json:"attrs,omitempty"`
}

const (
	maxDocumentNodes  = 5000
	maxDocumentDepth  = 16
	maxDocumentRunes  = 200000
	maxDocumentImages = 100
)

func ParseDocument(raw json.RawMessage) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, invalidDocument(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return Document{}, invalidDocument(err)
	}
	if err := ValidateDocument(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

func ValidateDocument(document Document) error {
	if document.Type != "doc" {
		return invalidDocument(fmt.Errorf("root type must be doc"))
	}
	state := documentValidationState{}
	for i := range document.Content {
		if err := state.validateNode(document.Content[i], "doc", 1); err != nil {
			return invalidDocument(err)
		}
	}
	return nil
}

func ReferencedDraftAssetIDs(document Document) []uint64 {
	seen := make(map[uint64]struct{})
	result := make([]uint64, 0)
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, node := range nodes {
			if node.Type == "image" {
				var id uint64
				if raw, ok := node.Attrs["draftAssetId"]; ok && json.Unmarshal(raw, &id) == nil && id > 0 {
					if _, exists := seen[id]; !exists {
						seen[id] = struct{}{}
						result = append(result, id)
					}
				}
			}
			walk(node.Content)
		}
	}
	walk(document.Content)
	return result
}

type documentValidationState struct {
	nodes  int
	runes  int
	images int
}

func (state *documentValidationState) validateNode(node Node, parent string, depth int) error {
	state.nodes++
	if state.nodes > maxDocumentNodes {
		return fmt.Errorf("document has more than %d nodes", maxDocumentNodes)
	}
	if depth > maxDocumentDepth {
		return fmt.Errorf("document depth exceeds %d", maxDocumentDepth)
	}
	if !allowsChild(parent, node.Type) {
		return fmt.Errorf("%s cannot contain %s", parent, node.Type)
	}

	switch node.Type {
	case "paragraph", "blockquote", "bulletList", "orderedList", "listItem":
		if err := requireNoAttrs(node.Attrs); err != nil {
			return err
		}
		if node.Text != "" || len(node.Marks) != 0 {
			return fmt.Errorf("%s has invalid text or marks", node.Type)
		}
	case "heading":
		if err := requireAttrs(node.Attrs, "level"); err != nil {
			return err
		}
		var level uint
		if err := decodeAttr(node.Attrs, "level", &level); err != nil || (level != 2 && level != 3) {
			return fmt.Errorf("heading level must be 2 or 3")
		}
		if node.Text != "" || len(node.Marks) != 0 {
			return fmt.Errorf("heading has invalid text or marks")
		}
	case "text":
		if err := requireNoAttrs(node.Attrs); err != nil {
			return err
		}
		if len(node.Content) != 0 {
			return fmt.Errorf("text cannot contain child nodes")
		}
		state.runes += utf8.RuneCountInString(node.Text)
		if state.runes > maxDocumentRunes {
			return fmt.Errorf("document text exceeds %d runes", maxDocumentRunes)
		}
		for _, mark := range node.Marks {
			if err := validateMark(mark); err != nil {
				return err
			}
		}
	case "hardBreak":
		if err := requireNoAttrs(node.Attrs); err != nil {
			return err
		}
		if len(node.Content) != 0 || node.Text != "" || len(node.Marks) != 0 {
			return fmt.Errorf("hardBreak must be empty")
		}
	case "image":
		if err := state.validateImage(node); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported node type %q", node.Type)
	}

	for i := range node.Content {
		if err := state.validateNode(node.Content[i], node.Type, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (state *documentValidationState) validateImage(node Node) error {
	if err := requireAttrs(node.Attrs, "draftAssetId", "width", "align", "alt", "caption"); err != nil {
		return err
	}
	if len(node.Content) != 0 || node.Text != "" || len(node.Marks) != 0 {
		return fmt.Errorf("image must be a leaf node")
	}
	var id uint64
	if err := decodeAttr(node.Attrs, "draftAssetId", &id); err != nil || id == 0 {
		return fmt.Errorf("image draftAssetId must be positive")
	}
	var width uint
	if err := decodeAttr(node.Attrs, "width", &width); err != nil || (width != 50 && width != 75 && width != 100) {
		return fmt.Errorf("image width must be 50, 75, or 100")
	}
	var align, alt, caption string
	if err := decodeAttr(node.Attrs, "align", &align); err != nil || align != "center" {
		return fmt.Errorf("image align must be center")
	}
	if err := decodeAttr(node.Attrs, "alt", &alt); err != nil {
		return fmt.Errorf("image alt must be a string")
	}
	if err := decodeAttr(node.Attrs, "caption", &caption); err != nil {
		return fmt.Errorf("image caption must be a string")
	}
	state.images++
	if state.images > maxDocumentImages {
		return fmt.Errorf("document has more than %d images", maxDocumentImages)
	}
	return nil
}

func validateMark(mark Mark) error {
	switch mark.Type {
	case "bold", "italic", "underline":
		return requireNoAttrs(mark.Attrs)
	case "link":
		if err := allowAttrs(mark.Attrs, []string{"href"}, "title"); err != nil {
			return err
		}
		var href string
		if err := decodeAttr(mark.Attrs, "href", &href); err != nil {
			return fmt.Errorf("link href must be a string")
		}
		parsed, err := url.ParseRequestURI(href)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("link href must use http or https")
		}
		if raw, ok := mark.Attrs["title"]; ok {
			var title string
			if err := json.Unmarshal(raw, &title); err != nil {
				return fmt.Errorf("link title must be a string")
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported mark type %q", mark.Type)
	}
}

func allowsChild(parent, child string) bool {
	switch parent {
	case "doc":
		return child == "paragraph" || child == "heading" || child == "blockquote" || child == "bulletList" || child == "orderedList" || child == "image"
	case "paragraph", "heading":
		return child == "text" || child == "hardBreak"
	case "blockquote":
		return child == "paragraph" || child == "heading" || child == "blockquote" || child == "bulletList" || child == "orderedList" || child == "image"
	case "bulletList", "orderedList":
		return child == "listItem"
	case "listItem":
		return child == "paragraph" || child == "heading" || child == "blockquote" || child == "bulletList" || child == "orderedList" || child == "image"
	default:
		return false
	}
}

func requireNoAttrs(attrs map[string]json.RawMessage) error {
	if len(attrs) != 0 {
		return fmt.Errorf("attributes are not allowed")
	}
	return nil
}

func requireAttrs(attrs map[string]json.RawMessage, names ...string) error {
	return allowAttrs(attrs, names)
}

func allowAttrs(attrs map[string]json.RawMessage, required []string, optional ...string) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, name := range required {
		allowed[name] = struct{}{}
		if _, ok := attrs[name]; !ok {
			return fmt.Errorf("missing attribute %q", name)
		}
	}
	for _, name := range optional {
		allowed[name] = struct{}{}
	}
	for name := range attrs {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("unsupported attribute %q", name)
		}
	}
	return nil
}

func decodeAttr(attrs map[string]json.RawMessage, name string, target any) error {
	raw, ok := attrs[name]
	if !ok {
		return fmt.Errorf("missing attribute %q", name)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid attribute %q", name)
	}
	return nil
}

func invalidDocument(err error) error {
	return fmt.Errorf("%w: %v", ErrDocumentInvalid, err)
}
