package editorial

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestValidateDocument(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"paragraph", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文","marks":[{"type":"bold"}]}]}]}`, true},
		{"image", `{"type":"doc","content":[{"type":"image","attrs":{"draftAssetId":18,"width":75,"align":"center","alt":"产品","caption":"说明"}}]}`, true},
		{"horizontal rule", `{"type":"doc","content":[{"type":"horizontalRule"}]}`, true},
		{"horizontal rule in blockquote", `{"type":"doc","content":[{"type":"blockquote","content":[{"type":"horizontalRule"}]}]}`, true},
		{"horizontal rule in list item", `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"horizontalRule"}]}]}]}`, true},
		{"horizontal rule in paragraph rejected", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"horizontalRule"}]}]}`, false},
		{"h1 rejected", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1}}]}`, false},
		{"script node rejected", `{"type":"doc","content":[{"type":"script"}]}`, false},
		{"javascript link rejected", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"链接","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}]}`, false},
		{"unknown image attr rejected", `{"type":"doc","content":[{"type":"image","attrs":{"draftAssetId":18,"width":100,"align":"center","alt":"","caption":"","style":"display:none"}}]}`, false},
		{"unknown node field rejected", `{"type":"doc","content":[{"type":"paragraph","unsafe":true}]}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseDocument(json.RawMessage(test.raw))
			if (err == nil) != test.ok {
				t.Fatalf("error = %v, ok = %v", err, test.ok)
			}
		})
	}
}

func TestValidateDocumentLimits(t *testing.T) {
	tests := []struct {
		name string
		doc  Document
	}{
		{
			name: "5001 nodes",
			doc:  Document{Type: "doc", Content: repeatedNodes(5001, Node{Type: "paragraph"})},
		},
		{
			name: "depth 17",
			doc:  Document{Type: "doc", Content: []Node{nestedBlockquotes(17)}},
		},
		{
			name: "more than 200000 runes",
			doc: Document{Type: "doc", Content: []Node{{
				Type: "paragraph", Content: []Node{{Type: "text", Text: strings.Repeat("界", 200001)}},
			}}},
		},
		{
			name: "101 images",
			doc:  Document{Type: "doc", Content: repeatedNodes(101, imageNode(1))},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateDocument(test.doc); err == nil {
				t.Fatal("document exceeding limit was accepted")
			}
		})
	}
}

func TestReferencedDraftAssetIDsDeduplicatesInFirstSeenOrder(t *testing.T) {
	doc := Document{Type: "doc", Content: []Node{imageNode(8), imageNode(3), imageNode(8), imageNode(5)}}
	if got := ReferencedDraftAssetIDs(doc); !reflect.DeepEqual(got, []uint64{8, 3, 5}) {
		t.Fatalf("ReferencedDraftAssetIDs() = %v", got)
	}
}

func repeatedNodes(count int, node Node) []Node {
	result := make([]Node, count)
	for i := range result {
		result[i] = node
	}
	return result
}

func nestedBlockquotes(depth int) Node {
	node := Node{Type: "paragraph"}
	for range depth {
		node = Node{Type: "blockquote", Content: []Node{node}}
	}
	return node
}

func imageNode(id uint64) Node {
	attrs := func(value any) json.RawMessage {
		raw, _ := json.Marshal(value)
		return raw
	}
	return Node{Type: "image", Attrs: map[string]json.RawMessage{
		"draftAssetId": attrs(id),
		"width":        attrs(100),
		"align":        attrs("center"),
		"alt":          attrs(""),
		"caption":      attrs(""),
	}}
}
